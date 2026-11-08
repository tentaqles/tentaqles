package cmd

import (
	"regexp"
	"testing"

	"github.com/spf13/cobra"
)

// Every command is `tentaqles <command>`; `tq <command>` is its alias. The help a developer reads
// names the tentaqles form only.
func TestCommandTextNamesTentaqles(t *testing.T) {
	tqForm := regexp.MustCompile(`\btq (reach|run|login|open|doctor|settings|hooks|env|activate|list|providers|version|claude-hook|statusline)\b`)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for field, text := range map[string]string{"Use": c.Use, "Short": c.Short, "Long": c.Long, "Example": c.Example} {
			if m := tqForm.FindString(text); m != "" {
				t.Errorf("%s %s names %q; use the tentaqles form", c.CommandPath(), field, m)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root := NewRoot()
	if root.Use != "tentaqles" {
		t.Fatalf("root command is %q", root.Use)
	}
	walk(root)
}
