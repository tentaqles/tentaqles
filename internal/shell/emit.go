// Package shell renders env operations and hook scripts per shell.
package shell

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
)

var Shells = []string{"bash", "zsh", "fish", "pwsh", "powershell", "cmd"}

func sq(s string) string    { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" } // POSIX
func psq(s string) string   { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func fishq(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }

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
