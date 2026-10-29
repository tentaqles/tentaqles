package data

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
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

// readOnlyCommands never rebuild a relation, so --full-refresh on them is
// not asked about. Any other subcommand (or none) with --full-refresh asks.
var readOnlyCommands = map[string]bool{"compile": true, "parse": true, "docs": true, "debug": true, "deps": true,
	"clean": true, "list": true, "ls": true, "source": true, "show": true, "init": true}

// dbtSubcommands are every dbt subcommand: the first positional word after
// `dbt` that is one of them is the subcommand (global options may come
// before it).
var dbtSubcommands = func() map[string]bool {
	m := map[string]bool{}
	for k := range prodWriteCommands {
		m[k] = true
	}
	for k := range readOnlyCommands {
		m[k] = true
	}
	return m
}()

// dbtValueOptions take a value as the next word, so that word is never the
// subcommand (dbt --log-level debug run: the subcommand is run).
var dbtValueOptions = map[string]bool{"--log-level": true, "--log-level-file": true, "--log-format": true,
	"--log-format-file": true, "--log-path": true, "--target-path": true, "--vars": true, "--threads": true,
	"-s": true, "--select": true, "--exclude": true, "--selector": true, "--state": true, "--defer-state": true,
	"--resource-type": true, "--resource-types": true, "--output": true, "-o": true, "--output-keys": true,
	"--args": true, "-m": true, "--models": true, "--inline": true, "--printer-width": true,
	"--warn-error-options": true, "-r": true, "--record-timing-info": true, "--event-time-start": true,
	"--event-time-end": true, "--limit": true}

// textOnly commands only print, search or record text: a segment they lead
// is never a dbt call (echo "dbt run --target prod", git commit -m "...").
var textOnly = map[string]bool{"echo": true, "printf": true, "write-host": true, "write-output": true,
	"write-error": true, "write-verbose": true, "write-warning": true, "git": true, "gh": true, "hg": true,
	"jj": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "findstr": true, "select-string": true,
	"sls": true, "cat": true, "type": true, "get-content": true, "gc": true, "ls": true, "dir": true,
	"get-childitem": true, "gci": true, "head": true, "tail": true, "less": true, "more": true, "wc": true,
	"man": true, "which": true, "where": true, "get-command": true, "test-path": true}

// argListFlags carry a Start-Process argument list (a string or a comma list).
var argListFlags = map[string]bool{"-argumentlist": true, "-args": true, "-arg": true}

// pathCommandPrefix: a quoted path with spaces is a command word after these.
var pathCommandPrefix = map[string]bool{"-filepath": true, "start-process": true, "saps": true, "start": true}

// dbtCall is one dbt invocation found in a shell command. Every value has
// had the variables set earlier in the command expanded; a value that still
// holds an expansion is dynamic (see dynamicValue).
type dbtCall struct {
	sub         string // the subcommand ("" when none was found)
	write       string // the first positional word that is a writing subcommand
	target      string // --target / -t, else DBT_TARGET set in the command
	fullRefresh bool
	profilesDir string
	projectDir  string
	profile     string
}

// writes reports a call that writes to its target, and the subcommand named.
// A writing word anywhere among the positional words counts, so an option
// value the parser does not know cannot hide the subcommand (fail closed).
func (c dbtCall) writes() (string, bool) {
	if prodWriteCommands[c.sub] {
		return c.sub, true
	}
	if c.write != "" {
		return c.write, true
	}
	return "", false
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
			// ${NAME} is a variable, not a brace group
			if c == '{' && i > 0 && cmd[i-1] == '$' {
				if end := strings.IndexByte(cmd[i:], '}'); end > 0 {
					inWord = true
					b.WriteString(cmd[i : i+end+1])
					i += end
					continue
				}
			}
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
	name = strings.TrimSpace(name)
	if strings.HasPrefix(strings.ToLower(name), "$env:") {
		name = name[5:]
	}
	if name == "" || strings.ContainsAny(name, " /") {
		return "", "", false
	}
	return strings.ToUpper(name), strings.TrimSpace(val), true
}

// reVarRef is $NAME, ${NAME}, $env:NAME and %NAME%.
var reVarRef = regexp.MustCompile(`\$\{(?i:env:)?([A-Za-z_][A-Za-z0-9_]*)\}|\$(?i:env:)?([A-Za-z_][A-Za-z0-9_]*)|%([A-Za-z_][A-Za-z0-9_]*)%`)

// expandVars replaces the variables a command set before (env) in v; any
// other reference is left as it is, so dynamicValue sees it.
func expandVars(v string, env map[string]string) string {
	if !strings.ContainsAny(v, "$%") {
		return v
	}
	return reVarRef.ReplaceAllStringFunc(v, func(m string) string {
		sm := reVarRef.FindStringSubmatch(m)
		name := sm[1] + sm[2] + sm[3]
		if val, ok := env[strings.ToUpper(name)]; ok && !dynamicValue(val) {
			return val
		}
		return m
	})
}

// dynamicValue reports a value the shell computes when it runs ($VAR,
// $(...), %VAR%, a backtick): the guard cannot know it.
func dynamicValue(v string) bool {
	return strings.ContainsAny(v, "$`") || reVarRef.MatchString(v)
}

// dbtCommandAt reports whether seg[k] runs dbt: its command name is dbt (or
// dbt.cli.main after python -m). A quoted word with spaces counts only as a
// path in command position (& "C:\Program Files\dbt\dbt.exe").
func dbtCommandAt(seg []word, k, first int) bool {
	w := seg[k].text
	if b := baseCommand(w); b != "dbt" && b != "dbt.cli.main" {
		return false
	}
	if strings.ContainsAny(w, " \t\n") {
		return k == first || (k > 0 && pathCommandPrefix[strings.ToLower(seg[k-1].text)])
	}
	return true
}

// parseDBTCalls finds the dbt invocations of a command. A segment is a dbt
// call when any word in it runs dbt (timeout 600 dbt run, cmd /c dbt run,
// docker compose run dbt dbt run), unless the segment's command only prints
// or searches text. A quoted word with spaces that mentions dbt (bash -c
// "...", ssh host "...") is read as a command line too, with the variables
// set so far. env holds the variables set by an enclosing command.
func parseDBTCalls(cmd string, depth int, inherited map[string]string) []dbtCall {
	if depth > 3 || !strings.Contains(strings.ToLower(cmd), "dbt") {
		return nil
	}
	env := make(map[string]string, len(inherited))
	for k, v := range inherited {
		env[k] = v
	}
	var out []dbtCall
	for _, seg := range shellSegments(cmd) {
		// a whole-segment assignment: $env:DBT_TARGET = "prod", export/set X=v
		if first := strings.ToLower(seg[0].text); strings.HasPrefix(first, "$env:") || first == "export" || first == "set" || first == "setx" {
			if name, val, ok := envAssign(seg); ok {
				env[name] = expandVars(val, env)
			}
			continue
		}
		// leading inline assignments: DBT_TARGET=prod dbt run, T=prod
		i := 0
		for i < len(seg) && reInlineAssign.MatchString(seg[i].text) {
			if name, val, ok := envAssign(seg[i : i+1]); ok {
				env[name] = expandVars(val, env)
			}
			i++
		}
		if i >= len(seg) || textOnly[baseCommand(seg[i].text)] {
			continue
		}
		at := -1
		for k := i; k < len(seg); k++ {
			w := seg[k]
			if at < 0 && dbtCommandAt(seg, k, i) {
				at = k
				continue
			}
			if w.quoted && strings.ContainsAny(w.text, " \t\n") && strings.Contains(strings.ToLower(w.text), "dbt") {
				out = append(out, parseDBTCalls(w.text, depth+1, env)...)
			}
		}
		if at < 0 {
			continue
		}
		out = append(out, readDBTArgs(dbtArgs(seg, i, at), env))
	}
	return out
}

// dbtArgs are the words after the dbt word, plus a Start-Process argument
// list wherever it stands (-ArgumentList 'run --target prod' or 'run','-t','prod').
func dbtArgs(seg []word, first, at int) []string {
	var args []string
	for k := first; k < len(seg); k++ {
		if k == at {
			continue
		}
		if k > first && argListFlags[strings.ToLower(seg[k-1].text)] {
			for _, s := range shellSegments(strings.ReplaceAll(seg[k].text, ",", " ")) {
				for _, w := range s {
					args = append(args, w.text)
				}
			}
			continue
		}
		if k > at && !argListFlags[strings.ToLower(seg[k].text)] {
			args = append(args, seg[k].text)
		}
	}
	return args
}

// readDBTArgs reads the options and subcommand of one dbt call.
func readDBTArgs(args []string, env map[string]string) dbtCall {
	c := dbtCall{target: env["DBT_TARGET"], profilesDir: env["DBT_PROFILES_DIR"], projectDir: env["DBT_PROJECT_DIR"], profile: env["DBT_PROFILE"]}
	if v := strings.ToLower(env["DBT_FULL_REFRESH"]); v == "1" || v == "true" || v == "yes" {
		c.fullRefresh = true
	}
	for j := 0; j < len(args); j++ {
		a := args[j]
		next := func() string {
			if j+1 < len(args) {
				j++
				return args[j]
			}
			return ""
		}
		switch {
		case a == "--target" || a == "-t":
			c.target = next()
		case strings.HasPrefix(a, "--target="):
			c.target = strings.TrimPrefix(a, "--target=")
		case strings.HasPrefix(a, "-t") && !strings.HasPrefix(a, "--"):
			// click accepts an attached short value: -tprod
			c.target = strings.TrimPrefix(a[2:], "=")
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
		case dbtValueOptions[a]:
			next()
		case strings.HasPrefix(a, "-"):
			// a flag, or --name=value
		default:
			if c.sub == "" && dbtSubcommands[a] {
				c.sub = a
			}
			if c.write == "" && prodWriteCommands[a] {
				c.write = a
			}
		}
	}
	c.target = expandVars(c.target, env)
	c.profilesDir = expandVars(c.profilesDir, env)
	c.projectDir = expandVars(c.projectDir, env)
	c.profile = expandVars(c.profile, env)
	return c
}

var reInlineAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// errProfiles is a profiles.yml the guard could not read safely.
var errProfiles = errors.New("profiles.yml")

// errOutsideRepo is a --profiles-dir or --project-dir outside the repository
// (absolute, ~, $HOME, %USERPROFILE%): a profiles.yml there is never read.
var errOutsideRepo = errors.New("outside the repository, where profiles.yml is never read")

// repoDir turns a --profiles-dir / --project-dir value into a path relative
// to the repository root, or errOutsideRepo.
func repoDir(g packs.GuardCtx, dir string) (string, error) {
	d := strings.TrimRight(strings.ReplaceAll(dir, `\`, "/"), "/")
	if d == "" || d == "." {
		return "", nil
	}
	if strings.HasPrefix(d, "~") || dynamicValue(d) {
		return "", errOutsideRepo
	}
	native := filepath.FromSlash(d)
	if filepath.IsAbs(native) || strings.HasPrefix(d, "/") || filepath.VolumeName(native) != "" {
		root := g.RepoRoot()
		if root == "" {
			return "", errOutsideRepo
		}
		rel, err := filepath.Rel(root, native)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", errOutsideRepo
		}
		d = filepath.ToSlash(rel)
		if d == "." {
			return "", nil
		}
	}
	return strings.TrimPrefix(d, "./"), nil
}

// defaultTarget returns the default target of the call's profile from a
// profiles.yml inside the repository ("" when there is none). A refused or
// over-budget read, a directory outside the repository and a profile name
// the shell computes are errors: the guard denies.
func defaultTarget(g packs.GuardCtx, c dbtCall, cfg dbtConfig) (string, error) {
	if dynamicValue(c.profile) {
		return "", fmt.Errorf("--profile %s is not a literal", c.profile)
	}
	projectDir, err := repoDir(g, c.projectDir)
	if err != nil {
		return "", fmt.Errorf("--project-dir is %w", err)
	}
	if projectDir == "" {
		projectDir = cfg.Project
	}
	var dirs []string
	if c.profilesDir != "" {
		d, err := repoDir(g, c.profilesDir)
		if err != nil {
			return "", fmt.Errorf("--profiles-dir is %w", err)
		}
		dirs = []string{d}
	} else {
		dirs = append(dirs, projectDir)
		if projectDir != "" {
			dirs = append(dirs, "")
		}
	}
	var raw []byte
	for _, d := range dirs {
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
// target is one of dbt.prod_targets, or cannot be known.
func checkProdTarget(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	calls := parseDBTCalls(call.Command, 0, nil)
	if len(calls) == 0 {
		return policy.Allow, "", false
	}
	cfg := dbtSettings(g.Settings())
	for _, c := range calls {
		sub, ok := c.writes()
		if !ok {
			continue
		}
		target := c.target
		if dynamicValue(target) {
			g.Count("dbt_prod_target:denied", 1)
			return policy.Deny, fmt.Sprintf("dbt %s --target %s: the shell computes the target, so the guard cannot check it against packs.data.dbt.prod_targets; pass a literal --target %s", sub, target, cfg.DevTarget), true
		}
		if target == "" {
			t, err := defaultTarget(g, c, cfg)
			if err != nil {
				g.Count("dbt_prod_target:denied", 1)
				return policy.Deny, fmt.Sprintf("dbt %s with no --target: cannot read the default target (%v); pass --target %s", sub, err, cfg.DevTarget), true
			}
			target = t
		}
		if target != "" && cfg.isProdTarget(target) {
			g.Count("dbt_prod_target:denied", 1)
			return policy.Deny, fmt.Sprintf("dbt %s against the production target %q is denied for agents (packs.data.dbt.prod_targets); run it with --target %s, or ask the human to run it", sub, target, cfg.DevTarget), true
		}
	}
	return policy.Allow, "", false
}

// checkFullRefresh is tq/dbt-full-refresh: ask before --full-refresh on any
// target and any subcommand that can rebuild a relation.
func checkFullRefresh(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	for _, c := range parseDBTCalls(call.Command, 0, nil) {
		sub, writes := c.writes()
		if !writes {
			sub = c.sub
		}
		if c.fullRefresh && (writes || !readOnlyCommands[sub]) {
			g.Count("dbt_full_refresh:asked", 1)
			t := c.target
			if t == "" {
				t = "the default target"
			}
			return policy.Ask, fmt.Sprintf("dbt %s --full-refresh drops and rebuilds incremental tables on %s; confirm the rebuild and its cost first", strings.TrimSpace(sub), t), true
		}
	}
	return policy.Allow, "", false
}
