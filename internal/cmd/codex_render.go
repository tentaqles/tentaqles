package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// renderCodex renders tq's pinned Codex settings (codexcfg.Pinned: the
// config.toml block and the hooks.json entries) into ws's Codex identity
// when the workspace isolates a codex identity, and removes what older tq
// versions wrote there (the guard-mode rules file, the unattended profile).
// When the workspace no longer isolates codex but its identity dir is still
// there, everything tq wrote into it is removed (the user's own files stay).
// It prints nothing when everything is up to date, and reports whether a
// file changed (or would, on a dry run).
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) bool {
	if ws == nil || ws.Name == "" {
		return false
	}
	dir := paths.IdentityDir(ws.Name, "codex")
	want := codexcfg.Want{}
	if codexIsolated(ws) {
		p := codexcfg.Pinned
		want = codexcfg.Want{Plan: &p, Hook: true}
	} else if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	res, err := codexcfg.Render(dir, want, dryRun)
	if err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: %v\n", ws.Name, err)
		return false
	}
	for _, conflict := range res.Conflicts {
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: not changed: %s\n", ws.Name, conflict)
	}
	if len(res.Changed) == 0 {
		return false
	}
	verb := "updated"
	if dryRun {
		verb = "would change"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s codex: %s (%s)\n", ws.Name, verb, strings.Join(res.Changed, ", "))
	return true
}
