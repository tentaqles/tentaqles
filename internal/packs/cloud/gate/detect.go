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
	// Unknown marks a command whose environment an earlier statement of the
	// line may have changed in a way the parser does not model (source, .,
	// declare -x, unset, Set-Item Env:, [Environment]::SetEnvironmentVariable,
	// direnv, eval of a substitution, an assignment of a computed value):
	// its variables are unknown, the hook's own environment is not used, and
	// the target is floored at the default environment.
	Unknown bool
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
	dir     string
	env     map[string]string
	unknown bool // an unmodelled statement may have changed the environment
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

// pkgAliases map the npm package names of the gate's tools to the tool.
var pkgAliases = map[string]string{"aws-cdk": "cdk", "cdktf-cli": "cdktf"}

// PackageTool is the tool a package runner (npx, bunx, pnpm dlx, yarn dlx,
// npm exec) runs for a package word: aws-cdk@2.150.0 and cdk@2 are cdk,
// cdktf-cli is cdktf. "" when the word names no gate tool.
func PackageTool(w string) string {
	n := strings.ToLower(strings.ReplaceAll(w, `\`, "/"))
	if strings.HasPrefix(n, "@") {
		// a scoped package (@scope/name@version): the name part
		if i := strings.Index(n, "/"); i > 0 {
			n = n[i+1:]
		}
	}
	if i := strings.Index(n, "@"); i > 0 {
		n = n[:i]
	}
	n = ToolName(n)
	if a, ok := pkgAliases[n]; ok {
		return a
	}
	if Tools[n] {
		return n
	}
	return ""
}

// looseTool reports a word that names a gate tool in a form tq does not
// unwrap: a package spelling (aws-cdk, cdk@2) or a tool name behind a
// prefix (aws-vault's -- terraform, x-cdk).
func looseTool(w string) bool {
	if Tools[ToolName(w)] || PackageTool(w) != "" {
		return true
	}
	n := ToolName(strings.SplitN(w, "@", 2)[0])
	if i := strings.LastIndexAny(n, "-_/"); i >= 0 && Tools[n[i+1:]] {
		return true
	}
	return false
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
var psEnvRe = regexp.MustCompile(`(?i)^\$(?:env:([A-Za-z_][A-Za-z0-9_]*)|\{env:([A-Za-z_][A-Za-z0-9_]*)\})(=(.*))?$`)

// envProviderRe is a PowerShell word that writes the Env: drive or calls
// the .NET environment API: Set-Item Env:X, New-Item -Path Env:\X,
// Remove-Item env:X, [Environment]::SetEnvironmentVariable.
var envProviderRe = regexp.MustCompile(`(?i)^(-path:?)?env:|setenvironmentvariable`)

// computed reports an assigned value tq cannot know: a substitution, a
// variable, or an empty value left by a $( ) the lexer split off.
func computed(v string) bool {
	return v == "" || strings.ContainsAny(v, "$`")
}

// assign records NAME=value into the line's variables, or marks the line
// unknown when the value is computed.
func (st *state) assign(k, v string) {
	if computed(v) {
		st.unknown = true
	}
	st.env[strings.ToUpper(k)] = v
}

// Analyze reads every simple command of a shell line, with the cd and
// variable state the line builds up, and the commands nested in bash -c,
// pwsh -Command / -EncodedCommand, cmd /c, eval and $(...).
func Analyze(cmd string) []Invocation { return AnalyzeShell(cmd, false) }

// AnalyzeShell is Analyze for a bash (powershell false) or PowerShell line:
// in PowerShell a backslash is a plain character (C:\infra\plan).
func AnalyzeShell(cmd string, powershell bool) []Invocation {
	return AnalyzeShellAt(cmd, powershell, "")
}

// AnalyzeShellAt is AnalyzeShell for a line that starts in base, the
// repo-relative folder the shell is in ("" the repo root, "?" unknown).
func AnalyzeShellAt(cmd string, powershell bool, base string) []Invocation {
	cmd = guard.BlankDataHeredocs(cmd)
	st := &state{dir: base, env: map[string]string{}}
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
	leaf := func() []Invocation { return []Invocation{{Raw: seg.Raw, Words: w, Unknown: st.unknown}} }
	// A statement that changes the environment in a way tq does not model
	// makes every later command's variables unknown (review r0 C4).
	for _, x := range w {
		if envProviderRe.MatchString(x.Text) {
			st.unknown = true
		}
	}
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
		return leaf()
	case "popd", "pop-location":
		st.dir = "?"
		return leaf()
	case "export":
		for _, x := range w[1:] {
			switch {
			case assignRe.MatchString(x.Text):
				k, v, _ := strings.Cut(x.Text, "=")
				st.assign(k, v)
			case strings.HasPrefix(x.Text, "-") || strings.ContainsAny(x.Text, "$`"):
				st.unknown = true // export -n, export $(cat f)
			}
		}
		return leaf()
	case "source", "declare", "typeset", "local", "readonly", "unset", "setenv", "unsetenv", "import-module", "ipmo":
		// source runs a file in this shell; declare -x, typeset -x and unset
		// change the exported set; a module can set variables.
		st.unknown = true
		return leaf()
	case "set":
		// set -a (or -o allexport) exports every later assignment
		for i, x := range w[1:] {
			t := x.Text
			if (strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") && strings.Contains(t, "a")) ||
				(strings.EqualFold(t, "allexport") && i > 0) {
				st.unknown = true
			}
		}
		return leaf()
	case "direnv":
		// direnv loads a folder's .envrc; direnv exec DIR cmd runs cmd with it
		st.unknown = true
		return []Invocation{{Raw: seg.Raw, Words: w, Env: map[string]string{}, Dir: "?", Unknown: true, Opaque: true}}
	case ".":
		// bash `. file` sources a file; PowerShell `. cmd` dot-sources a
		// script or runs a command in this scope.
		if len(w) < 2 || !Tools[ToolName(w[1].Text)] {
			st.unknown = true
			return leaf()
		}
	}
	// PowerShell $env:NAME = value, ${env:NAME} = value
	if m := psEnvRe.FindStringSubmatch(w[0].Text); m != nil {
		name := m[1] + m[2]
		v, ok := m[4], m[3] != ""
		if !ok && len(w) >= 3 && w[1].Text == "=" {
			v, ok = joinWords(w[2:]), true
		} else if !ok && len(w) == 2 && strings.HasPrefix(w[1].Text, "=") {
			v, ok = strings.TrimPrefix(w[1].Text, "="), true
		}
		if !ok || len(w) > 3 || (len(w) == 3 && w[1].Text != "=") {
			st.unknown = true // += , a pipeline, or an expression
		}
		st.assign(name, v)
		return leaf()
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
			st.assign(k, v)
		}
		return leaf()
	}
	iv := Invocation{Raw: seg.Raw, Words: w, Env: map[string]string{}, Dir: st.dir, Unknown: st.unknown}
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
	sub := &state{dir: iv.Dir, env: map[string]string{}, unknown: iv.Unknown}
	for k, v := range iv.Env {
		sub.env[k] = v
	}
	return nestedIn(iv, script, sub, depth, ps)
}

// nestedHere is nested for a script that runs in the line's own shell (eval,
// Invoke-Expression): what it sets, its cd and anything unknown carry on to
// the rest of the line.
func nestedHere(iv Invocation, script string, st *state, depth int, ps bool) []Invocation {
	if depth >= maxDepth {
		iv.Opaque = true
		return []Invocation{iv}
	}
	if iv.Unknown {
		st.unknown = true
	}
	return nestedIn(iv, script, st, depth, ps)
}

func nestedIn(iv Invocation, script string, sub *state, depth int, ps bool) []Invocation {
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
	pkg := false // the next word is a package runner's package
	for i < len(w) {
		t := w[i].Text
		if assignRe.MatchString(t) {
			k, v, _ := strings.Cut(t, "=")
			iv.Env[strings.ToUpper(k)] = v
			if computed(v) {
				iv.Unknown = true
			}
			i++
			continue
		}
		name := ToolName(t)
		if pkg {
			// the word after a package runner is a package spelling
			if pt := PackageTool(t); pt != "" {
				name = pt
			}
			pkg = false
		}
		switch {
		case Tools[name]:
			iv.Tool, iv.ToolAt = name, i
			parseTool(&iv)
			return []Invocation{iv}
		case name == "env":
			i++
			for i < len(w) {
				x := w[i].Text
				if x == "-u" || x == "--unset" || strings.HasPrefix(x, "--unset=") {
					// an unset variable falls back to nothing, not to the
					// hook's value: unknown
					iv.Unknown = true
					if x == "-u" || x == "--unset" {
						i++
					}
					i++
					continue
				}
				if x == "-i" || x == "--ignore-environment" || x == "-" {
					iv.Unknown = true
					i++
					continue
				}
				if x == "--" {
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
			pkg = true
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
			pkg = true
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
			script := joinWords(w[j:])
			if strings.ContainsAny(script, "$`") {
				// eval "$(aws configure export-credentials …)": the code is
				// a command's output
				st.unknown = true
				iv.Unknown = true
			}
			return nestedHere(iv, script, st, depth, name != "eval")
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
