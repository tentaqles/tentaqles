package data

import (
	"path"
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
	for _, seg := range shellSegments(call.Command) {
		words := programWords(seg)
		if len(words) == 0 {
			continue
		}
		prog := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(words[0], `\`, "/"))), ".exe")
		for _, rc := range runCommands {
			if prog != rc.prog || !hasSub(words[1:], rc.sub) || helpOnly(words) {
				continue
			}
			what := rc.prog + " " + strings.Join(rc.sub, " ")
			if rc.backfill {
				return policy.Ask, "`" + what + "` reruns past intervals of a pipeline: check D1 and D2 idempotency first (pipeline-review), then confirm", true
			}
			return policy.Ask, "`" + what + "` starts a pipeline run outside the build: confirm it targets a dev environment and is safe to rerun", true
		}
	}
	return policy.Allow, "", false
}

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
func programWords(seg string) []string {
	w := shellWords(seg)
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
		case (h == "python" || h == "python3" || h == "py" || strings.HasSuffix(h, "python.exe")) && len(w) > 2 && w[1] == "-m":
			w = w[2:]
		default:
			return w
		}
	}
	return nil
}

// shellSegments splits a command on unquoted ; & | newlines and ( ).
func shellSegments(cmd string) []string {
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
