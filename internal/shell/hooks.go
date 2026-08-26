package shell

import (
	"embed"
	"fmt"
	"os"
	"strings"
)

//go:embed hooks/*.tmpl
var hookFS embed.FS

// Hook returns the shell snippet users eval in their profile.
func Hook(sh string) (string, error) {
	raw, err := hookFS.ReadFile("hooks/" + sh + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("no hook for shell %q (known: %s)", sh, strings.Join(Shells, ", "))
	}
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "tq"
	}
	return strings.ReplaceAll(string(raw), "{{.Bin}}", bin), nil
}
