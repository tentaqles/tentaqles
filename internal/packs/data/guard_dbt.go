package data

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Guard ids of spec C §3.6 / §5.3 owned by the dbt workstream.
const (
	guardProdTarget  = "tq/dbt-prod-target"
	guardFullRefresh = "tq/dbt-full-refresh"
)

// prodWriteCommands are the dbt subcommands that write to the target. The
// spec lists build, run, seed, snapshot, run-operation and test; clone and
// retry write too (clone creates relations in the target, retry replays the
// last invocation), so they are covered as well.
var prodWriteCommands = map[string]bool{"build": true, "run": true, "seed": true, "snapshot": true,
	"run-operation": true, "test": true, "clone": true, "retry": true}

// fullRefreshCommands take --full-refresh.
var fullRefreshCommands = map[string]bool{"build": true, "run": true, "seed": true}

// dbtSubcommands are every dbt subcommand (the first of them after `dbt` is
// the subcommand; global flags may come before it).
var dbtSubcommands = map[string]bool{"build": true, "run": true, "seed": true, "snapshot": true, "run-operation": true,
	"test": true, "clone": true, "retry": true, "compile": true, "parse": true, "docs": true, "debug": true, "deps": true,
	"clean": true, "list": true, "ls": true, "source": true, "show": true, "init": true}

// wrappers may precede dbt in a command (uv run dbt, python -m dbt, ...).
var wrappers = map[string]bool{"env": true, "sudo": true, "time": true, "nohup": true, "exec": true, "call": true,
	"uv": true, "uvx": true, "poetry": true, "pipx": true, "pdm": true, "hatch": true, "rye": true, "pipenv": true,
	"run": true, "python": true, "python3": true, "py": true, "-m": true, "--": true, "conda": true}

// dbtCall is one dbt invocation found in a shell command.
type dbtCall struct {
	sub         string
	target      string // --target / -t, else DBT_TARGET set in the command
	fullRefresh bool
	profilesDir string
	projectDir  string
	profile     string
}

// word is one shell word; quoted reports a word that had quotes in it.
type word struct {
	text   string
	quoted bool
}

// shellSegments splits a Bash or PowerShell command into simple commands
// (on ; & | newlines, parentheses and braces outside quotes) of words. A
// backslash is kept outside quotes (Windows paths); the PowerShell backtick
// escapes the next character, and inside double quotes both escape.
func shellSegments(cmd string) [][]word {
	var segs [][]word
	var cur []word
	var b strings.Builder
	inWord, quoted := false, false
	flushWord := func() {
		if inWord {
			cur = append(cur, word{b.String(), quoted})
		}
		b.Reset()
		inWord, quoted = false, false
	}
	flushSeg := func() {
		flushWord()
		if len(cur) > 0 {
			segs = append(segs, cur)
		}
		cur = nil
	}
	n := len(cmd)
	for i := 0; i < n; i++ {
		c := cmd[i]
		switch {
		case c == '\'' || c == '"':
			inWord, quoted = true, true
			j := i + 1
			for j < n && cmd[j] != c {
				if c == '"' && j+1 < n && (cmd[j] == '`' || (cmd[j] == '\\' && strings.IndexByte("\"\\$`", cmd[j+1]) >= 0)) {
					j++
				}
				b.WriteByte(cmd[j])
				j++
			}
			i = j
		case c == '`' && i+1 < n && cmd[i+1] != '\n' && cmd[i+1] != '\r':
			inWord = true
			b.WriteByte(cmd[i+1])
			i++
		case c == ' ' || c == '\t' || c == '\r':
			flushWord()
		case c == ';' || c == '&' || c == '|' || c == '\n' || c == '(' || c == ')' || c == '{' || c == '}':
			flushSeg()
		default:
			inWord = true
			b.WriteByte(c)
		}
	}
	flushSeg()
	return segs
}

// baseCommand is a word's command name: no directory, no .exe/.cmd/.bat,
// lower case.
func baseCommand(w string) string {
	w = strings.ToLower(strings.ReplaceAll(w, `\`, "/"))
	w = path.Base(w)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		w = strings.TrimSuffix(w, ext)
	}
	return w
}

// envAssign reads NAME=value, $env:NAME=value, export/set NAME=value.
func envAssign(ws []word) (string, string, bool) {
	if len(ws) == 0 {
		return "", "", false
	}
	i := 0
	if w := strings.ToLower(ws[0].text); (w == "export" || w == "set" || w == "setx") && len(ws) > 1 {
		i = 1
	}
	t := ws[i].text
	name, val, ok := strings.Cut(t, "=")
	if !ok {
		// $env:NAME = value
		if i+2 < len(ws) && ws[i+1].text == "=" {
			name, val, ok = t, ws[i+2].text, true
		} else if i+1 < len(ws) && strings.HasPrefix(ws[i+1].text, "=") {
			name, val, ok = t, strings.TrimPrefix(ws[i+1].text, "="), true
		}
	}
	if !ok {
		return "", "", false
	}
	name = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(name), "$env:"), "$ENV:")
	if strings.HasPrefix(strings.ToLower(name), "$env:") {
		name = name[5:]
	}
	if name == "" || strings.ContainsAny(name, " /") {
		return "", "", false
	}
	return strings.ToUpper(name), strings.TrimSpace(val), true
}

// parseDBTCalls finds the dbt invocations of a command. A quoted word that
// itself holds a command (bash -c "...", pwsh -Command "...") is read too.
func parseDBTCalls(cmd string, depth int) []dbtCall {
	if depth > 3 || !strings.Contains(strings.ToLower(cmd), "dbt") {
		return nil
	}
	var out []dbtCall
	env := map[string]string{}
	for _, seg := range shellSegments(cmd) {
		for k, w := range seg {
			if k > 0 && w.quoted && shellFlags[strings.ToLower(seg[k-1].text)] && strings.Contains(strings.ToLower(w.text), "dbt") {
				out = append(out, parseDBTCalls(w.text, depth+1)...)
			}
		}
		// a whole-segment assignment: $env:DBT_TARGET = "prod", export/set X=v
		if first := strings.ToLower(seg[0].text); strings.HasPrefix(first, "$env:") || first == "export" || first == "set" || first == "setx" {
			if name, val, ok := envAssign(seg); ok {
				env[name] = val
			}
			continue
		}
		// leading inline assignments: DBT_TARGET=prod dbt run
		i := 0
		for i < len(seg) && reInlineAssign.MatchString(seg[i].text) {
			if name, val, ok := envAssign(seg[i : i+1]); ok {
				env[name] = val
			}
			i++
		}
		// dbt is the command, or comes anywhere after a wrapper
		// (uv run --with dbt-postgres dbt run, python -m dbt run).
		if i < len(seg) && baseCommand(seg[i].text) != "dbt" {
			if wrappers[baseCommand(seg[i].text)] {
				for i < len(seg) && baseCommand(seg[i].text) != "dbt" {
					i++
				}
			} else {
				i = len(seg)
			}
		}
		if i >= len(seg) {
			continue
		}
		c := dbtCall{target: env["DBT_TARGET"], profilesDir: env["DBT_PROFILES_DIR"], projectDir: env["DBT_PROJECT_DIR"], profile: env["DBT_PROFILE"]}
		if v := strings.ToLower(env["DBT_FULL_REFRESH"]); v == "1" || v == "true" || v == "yes" {
			c.fullRefresh = true
		}
		args := seg[i+1:]
		for j := 0; j < len(args); j++ {
			a := args[j].text
			next := func() string {
				if j+1 < len(args) {
					j++
					return args[j].text
				}
				return ""
			}
			switch {
			case c.sub == "" && dbtSubcommands[a]:
				c.sub = a
			case a == "--target" || a == "-t":
				c.target = next()
			case strings.HasPrefix(a, "--target="):
				c.target = strings.TrimPrefix(a, "--target=")
			case a == "--full-refresh" || a == "-f":
				c.fullRefresh = true
			case a == "--profiles-dir":
				c.profilesDir = next()
			case strings.HasPrefix(a, "--profiles-dir="):
				c.profilesDir = strings.TrimPrefix(a, "--profiles-dir=")
			case a == "--project-dir":
				c.projectDir = next()
			case strings.HasPrefix(a, "--project-dir="):
				c.projectDir = strings.TrimPrefix(a, "--project-dir=")
			case a == "--profile":
				c.profile = next()
			case strings.HasPrefix(a, "--profile="):
				c.profile = strings.TrimPrefix(a, "--profile=")
			}
		}
		out = append(out, c)
	}
	return out
}

// shellFlags precede a command string that a shell runs (bash -c "...",
// pwsh -Command "...", cmd /c "...").
var shellFlags = map[string]bool{"-c": true, "-lc": true, "-ic": true, "-command": true, "/c": true, "/k": true}

var reInlineAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// errProfiles is a profiles.yml the guard could not read safely.
var errProfiles = errors.New("profiles.yml")

// defaultTarget returns the default target of the call's profile from a
// profiles.yml inside the repository ("" when there is none). A refused or
// over-budget read is an error: the guard denies.
func defaultTarget(g packs.GuardCtx, c dbtCall, cfg dbtConfig) (string, error) {
	projectDir := strings.Trim(strings.ReplaceAll(c.projectDir, `\`, "/"), "/")
	if projectDir == "" || projectDir == "." {
		projectDir = cfg.Project
	}
	var dirs []string
	if c.profilesDir != "" {
		dirs = []string{strings.TrimRight(strings.ReplaceAll(c.profilesDir, `\`, "/"), "/")}
	} else {
		dirs = append(dirs, projectDir)
		if projectDir != "" {
			dirs = append(dirs, "")
		}
	}
	var raw []byte
	for _, d := range dirs {
		if d == "." {
			d = ""
		}
		b, err := g.ReadLocal(joinRel(d, "profiles.yml"), packs.ReadLimit)
		if err == nil {
			raw = b
			break
		}
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return "", fmt.Errorf("%w: %v", errProfiles, err)
	}
	if raw == nil {
		return "", nil
	}
	root, err := scan.ParseYAML(raw)
	if err != nil {
		return "", fmt.Errorf("%w: it does not parse", errProfiles)
	}
	profile := c.profile
	if profile == "" {
		if b, err := g.ReadLocal(joinRel(projectDir, "dbt_project.yml"), packs.ReadLimit); err == nil {
			if pr, err := scan.ParseYAML(b); err == nil {
				profile = scan.Scalar(scan.Get(pr, "profile"))
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("dbt_project.yml: %v", err)
		}
	}
	if profile != "" {
		return scan.Scalar(scan.Get(root, profile, "target")), nil
	}
	var targets []string
	for _, kv := range scan.Entries(root) {
		if kv.Key != "config" {
			targets = append(targets, scan.Scalar(scan.Get(kv.Value, "target")))
		}
	}
	if len(targets) == 1 {
		return targets[0], nil
	}
	// several profiles and no way to tell which: the strictest reading
	for _, t := range targets {
		if cfg.isProdTarget(t) {
			return t, nil
		}
	}
	return "", nil
}

// checkProdTarget is tq/dbt-prod-target: deny a writing dbt command whose
// target is one of dbt.prod_targets.
func checkProdTarget(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	calls := parseDBTCalls(call.Command, 0)
	if len(calls) == 0 {
		return policy.Allow, "", false
	}
	cfg := dbtSettings(g.Settings())
	for _, c := range calls {
		if !prodWriteCommands[c.sub] {
			continue
		}
		target := c.target
		if target == "" {
			t, err := defaultTarget(g, c, cfg)
			if err != nil {
				g.Count("dbt_prod_target:denied", 1)
				return policy.Deny, fmt.Sprintf("dbt %s with no --target: cannot read the default target (%v); pass --target %s", c.sub, err, cfg.DevTarget), true
			}
			target = t
		}
		if target != "" && cfg.isProdTarget(target) {
			g.Count("dbt_prod_target:denied", 1)
			return policy.Deny, fmt.Sprintf("dbt %s against the production target %q is denied for agents (packs.data.dbt.prod_targets); run it with --target %s, or ask the human to run it", c.sub, target, cfg.DevTarget), true
		}
	}
	return policy.Allow, "", false
}

// checkFullRefresh is tq/dbt-full-refresh: ask before --full-refresh on any
// target.
func checkFullRefresh(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	for _, c := range parseDBTCalls(call.Command, 0) {
		if c.fullRefresh && (fullRefreshCommands[c.sub] || c.sub == "") {
			g.Count("dbt_full_refresh:asked", 1)
			t := c.target
			if t == "" {
				t = "the default target"
			}
			return policy.Ask, fmt.Sprintf("dbt %s --full-refresh drops and rebuilds incremental tables on %s; confirm the rebuild and its cost first", strings.TrimSpace(c.sub), t), true
		}
	}
	return policy.Allow, "", false
}
