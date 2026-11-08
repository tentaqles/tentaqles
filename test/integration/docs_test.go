package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/cmd"
)

// documentedGroups are the commands whose subcommands the reference documents one by one.
var documentedGroups = map[string]bool{"providers": true, "hooks": true, "claude-hook": true, "settings": true}

// commandPaths are the command paths the reference must document: every top-level command, and the
// subcommands of documentedGroups in their place.
func commandPaths(root *cobra.Command) []string {
	var out []string
	for _, c := range root.Commands() {
		name := c.Name()
		if name == "help" || name == "completion" {
			continue
		}
		if documentedGroups[name] {
			for _, sub := range c.Commands() {
				out = append(out, name+" "+sub.Name())
			}
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// The command reference has a `## tentaqles <path>` section for every command the binary has, and
// none for a command it doesn't.
func TestDocsReferenceEveryCommand(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "reference", "commands.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^## tentaqles ([a-z -]+?)\s*$`).FindAllStringSubmatch(string(raw), -1) {
		documented[strings.TrimSpace(m[1])] = true
	}
	real := map[string]bool{}
	for _, p := range commandPaths(cmd.NewRoot()) {
		real[p] = true
		if !documented[p] {
			t.Errorf("tentaqles %s has no `## tentaqles %s` section in docs/reference/commands.md", p, p)
		}
	}
	for p := range documented {
		if !real[p] {
			t.Errorf("docs/reference/commands.md documents `tentaqles %s`, which is no command", p)
		}
	}
}
