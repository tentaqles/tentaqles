// Package shell renders env operations and hook scripts per shell.
package shell

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
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

func Emit(sh string, ops envplan.Ops) (string, error) {
	var set func(k, v string) string
	var unset func(k string) string
	switch sh {
	case "bash", "zsh":
		set = func(k, v string) string { return "export " + k + "=" + sq(v) }
		unset = func(k string) string { return "unset " + k }
	case "fish":
		set = func(k, v string) string { return "set -gx " + k + " " + fishq(v) }
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
	if !ops.Changed {
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
	return b.String(), nil
}
