package shell

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:embed hooks/*.tmpl
var hookFS embed.FS

// Hook returns the shell snippet users eval in their profile, calling this
// tq binary by its absolute path (so the tq, claude and codex functions it
// defines never recurse into themselves). When the path is unknown it calls
// the tq program found on PATH, never a function or alias named tq.
func Hook(sh string) (string, error) {
	bin, err := os.Executable()
	if err != nil {
		bin = ""
	}
	return HookFor(sh, bin)
}

// HookFor is Hook calling bin ("" = the tq program on PATH).
func HookFor(sh, bin string) (string, error) {
	raw, err := hookFS.ReadFile("hooks/" + sh + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("no hook for shell %q (known: %s)", sh, strings.Join(Shells, ", "))
	}
	return strings.ReplaceAll(string(raw), "{{.Bin}}", binWord(sh, bin)), nil
}

// binWord is how sh names bin as a command word: quoted for that shell, so
// a quote, space, $ or backtick in the path is data. "" is the program tq
// on PATH, skipping functions (which would recurse into tq's own).
func binWord(sh, bin string) string {
	switch sh {
	case "powershell", "pwsh":
		if bin == "" {
			return "(Get-Command -CommandType Application -Name tq -ErrorAction Stop | Select-Object -First 1).Source"
		}
		return "'" + strings.ReplaceAll(bin, "'", "''") + "'"
	case "cmd":
		if bin == "" {
			return "tq"
		}
		return `"` + bin + `"` // a Windows path holds no `"`
	case "fish":
		if bin == "" {
			return "command tq"
		}
		return "'" + strings.ReplaceAll(strings.ReplaceAll(bin, `\`, `\\`), "'", `\'`) + "'"
	default: // bash, zsh
		if bin == "" {
			return "command tq"
		}
		return "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
	}
}
