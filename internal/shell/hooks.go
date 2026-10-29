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
// defines never recurse into themselves).
func Hook(sh string) (string, error) {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "tq"
	}
	return HookFor(sh, bin)
}

// HookFor is Hook calling bin.
func HookFor(sh, bin string) (string, error) {
	raw, err := hookFS.ReadFile("hooks/" + sh + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("no hook for shell %q (known: %s)", sh, strings.Join(Shells, ", "))
	}
	return strings.ReplaceAll(string(raw), "{{.Bin}}", bin), nil
}
