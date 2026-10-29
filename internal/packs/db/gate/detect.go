// Package gate holds the db pack's decision guards (spec D §14, §16):
// tq/db-migrate-env resolves the target environment of a migration command
// and applies its migrate policy, and tq/restore-target denies a restore
// into a declared environment or a non-local host. Both are pure: they read
// the command, the hook's own environment variables and small local files
// through GuardCtx, and run no process and no network call.
package gate

import (
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	cgate "github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// Leaf is one simple command of a shell line, with its wrappers removed.
type Leaf struct {
	Raw  string   // the segment text
	Prog string   // the program, normalised (psql, supabase, prisma …); "" none
	Args []string // the words after the program
	// Env are the variables set for this command: an inline NAME=value
	// prefix, env NAME=value, or an earlier export / $env: on the line.
	Env map[string]string
	// Dotenv are the files of a tq dotenv run wrapper (default .env).
	Dotenv  []string
	Dotenvs bool
	// Opaque: a payload tq cannot open (a script file, an unknown wrapper,
	// docker exec, ssh, an undecodable -EncodedCommand).
	Opaque bool
	// Unknown: an earlier statement changed the environment in a way tq
	// does not model (source, unset, set -a, a computed export).
	Unknown bool
	// Stdin: the segment reads a redirect (psql < dump.sql).
	Stdin bool
	// Piped: the line has a pipe, so the segment may read another
	// command's output (gunzip -c x.gz | psql). Conservative: any single |
	// on the line, quoted or not.
	Piped bool
	// Dir is the folder an earlier literal cd / pushd / Set-Location moved
	// to ("" none), relative to the session's folder unless absolute.
	Dir string
	// Moved: an earlier statement changed the folder in a way tq does not
	// model (cd $X, cd -, cd ~, popd).
	Moved bool
}

// Words is Prog followed by Args.
func (l Leaf) Words() []string {
	if l.Prog == "" {
		return l.Args
	}
	return append([]string{l.Prog}, l.Args...)
}

// Text is the leaf's words joined, lower case.
func (l Leaf) Text() string { return strings.ToLower(strings.Join(l.Words(), " ")) }

const maxDepth = 4

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
var psEnvRe = regexp.MustCompile(`(?i)^\$(?:env:([A-Za-z_][A-Za-z0-9_]*)|\{env:([A-Za-z_][A-Za-z0-9_]*)\})(=(.*))?$`)
var envProviderRe = regexp.MustCompile(`(?i)^(-path:?)?env:|setenvironmentvariable`)

type state struct {
	env     map[string]string
	unknown bool
	pipe    bool
	dir     string
	moved   bool
}

// hasPipe reports a single | anywhere on the line (|| is not a pipe).
func hasPipe(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '|' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '|' {
			i++
			continue
		}
		return true
	}
	return false
}

// cdProgs change the folder.
var cdProgs = map[string]bool{"cd": true, "chdir": true, "pushd": true, "set-location": true, "sl": true, "push-location": true,
	"popd": true, "pop-location": true}

// changeDir applies a cd-like statement: a literal folder is followed, any
// other form makes the folder unknown.
func (st *state) changeDir(prog string, args []string) {
	if prog == "popd" || prog == "pop-location" {
		st.moved = true
		return
	}
	var dirs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch la := strings.ToLower(a); {
		case la == "-path" || la == "-literalpath" || la == "-lp":
			if i+1 < len(args) {
				dirs = append(dirs, args[i+1])
				i++
			}
		case strings.HasPrefix(la, "-path:") || strings.HasPrefix(la, "-literalpath:"):
			_, v, _ := strings.Cut(a, ":")
			dirs = append(dirs, v)
		case a == "-L" || a == "-P" || a == "-e" || a == "-@" || la == "-passthru":
		default:
			dirs = append(dirs, a)
		}
	}
	if len(dirs) != 1 {
		st.moved = true // cd alone goes home; two folders tq does not read
		return
	}
	d := strings.ReplaceAll(dirs[0], `\`, "/")
	if computed(d) || d == "-" || strings.HasPrefix(d, "~") || strings.HasPrefix(d, "-") {
		st.moved = true
		return
	}
	if isAbs(d) || st.dir == "" {
		st.dir = d
		return
	}
	st.dir = path.Join(st.dir, d)
}

// isAbs: a slash path or a drive letter path (C:/x).
func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 3 && p[1] == ':' && p[2] == '/')
}

func computed(v string) bool { return v == "" || strings.ContainsAny(v, "$`%") }

// Analyze reads every simple command of a bash or PowerShell line.
func Analyze(cmd string, powershell bool) []Leaf {
	cmd = guard.BlankDataHeredocs(cmd)
	if powershell {
		// a backslash is a plain character in PowerShell (C:\dumps\x.sql)
		cmd = strings.ReplaceAll(cmd, `\`, "/")
	}
	st := &state{env: map[string]string{}, pipe: hasPipe(cmd)}
	var out []Leaf
	for _, seg := range cgate.Split(cmd) {
		out = append(out, analyzeSeg(seg, st, 0)...)
	}
	return out
}

func texts(ws []cgate.Word) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Text
	}
	return out
}

func analyzeSeg(seg cgate.Segment, st *state, depth int) []Leaf {
	w := texts(seg.Words)
	if len(w) == 0 {
		return nil
	}
	stdin := strings.Contains(seg.Raw, "<")
	leaf := func() []Leaf {
		return []Leaf{{Raw: seg.Raw, Args: w, Unknown: st.unknown, Piped: st.pipe, Dir: st.dir, Moved: st.moved}}
	}
	for _, x := range w {
		if envProviderRe.MatchString(x) {
			st.unknown = true
		}
	}
	if n := cgate.ToolName(w[0]); cdProgs[n] {
		st.changeDir(n, w[1:])
		return leaf()
	}
	switch cgate.ToolName(w[0]) {
	case "export":
		for _, x := range w[1:] {
			if assignRe.MatchString(x) {
				k, v, _ := strings.Cut(x, "=")
				st.env[strings.ToUpper(k)] = v
				if computed(v) {
					st.unknown = true
				}
			} else {
				st.unknown = true
			}
		}
		return leaf()
	case "source", ".", "declare", "typeset", "unset", "setenv", "direnv", "import-module", "ipmo":
		st.unknown = true
		return leaf()
	case "set":
		// set -a (allexport) exports every later assignment
		for _, x := range w[1:] {
			if (strings.HasPrefix(x, "-") && !strings.HasPrefix(x, "--") && strings.Contains(x, "a")) || strings.EqualFold(x, "allexport") {
				st.unknown = true
			}
		}
		return leaf()
	}
	if m := psEnvRe.FindStringSubmatch(w[0]); m != nil {
		v := m[4]
		if m[3] == "" && len(w) >= 3 && w[1] == "=" {
			v = strings.Join(w[2:], " ")
		}
		st.env[strings.ToUpper(m[1]+m[2])] = v
		if computed(v) || len(w) > 3 {
			st.unknown = true
		}
		return leaf()
	}
	all := true
	for _, x := range w {
		if !assignRe.MatchString(x) {
			all = false
			break
		}
	}
	if all {
		for _, x := range w {
			k, v, _ := strings.Cut(x, "=")
			st.env[strings.ToUpper(k)] = v
			if computed(v) {
				st.unknown = true
			}
		}
		return leaf()
	}
	l := Leaf{Raw: seg.Raw, Env: map[string]string{}, Unknown: st.unknown, Stdin: stdin, Piped: st.pipe, Dir: st.dir, Moved: st.moved}
	for k, v := range st.env {
		l.Env[k] = v
	}
	return unwrap(l, w, st, depth)
}

func nested(l Leaf, script string, st *state, depth int, ps bool) []Leaf {
	if depth >= maxDepth {
		l.Opaque = true
		return []Leaf{l}
	}
	sub := &state{env: map[string]string{}, unknown: l.Unknown || st.unknown, pipe: st.pipe || hasPipe(script), dir: l.Dir, moved: l.Moved}
	for k, v := range l.Env {
		sub.env[k] = v
	}
	script = guard.BlankDataHeredocs(script)
	if ps {
		script = strings.ReplaceAll(script, `\`, "/")
	}
	var out []Leaf
	for _, seg := range cgate.Split(script) {
		for _, x := range analyzeSeg(seg, sub, depth+1) {
			if l.Dotenvs && !x.Dotenvs {
				x.Dotenvs, x.Dotenv = true, append([]string(nil), l.Dotenv...)
			}
			out = append(out, x)
		}
	}
	return out
}

// runners whose next word is a package (npx prisma, pnpm dlx drizzle-kit).
func pkgRunner(w []string, i int) (int, bool) {
	n := cgate.ToolName(w[i])
	j := i + 1
	switch n {
	case "npx", "bunx", "uvx", "pipx":
		if n == "pipx" {
			if j < len(w) && w[j] == "run" {
				j++
			} else {
				return 0, false
			}
		}
	case "pnpm", "yarn", "bun", "npm", "uv", "poetry", "pipenv", "bundle", "pdm", "hatch":
		if j >= len(w) {
			return 0, false
		}
		switch w[j] {
		case "exec", "dlx", "x", "run":
			j++
		default:
			if n == "yarn" || n == "pnpm" {
				return j, true // yarn prisma migrate deploy
			}
			return 0, false
		}
	default:
		return 0, false
	}
	for j < len(w) && strings.HasPrefix(w[j], "-") {
		if w[j] == "--" {
			j++
			break
		}
		if w[j] == "-p" || w[j] == "--package" || w[j] == "--with" || w[j] == "--from" {
			j++
		}
		j++
	}
	return j, true
}

func unwrap(l Leaf, w []string, st *state, depth int) []Leaf {
	i := 0
	for i < len(w) {
		t := w[i]
		if assignRe.MatchString(t) {
			k, v, _ := strings.Cut(t, "=")
			l.Env[strings.ToUpper(k)] = v
			if computed(v) {
				l.Unknown = true
			}
			i++
			continue
		}
		name := cgate.ToolName(t)
		if j, ok := pkgRunner(w, i); ok {
			i = j
			continue
		}
		switch name {
		case "env":
			i++
			for i < len(w) && strings.HasPrefix(w[i], "-") {
				if w[i] == "--" {
					i++
					break
				}
				l.Unknown = true // -i, -u NAME, -S: the environment changes
				if w[i] == "-u" || w[i] == "--unset" {
					i++
				}
				i++
			}
			continue
		case "tq":
			if i+2 < len(w) && w[i+1] == "dotenv" && w[i+2] == "run" {
				i += 3
				l.Dotenvs = true
				var files []string
				for i < len(w) && w[i] != "--" {
					switch x := w[i]; {
					case x == "--file" && i+1 < len(w):
						files = append(files, w[i+1])
						i += 2
					case strings.HasPrefix(x, "--file="):
						files = append(files, strings.TrimPrefix(x, "--file="))
						i++
					default:
						l.Opaque = true
						i = len(w)
					}
				}
				if len(files) == 0 {
					files = []string{".env"}
				}
				l.Dotenv = append(l.Dotenv, files...)
				i++
				continue
			}
		case "sudo", "doas", "nohup", "time", "nice", "command", "builtin", "exec", "stdbuf", "&":
			i++
			for i < len(w) && strings.HasPrefix(w[i], "-") {
				if (name == "sudo" && (w[i] == "-u" || w[i] == "-g")) || (name == "nice" && w[i] == "-n") {
					i++
				}
				i++
			}
			continue
		case "timeout":
			i++
			for i < len(w) && strings.HasPrefix(w[i], "-") {
				i++
			}
			i++
			continue
		case "python", "python3", "py":
			// python manage.py migrate, python -m alembic upgrade
			j := i + 1
			for j < len(w) && strings.HasPrefix(w[j], "-") && w[j] != "-m" && w[j] != "-c" {
				j++
			}
			switch {
			case j+1 < len(w) && w[j] == "-m":
				i = j + 1
				continue
			case j < len(w) && w[j] == "-c":
				l.Opaque = true
				l.Prog, l.Args = name, w[i+1:]
				return []Leaf{l}
			}
		case "bash", "sh", "zsh", "dash", "ksh":
			for j := i + 1; j < len(w); j++ {
				x := w[j]
				if strings.HasPrefix(x, "-") && !strings.HasPrefix(x, "--") && strings.Contains(x, "c") {
					if j+1 < len(w) {
						return nested(l, w[j+1], st, depth, false)
					}
					break
				}
				if !strings.HasPrefix(x, "-") {
					break
				}
			}
			l.Opaque = true
			l.Prog, l.Args = name, w[i+1:]
			return []Leaf{l}
		case "pwsh", "powershell":
			run := scan.PowerShell(t, w[i+1:])
			if run.Opaque != "" || run.Script == "" {
				l.Opaque = run.Opaque != ""
				l.Prog, l.Args = name, w[i+1:]
				return []Leaf{l}
			}
			return nested(l, run.Script, st, depth, true)
		case "cmd":
			for j := i + 1; j < len(w); j++ {
				if x := strings.ToLower(w[j]); x == "/c" || x == "/k" {
					return nested(l, strings.Join(w[j+1:], " "), st, depth, true)
				}
			}
			l.Opaque = true
			l.Prog, l.Args = name, w[i+1:]
			return []Leaf{l}
		case "eval", "iex", "invoke-expression":
			j := i + 1
			if j < len(w) && strings.EqualFold(w[j], "-command") {
				j++
			}
			script := strings.Join(w[j:], " ")
			if strings.ContainsAny(script, "$`") {
				st.unknown, l.Unknown = true, true
			}
			return nested(l, script, st, depth, name != "eval")
		case "xargs", "start-process", "saps", "start", "watch", "parallel", "ssh", "docker", "podman", "kubectl", "nerdctl", "make", "just", "task":
			l.Opaque = true
			l.Prog, l.Args = name, w[i+1:]
			return []Leaf{l}
		}
		l.Prog, l.Args = name, w[i+1:]
		if name == "supabase" {
			l.Args = supabaseArgs(l.Args)
		}
		return []Leaf{l}
	}
	return []Leaf{l}
}

// supabaseGlobals are the Supabase CLI's global flags that take a value.
var supabaseGlobals = map[string]bool{"--workdir": true, "--profile": true, "--output": true, "-o": true, "--dns-resolver": true, "--network-id": true}

// supabaseArgs moves the global flags written before the subcommand
// (supabase --workdir x db push) after it, so the line reads as
// supabase db push --workdir x.
func supabaseArgs(args []string) []string {
	var lead []string
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		lead = append(lead, args[i])
		if supabaseGlobals[args[i]] && i+1 < len(args) {
			lead = append(lead, args[i+1])
			i++
		}
		i++
	}
	if len(lead) == 0 {
		return args
	}
	return append(append([]string(nil), args[i:]...), lead...)
}

// Flag returns the value of the first of names (--name v, --name=v, -n v,
// /n:v), and whether the flag is present.
func (l Leaf) Flag(names ...string) (string, bool) {
	for i, a := range l.Args {
		for _, n := range names {
			switch {
			case a == n:
				if i+1 < len(l.Args) && !strings.HasPrefix(l.Args[i+1], "-") {
					return l.Args[i+1], true
				}
				return "", true
			case strings.HasPrefix(a, n+"=") || (strings.HasPrefix(n, "/") && strings.HasPrefix(strings.ToLower(a), strings.ToLower(n)+":")):
				return a[len(n)+1:], true
			}
		}
	}
	return "", false
}

// Has reports a flag.
func (l Leaf) Has(names ...string) bool {
	_, ok := l.Flag(names...)
	return ok
}
