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

// NoticeText is the fixed message of a notice: one of three sentences, with
// the workspace name only when it is a valid workspace name
// (workspace.NameRe: lower-case letters, digits, '.', '_', '-'), so nothing
// a folder name, a manifest or a file path holds ever reaches the shell
// line. The paths are what `tq hooks verify` prints. ok is false for an
// unknown kind.
func NoticeText(n envplan.Notice) (string, bool) {
	ws := n.Workspace
	if !workspace.NameRe.MatchString(ws) {
		ws = ""
	}
	switch n.Kind {
	case envplan.NoticeUntrusted:
		if ws == "" {
			return "tq: untrusted: this workspace carries no identity, run tq doctor", true
		}
		return "tq: untrusted: run tq allow " + ws, true
	case envplan.NoticeMismatch:
		if ws == "" {
			return "tq: integrity mismatch: this workspace carries no identity, run tq doctor", true
		}
		return "tq: integrity mismatch: run tq hooks verify " + ws, true
	case envplan.NoticeLegacy:
		if ws == "" {
			return "tq: legacy trust record: run tq doctor", true
		}
		return "tq: legacy trust record: run tq allow " + ws + " to sign hooks and manifest", true
	case envplan.NoticeMask:
		if ws == "" {
			return "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tq doctor", true
		}
		return "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tq settings render " + ws, true
	}
	return "", false
}

func Emit(sh string, ops envplan.Ops) (string, error) {
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
		set = func(k, v string) string { return `set "` + k + "=" + v + `"` }
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
			if strings.ContainsAny(v, "\"\r\n%!") {
				return "", fmt.Errorf("cmd: value for %s contains a character cmd.exe cannot quote", k)
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
		return "echo " + msg + " 1>&2"
	default: // bash, zsh
		return "printf '%s\\n' " + sq(msg) + " >&2"
	}
}
