package data

import (
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// tq/pipeline-run (spec C §3.6, §5.3): ask before an agent starts an
// orchestrator run or a backfill. It is pure: it splits the command into
// shell segments and words and matches the program and its subcommand.

// runCommand is one orchestrator command the guard asks about.
type runCommand struct {
	prog     string   // program base name, lower case, without .exe
	sub      []string // leading subcommand words
	backfill bool
}

var runCommands = []runCommand{
	{"airflow", []string{"dags", "trigger"}, false},
	{"airflow", []string{"dags", "backfill"}, true},   // Airflow 2
	{"airflow", []string{"backfill", "create"}, true}, // Airflow 3
	{"dagster", []string{"job", "launch"}, false},
	{"dagster", []string{"job", "execute"}, false},
	{"dagster", []string{"asset", "materialize"}, false},
	{"dg", []string{"launch"}, false},
	{"prefect", []string{"deployment", "run"}, false},
	{"az", []string{"datafactory", "pipeline", "create-run"}, false},
	{"az", []string{"synapse", "pipeline", "create-run"}, false},
}

// launchers are wrappers whose first non-flag argument is the real program.
var launchers = map[string]bool{"sudo": true, "env": true, "exec": true, "nohup": true, "time": true, "&": true, "call": true}

func pipelineRunGuard(call policy.ToolCall, _ packs.GuardCtx) (policy.Action, string, bool) {
	if !call.IsShell() {
		return policy.Allow, "", false
	}
	m := &wrapMatcher{steps: maxWrapSteps}
	rc, ok := m.script(call.Command, 0)
	if m.exhausted {
		// Fail closed: a command wrapped too deeply to see through.
		return policy.Ask, "the command nests more wrappers (shells, containers, ssh, tq dotenv run) than tq/pipeline-run looks through; confirm it starts no pipeline run or backfill", true
	}
	if !ok {
		return policy.Allow, "", false
	}
	what := rc.prog + " " + strings.Join(rc.sub, " ")
	if rc.backfill {
		return policy.Ask, "`" + what + "` reruns past intervals of a pipeline: check D1 and D2 idempotency first (pipeline-review), then confirm", true
	}
	return policy.Ask, "`" + what + "` starts a pipeline run outside the build: confirm it targets a dev environment and is safe to rerun", true
}

// maxWrapDepth bounds the nesting of wrappers (`bash -c`, `docker exec`,
// `tq dotenv run`, ...) the guard looks through; maxWrapScan bounds how many
// words after a container or remote wrapper are tried as the start of the
// inner command; maxWrapSteps bounds the suffixes tried in all. Going past
// the depth or the steps fails closed.
const (
	maxWrapDepth = 4
	maxWrapScan  = 64
	maxWrapSteps = 2048
)

// wrapMatcher matches commands through wrappers within a step budget.
type wrapMatcher struct {
	steps     int
	exhausted bool
}

// script splits a shell string into segments and matches each.
func (m *wrapMatcher) script(script string, depth int) (runCommand, bool) {
	for _, seg := range pipeShellSegments(script) {
		if rc, ok := m.words(shellWords(seg), depth); ok || m.exhausted {
			return rc, ok
		}
	}
	return runCommand{}, false
}

// matchWords matches one segment's words: the orchestrator command itself,
// or a wrapper around it. Shells (`bash -c`, `pwsh -Command`, `cmd /c`,
// `eval`) have their script re-split; `astro dev run` is the Airflow CLI;
// container and remote wrappers (`docker [compose] exec|run`, `kubectl
// exec`, `ssh`, `tq dotenv run`) are matched at every later word, because
// their own flags may take values.
func (m *wrapMatcher) words(w []string, depth int) (runCommand, bool) {
	if m.exhausted {
		return runCommand{}, false
	}
	if depth > maxWrapDepth {
		m.exhausted = true
		return runCommand{}, false
	}
	w = programWords(w)
	if len(w) == 0 {
		return runCommand{}, false
	}
	prog := progName(w[0])
	for _, rc := range runCommands {
		if prog == rc.prog && hasSub(w[1:], rc.sub) && !helpOnly(w) {
			return rc, true
		}
	}
	switch {
	case shellProgs[prog]:
		for i := 1; i+1 < len(w); i++ {
			a := w[i]
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a[1:], 'c') {
				return m.script(w[i+1], depth+1)
			}
		}
	case prog == "pwsh" || prog == "powershell":
		for i := 1; i < len(w); i++ {
			a := strings.ToLower(w[i])
			if a == "-c" || a == "--command" || (len(a) >= 4 && strings.HasPrefix("-command", a)) {
				return m.script(strings.Join(w[i+1:], " "), depth+1)
			}
		}
	case prog == "cmd":
		for i := 1; i < len(w); i++ {
			if a := strings.ToLower(w[i]); a == "/c" || a == "/k" {
				return m.script(strings.Join(w[i+1:], " "), depth+1)
			}
		}
	case prog == "eval" || prog == "invoke-expression" || prog == "iex":
		return m.script(strings.Join(w[1:], " "), depth+1)
	case prog == "astro" && hasSub(w[1:], []string{"dev", "run"}):
		rest := w[1:]
		for len(rest) > 0 && !strings.EqualFold(rest[0], "run") {
			rest = rest[1:]
		}
		return m.words(append([]string{"airflow"}, rest[1:]...), depth+1)
	case wrapperProg(prog, w):
		for i := 1; i < len(w) && i <= maxWrapScan; i++ {
			// Only this scan branches; shell re-splits are linear in the
			// command, so the steps count wrapper suffixes alone.
			if m.steps--; m.steps < 0 {
				m.exhausted = true
				return runCommand{}, false
			}
			if rc, ok := m.words(w[i:], depth+1); ok || m.exhausted {
				return rc, ok
			}
			// A quoted remote command (`ssh host 'airflow dags trigger x'`).
			if strings.ContainsAny(w[i], " \t;&|") {
				if rc, ok := m.script(w[i], depth+1); ok || m.exhausted {
					return rc, ok
				}
			}
		}
	}
	return runCommand{}, false
}

// shellProgs take a script after a short flag holding `c` (`-c`, `-lc`).
var shellProgs = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

// wrapperProg reports a container or remote wrapper whose inner command is
// somewhere in its arguments.
func wrapperProg(prog string, w []string) bool {
	// Global flags may sit before the subcommand and take values
	// (`kubectl -n acme exec`), so any word naming it counts.
	hasWord := func(subs ...string) bool {
		for i, a := range w[1:] {
			if i >= maxWrapScan {
				break
			}
			for _, s := range subs {
				if strings.EqualFold(a, s) {
					return true
				}
			}
		}
		return false
	}
	switch prog {
	case "docker", "podman", "nerdctl", "docker-compose", "podman-compose":
		return hasWord("exec", "run")
	case "kubectl", "oc":
		return hasWord("exec", "run", "debug")
	case "ssh":
		return true
	case "tq":
		return hasSub(w[1:], []string{"dotenv", "run"})
	}
	return false
}

// progName is a program word's base name, lower case, without .exe.
func progName(word string) string {
	return strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(word, `\`, "/"))), ".exe")
}

// pythonProgRe matches python, python3, python3.12, py and pypy3.
var pythonProgRe = regexp.MustCompile(`^(?:python(?:[23](?:\.\d+)?)?|py|pypy3?)$`)

// hasSub reports whether the leading non-flag words are sub. Flags between
// subcommand words (`az synapse --only-show-errors pipeline create-run`)
// are skipped.
func hasSub(args, sub []string) bool {
	i := 0
	for _, a := range args {
		if i == len(sub) {
			return true
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if !strings.EqualFold(a, sub[i]) {
			return false
		}
		i++
	}
	return i == len(sub)
}

func helpOnly(words []string) bool {
	for _, w := range words[1:] {
		if w == "--help" || w == "-h" {
			return true
		}
	}
	return false
}

// programWords returns the words of one segment from the program on:
// leading VAR=value assignments, wrappers (sudo, env, &), `uv run`,
// `poetry run` and `python -m` are skipped.
func programWords(w []string) []string {
	for len(w) > 0 {
		h := strings.ToLower(w[0])
		switch {
		case strings.Contains(w[0], "=") && !strings.HasPrefix(w[0], "-") && !strings.HasPrefix(w[0], "="):
			w = w[1:]
		case launchers[h]:
			w = w[1:]
			for len(w) > 0 && strings.HasPrefix(w[0], "-") {
				w = w[1:]
			}
		case (h == "uv" || h == "poetry" || h == "pipenv" || h == "pdm" || h == "hatch") && len(w) > 1 && strings.EqualFold(w[1], "run"):
			w = w[2:]
		case pythonProgRe.MatchString(progName(w[0])) && len(w) > 2 && w[1] == "-m":
			w = w[2:]
		default:
			return w
		}
	}
	return nil
}

// pipeShellSegments splits a command on unquoted ; & | newlines and ( ).
func pipeShellSegments(cmd string) []string {
	var out []string
	var b strings.Builder
	var quote byte
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		b.Reset()
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			b.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			b.WriteByte(c)
		case (c == '\\' || c == '`') && continuation(cmd, i+1) > 0:
			// A line continuation: bash `\` or PowerShell backtick before
			// the newline joins the two lines into one command.
			i += continuation(cmd, i+1)
			b.WriteByte(' ')
		case c == '`':
			// Bash command substitution: the inner command is its own segment.
			flush()
		case c == ';' || c == '\n' || c == '\r' || c == '|' || c == '(' || c == ')' || c == '{' || c == '}':
			flush()
		case c == '&':
			// PowerShell's call operator `& prog` starts a segment; `&&` and a
			// trailing `&` separate commands.
			flush()
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				i++
			} else if strings.TrimSpace(cmd[i+1:]) != "" {
				b.WriteString("& ")
			}
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

// continuation returns the length of the newline at cmd[i:] ("\n" or
// "\r\n"), or 0.
func continuation(cmd string, i int) int {
	switch {
	case strings.HasPrefix(cmd[i:], "\r\n"):
		return 2
	case strings.HasPrefix(cmd[i:], "\n"):
		return 1
	}
	return 0
}

// shellWords splits a segment into words, removing quotes.
func shellWords(seg string) []string {
	var out []string
	var b strings.Builder
	var quote byte
	in := false
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t':
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		default:
			b.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, b.String())
	}
	return out
}
