package gate

import (
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// Op is what a command does to infrastructure.
type Op int

const (
	OpNone       Op = iota // not an IaC tool of the gate
	OpOther                // a gate tool, a verb that changes nothing the gate owns (show, init, s3 ls)
	OpDry                  // a dry run: plan, preview, what-if, synth, diff, --dry-run
	OpApply                // an apply (§9.1)
	OpDestroy              // a destroy (§9.1)
	OpUnreadable           // an apply the parser cannot read: it counts as an apply to the default environment
)

func (o Op) String() string {
	return [...]string{"none", "other", "dry-run", "apply", "destroy", "unreadable"}[o]
}

// Tools are the CLIs the gate reads.
var Tools = map[string]bool{"terraform": true, "tofu": true, "pulumi": true, "az": true, "cdk": true, "cdktf": true, "aws": true, "sam": true}

// Invocation is one simple command, read.
type Invocation struct {
	Raw    string // the leaf segment text
	Tool   string // a key of Tools, or "" (not a gate tool)
	ToolAt int    // index of the tool word in Words
	Words  []Word
	Op     Op
	// AlsoDestroys marks an apply that also destroys (az --mode Complete).
	AlsoDestroys bool
	Verb         []string            // the subcommand words: [deployment group create]
	Pos          []string            // positionals after the verb
	Flags        map[string][]string // flag name without dashes, lower case -> values ("" for a switch)
	Env          map[string]string   // variables set for this command (prefix, env, an earlier export or $env:)
	Dotenv       []string            // tq dotenv run --file files (default .env)
	Dotenvs      bool                // behind tq dotenv run
	Dir          string              // the folder an earlier cd moved to, relative ("" none, "?" unknown)
	// Opaque marks a command whose payload the parser cannot open (a script
	// file, an unknown wrapper flag, an undecodable -EncodedCommand).
	Opaque bool
}

// Flag returns the first value of any of the names.
func (iv Invocation) Flag(names ...string) (string, bool) {
	for _, n := range names {
		if v, ok := iv.Flags[n]; ok {
			if len(v) == 0 {
				return "", true
			}
			return v[0], true
		}
	}
	return "", false
}

// FlagAll returns every value of the names, in order.
func (iv Invocation) FlagAll(names ...string) []string {
	var out []string
	for _, n := range names {
		out = append(out, iv.Flags[n]...)
	}
	return out
}

// Has reports a flag.
func (iv Invocation) Has(names ...string) bool {
	for _, n := range names {
		if _, ok := iv.Flags[n]; ok {
			return true
		}
	}
	return false
}

type state struct {
	dir string
	env map[string]string
}

// ToolName normalises a command word: its base name, lower case, without
// .exe/.cmd/.bat/.ps1.
func ToolName(w string) string {
	w = strings.ReplaceAll(w, `\`, "/")
	w = strings.ToLower(path.Base(w))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		w = strings.TrimSuffix(w, ext)
	}
	return w
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
var psEnvRe = regexp.MustCompile(`(?i)^\$env:([A-Za-z_][A-Za-z0-9_]*)(=(.*))?$`)

// Analyze reads every simple command of a shell line, with the cd and
// variable state the line builds up, and the commands nested in bash -c,
// pwsh -Command / -EncodedCommand, cmd /c, eval and $(...).
func Analyze(cmd string) []Invocation { return AnalyzeShell(cmd, false) }

// AnalyzeShell is Analyze for a bash (powershell false) or PowerShell line:
// in PowerShell a backslash is a plain character (C:\infra\plan).
func AnalyzeShell(cmd string, powershell bool) []Invocation {
	cmd = guard.BlankDataHeredocs(cmd)
	st := &state{env: map[string]string{}}
	var out []Invocation
	for _, seg := range splitShell(cmd, 0, powershell) {
		out = append(out, analyzeSeg(seg, st, 0)...)
	}
	return out
}

// AnalyzeArgv reads one argv (no shell).
func AnalyzeArgv(argv []string) []Invocation {
	return analyzeSeg(FromArgv(argv), &state{env: map[string]string{}}, 0)
}

func analyzeSeg(seg Segment, st *state, depth int) []Invocation {
	w := seg.Words
	if len(w) == 0 {
		return nil
	}
	head := ToolName(w[0].Text)
	// cd and friends move the line's folder.
	switch head {
	case "cd", "set-location", "sl", "chdir", "pushd", "push-location":
		arg := ""
		for _, x := range w[1:] {
			if !strings.HasPrefix(x.Text, "-") {
				arg = x.Text
				break
			}
		}
		st.dir = joinDir(st.dir, arg)
		return []Invocation{{Raw: seg.Raw, Words: w}}
	case "export":
		for _, x := range w[1:] {
			if assignRe.MatchString(x.Text) {
				k, v, _ := strings.Cut(x.Text, "=")
				st.env[strings.ToUpper(k)] = v
			}
		}
		return []Invocation{{Raw: seg.Raw, Words: w}}
	}
	// PowerShell $env:NAME = value
	if m := psEnvRe.FindStringSubmatch(w[0].Text); m != nil {
		v := m[3]
		if m[2] == "" && len(w) >= 3 && w[1].Text == "=" {
			v = w[2].Text
		} else if m[2] == "" && len(w) == 2 && strings.HasPrefix(w[1].Text, "=") {
			v = strings.TrimPrefix(w[1].Text, "=")
		}
		st.env[strings.ToUpper(m[1])] = v
		return []Invocation{{Raw: seg.Raw, Words: w}}
	}
	// a line of bare assignments sets shell variables for the rest
	allAssign := true
	for _, x := range w {
		if !assignRe.MatchString(x.Text) {
			allAssign = false
			break
		}
	}
	if allAssign {
		for _, x := range w {
			k, v, _ := strings.Cut(x.Text, "=")
			st.env[strings.ToUpper(k)] = v
		}
		return []Invocation{{Raw: seg.Raw, Words: w}}
	}
	iv := Invocation{Raw: seg.Raw, Words: w, Env: map[string]string{}, Dir: st.dir}
	for k, v := range st.env {
		iv.Env[k] = v
	}
	return unwrap(iv, 0, st, depth)
}

func joinDir(cur, arg string) string {
	if cur == "?" {
		return "?"
	}
	a := strings.ReplaceAll(strings.Trim(arg, `"'`), `\`, "/")
	switch {
	case a == "" || a == "~" || a == "-":
		return "?"
	case strings.HasPrefix(a, "/") || strings.HasPrefix(a, "~") || strings.Contains(a, "$") || strings.Contains(a, "%") || (len(a) > 1 && a[1] == ':'):
		return "?"
	}
	p := path.Clean(path.Join(cur, a))
	if p == ".." || strings.HasPrefix(p, "../") {
		return "?"
	}
	return p
}

// nested parses a script string (bash -c, pwsh -Command, eval) as its own
// line, inheriting the variables and folder.
func nested(iv Invocation, script string, st *state, depth int, ps bool) []Invocation {
	if depth >= maxDepth {
		iv.Opaque = true
		return []Invocation{iv}
	}
	sub := &state{dir: iv.Dir, env: map[string]string{}}
	for k, v := range iv.Env {
		sub.env[k] = v
	}
	var out []Invocation
	for _, seg := range splitShell(guard.BlankDataHeredocs(script), depth+1, ps) {
		got := analyzeSeg(seg, sub, depth+1)
		for i := range got {
			if got[i].Env == nil {
				continue
			}
			if iv.Dotenvs && !got[i].Dotenvs {
				got[i].Dotenvs, got[i].Dotenv = true, append([]string(nil), iv.Dotenv...)
			}
		}
		out = append(out, got...)
	}
	return out
}

func joinWords(ws []Word) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

// unwrap strips the wrappers in front of the tool (§9.1) and reads it.
func unwrap(iv Invocation, i int, st *state, depth int) []Invocation {
	w := iv.Words
	for i < len(w) {
		t := w[i].Text
		if assignRe.MatchString(t) {
			k, v, _ := strings.Cut(t, "=")
			iv.Env[strings.ToUpper(k)] = v
			i++
			continue
		}
		name := ToolName(t)
		switch {
		case Tools[name]:
			iv.Tool, iv.ToolAt = name, i
			parseTool(&iv)
			return []Invocation{iv}
		case name == "env":
			i++
			for i < len(w) {
				x := w[i].Text
				if x == "-u" || x == "--unset" {
					i += 2
					continue
				}
				if x == "-i" || x == "--ignore-environment" || x == "-" || x == "--" {
					i++
					continue
				}
				if strings.HasPrefix(x, "-") {
					iv.Opaque = true
					return []Invocation{iv}
				}
				break
			}
		case name == "tq":
			if i+2 < len(w) && w[i+1].Text == "dotenv" && w[i+2].Text == "run" {
				i += 3
				iv.Dotenvs = true
				var files []string
				for i < len(w) && w[i].Text != "--" {
					x := w[i].Text
					switch {
					case x == "--file" && i+1 < len(w):
						files = append(files, w[i+1].Text)
						i += 2
					case strings.HasPrefix(x, "--file="):
						files = append(files, strings.TrimPrefix(x, "--file="))
						i++
					default:
						// the command itself without --: tq dotenv run needs --
						iv.Opaque = true
						i = len(w)
					}
				}
				if len(files) == 0 {
					files = []string{".env"}
				}
				iv.Dotenv = append(iv.Dotenv, files...)
				i++ // the --
				continue
			}
			return []Invocation{iv}
		case name == "npx" || name == "bunx":
			i = skipRunnerFlags(&iv, i+1, st, depth)
			if i < 0 {
				return []Invocation{iv}
			}
		case name == "pnpm" || name == "yarn" || name == "bun" || name == "npm":
			j := i + 1
			if j < len(w) {
				sub := w[j].Text
				switch {
				case (name == "pnpm" && (sub == "exec" || sub == "dlx" || sub == "x")) ||
					(name == "yarn" && (sub == "dlx" || sub == "exec" || sub == "run")) ||
					(name == "bun" && sub == "x") || (name == "npm" && (sub == "exec" || sub == "x")):
					j++
				case name == "npm" || name == "bun":
					return []Invocation{iv} // npm run <script>, bun <file>: not a tool
				}
			}
			i = skipRunnerFlags(&iv, j, st, depth)
			if i < 0 {
				return []Invocation{iv}
			}
		case name == "sudo" || name == "doas" || name == "nohup" || name == "time" || name == "nice" || name == "command" || name == "builtin" || name == "exec" || name == "stdbuf":
			i++
			for i < len(w) && strings.HasPrefix(w[i].Text, "-") {
				if (name == "sudo" && (w[i].Text == "-u" || w[i].Text == "-g")) || (name == "nice" && w[i].Text == "-n") {
					i++
				}
				i++
			}
		case name == "timeout":
			i++
			for i < len(w) && strings.HasPrefix(w[i].Text, "-") {
				i++
			}
			i++ // the duration
		case name == "bash" || name == "sh" || name == "zsh" || name == "dash" || name == "ksh":
			for j := i + 1; j < len(w); j++ {
				x := w[j].Text
				if strings.HasPrefix(x, "-") && !strings.HasPrefix(x, "--") && strings.Contains(x, "c") {
					if j+1 < len(w) {
						return nested(iv, w[j+1].Text, st, depth, false)
					}
					break
				}
				if !strings.HasPrefix(x, "-") {
					break // a script file
				}
			}
			iv.Opaque = true
			return []Invocation{iv}
		case name == "pwsh" || name == "powershell":
			for j := i + 1; j < len(w); j++ {
				x := strings.ToLower(w[j].Text)
				if !strings.HasPrefix(x, "-") && !strings.HasPrefix(x, "/") {
					return nested(iv, joinWords(w[j:]), st, depth, true) // the default is -Command
				}
				f := strings.TrimLeft(x, "-/")
				switch {
				case f == "e" || f == "ec" || (len(f) >= 2 && strings.HasPrefix("encodedcommand", f)):
					if j+1 < len(w) {
						if s, ok := decodePowerShell(w[j+1].Text); ok {
							return nested(iv, s, st, depth, true)
						}
					}
					iv.Opaque = true
					return []Invocation{iv}
				case f == "c" || (len(f) >= 2 && strings.HasPrefix("command", f)):
					return nested(iv, joinWords(w[min(j+1, len(w)):]), st, depth, true)
				case f == "f" || (len(f) >= 2 && strings.HasPrefix("file", f)):
					iv.Opaque = true
					return []Invocation{iv}
				case f == "executionpolicy" || f == "ex" || f == "ep" || f == "workingdirectory" || f == "wd" || f == "configurationname" || f == "settingsfile" || f == "windowstyle" || f == "w" || f == "outputformat" || f == "of" || f == "inputformat" || f == "if" || f == "psconsolefile" || f == "version" || f == "v":
					j++ // takes a value
				}
			}
			return []Invocation{iv}
		case name == "cmd":
			for j := i + 1; j < len(w); j++ {
				x := strings.ToLower(w[j].Text)
				if x == "/c" || x == "/k" {
					return nested(iv, joinWords(w[j+1:]), st, depth, true)
				}
			}
			iv.Opaque = true
			return []Invocation{iv}
		case name == "eval" || name == "iex" || name == "invoke-expression":
			j := i + 1
			if j < len(w) && strings.EqualFold(w[j].Text, "-command") {
				j++
			}
			return nested(iv, joinWords(w[j:]), st, depth, name != "eval")
		case name == "&" || name == ".":
			i++
		case name == "xargs" || name == "start-process" || name == "saps" || name == "start" || name == "watch" || name == "parallel" || name == "ssh":
			iv.Opaque = true
			return []Invocation{iv}
		default:
			return []Invocation{iv}
		}
	}
	return []Invocation{iv}
}

// skipRunnerFlags skips the flags of npx/pnpm/yarn/bunx before the tool; -1
// means the runner runs a command string or a flag the parser does not know.
func skipRunnerFlags(iv *Invocation, i int, st *state, depth int) int {
	w := iv.Words
	for i < len(w) {
		x := w[i].Text
		switch {
		case x == "--":
			return i + 1
		case x == "-y" || x == "--yes" || x == "--no" || x == "-q" || x == "--quiet" || x == "--no-install" || x == "--ignore-existing" || x == "--prefer-offline" || x == "--silent" || x == "-s":
			i++
		case x == "-p" || x == "--package":
			i += 2
		case strings.HasPrefix(x, "--package="):
			i++
		case strings.HasPrefix(x, "-"):
			iv.Opaque = true
			return -1
		default:
			return i
		}
	}
	return i
}
