// Package shell renders env operations and hook scripts per shell.
package shell

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

var Shells = []string{"bash", "zsh", "fish", "pwsh", "powershell", "cmd"}

func sq(s string) string    { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" } // POSIX
func psq(s string) string   { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func fishq(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }

// envKeyRe mirrors providers.EnvKeyRe. Emit re-checks keys as defence in
// depth: nothing that reaches a user's shell should be able to break out of an
// assignment, whatever produced it.
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// checkOps rejects env keys that are not valid variable names and values that
// carry line breaks or NUL, which no shell can quote safely.
func checkOps(ops envplan.Ops) error {
	for _, k := range ops.Unset {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("refusing to emit: %q is not a valid environment variable name", k)
		}
	}
	for k, v := range ops.Set {
		if !envKeyRe.MatchString(k) {
			return fmt.Errorf("refusing to emit: %q is not a valid environment variable name", k)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("refusing to emit: value for %s contains a line break or NUL", k)
		}
	}
	return nil
}

// NoticeText is the fixed message of a notice: one fixed sentence, with
// the workspace name only when it is a valid workspace name
// (workspace.NameRe: lower-case letters, digits, '.', '_', '-'), so nothing
// a folder name, a manifest or a file path holds ever reaches the shell
// line. ok is false for an unknown kind.
func NoticeText(n envplan.Notice) (string, bool) {
	ws := n.Workspace
	if !workspace.NameRe.MatchString(ws) {
		ws = ""
	}
	switch n.Kind {
	case envplan.NoticeStateLost:
		return "tq: shell state lost: tq unset its own variables and cannot restore the values from before it; open a new terminal to get them back", true
	case envplan.NoticeCmdLong:
		return "tq: PATH over cmd.exe's 8191-character line: out-of-scope CLIs still resolve here, shorten PATH or use PowerShell", true
	case envplan.NoticeMask:
		if ws == "" {
			return "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tentaqles doctor", true
		}
		return "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tentaqles settings render " + ws, true
	}
	return "", false
}

// CmdLineMax is the longest line tq may print to the cmd.exe hook, in bytes. Measured through
// the hook's own form (`@if "!!"=="" (for /f ... do @%%L) else (...)`, delayed expansion off
// and on): a printed line of 8192 characters runs, one of 8193 is dropped together with every
// line after it (TestEmit_Cmd_LineBudget). cmd.exe's 8191-character command line plus the `@`
// the body adds is that budget. tentaqles env leaves a longer PATH alone and says so;
// Emit refuses a longer line rather than lose it silently. Bytes, not characters: cmd.exe
// reads the UTF-8 tq prints byte by byte, so this is the conservative count.
const CmdLineMax = 8192

// Opts are the shell-specific choices of an emit.
type Opts struct {
	// CmdDelayed: the cmd.exe that runs the lines has delayed expansion on (the hook tests
	// "!!"), which would eat a ! and, on a line holding one, a ^: those are escaped.
	CmdDelayed bool
}

// cmdValue is a value as a `set "K=V"` line run from the hook's for /f must spell it: the
// for variable's text is not parsed again for %, &, |, <, > or quotes (verified), so only
// delayed expansion needs escaping.
func cmdValue(v string, o Opts) string {
	if o.CmdDelayed && strings.Contains(v, "!") {
		return strings.NewReplacer("^", "^^", "!", "^!").Replace(v)
	}
	return v
}

// CmdSetLine is the line Emit prints to cmd.exe to set k to v.
func CmdSetLine(k, v string, o Opts) string { return `set "` + k + "=" + cmdValue(v, o) + `"` }

// EmitOpts is Emit with shell-specific options.
func EmitOpts(sh string, ops envplan.Ops, o Opts) (string, error) {
	var set func(k, v string) string
	var unset func(k string) string
	switch sh {
	case "bash", "zsh":
		set = func(k, v string) string { return "export " + k + "=" + sq(v) }
		unset = func(k string) string { return "unset " + k }
	case "fish":
		set = func(k, v string) string {
			if k == "PATH" {
				// fish holds PATH as a list: one quoted word per entry.
				var w []string
				for _, e := range strings.Split(v, ":") {
					w = append(w, fishq(e))
				}
				return "set -gx PATH " + strings.Join(w, " ")
			}
			return "set -gx " + k + " " + fishq(v)
		}
		unset = func(k string) string { return "set -e " + k }
	case "pwsh", "powershell":
		set = func(k, v string) string { return "$env:" + k + " = " + psq(v) }
		unset = func(k string) string { return "Remove-Item -ErrorAction SilentlyContinue Env:" + k }
	case "cmd":
		set = func(k, v string) string { return CmdSetLine(k, v, o) }
		unset = func(k string) string { return `set "` + k + `="` }
	default:
		return "", fmt.Errorf("unknown shell %q (known: %s)", sh, strings.Join(Shells, ", "))
	}
	if err := checkOps(ops); err != nil {
		return "", err
	}
	notice := ""
	if ops.Notice != nil {
		t, ok := NoticeText(*ops.Notice)
		if !ok {
			return "", fmt.Errorf("refusing to emit: unknown notice %q", ops.Notice.Kind)
		}
		notice = t
	}
	if !ops.Changed && notice == "" {
		return "", nil
	}
	if sh == "cmd" {
		for k, v := range ops.Set {
			if n := len(set(k, v)); n > CmdLineMax {
				return "", fmt.Errorf("cmd: the line for %s is %d characters, over cmd.exe's %d", k, n, CmdLineMax)
			}
		}
	}
	var b strings.Builder
	if ops.Changed {
		for _, k := range ops.Unset {
			b.WriteString(unset(k) + "\n")
		}
		keys := make([]string, 0, len(ops.Set))
		for k := range ops.Set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(set(k, ops.Set[k]) + "\n")
		}
	}
	if notice != "" {
		b.WriteString(warn(sh, notice) + "\n")
	}
	return b.String(), nil
}

// warn is the shell line that prints msg on stderr. msg is a NoticeText:
// fixed words and a validated workspace name, nothing any shell treats as
// syntax; it is still quoted for the shell.
func warn(sh, msg string) string {
	switch sh {
	case "fish":
		return "printf '%s\\n' " + fishq(msg) + " >&2"
	case "pwsh", "powershell":
		return "[Console]::Error.WriteLine(" + psq(msg) + ")"
	case "cmd":
		// The hook runs each line from a for /f variable, whose text cmd.exe never parses
		// for a redirection (verified): the line goes to the console as it is.
		return "echo " + msg
	default: // bash, zsh
		return "printf '%s\\n' " + sq(msg) + " >&2"
	}
}
