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

// Owned by WS4 (spec E §12, §17).

// renderCodex renders ws's workspace-wide guard mode into its Codex identity
// (config.toml block, rules/tq.rules, hooks.json entries) when the workspace
// isolates a codex identity, and removes the old tq-unattended profile file
// when tq wrote it. When the workspace no longer isolates codex but its
// identity dir is still there, everything tq wrote into it is removed (the
// user's own files stay). The files hold the workspace-wide mode only: a
// launch override (TQ_MODE) never reaches them. Callers pass a workspace resolved after any manifest
// rewrite so the trust check sees the current hash. It prints nothing when
// everything is up to date.
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) {
	if ws == nil || ws.Name == "" {
		return
	}
	dir := paths.IdentityDir(ws.Name, "codex")
	want := codexcfg.Want{}
	if codexIsolated(ws) {
		p := codexFilePlan(ws)
		want = codexcfg.Want{Plan: &p, Hook: true}
	} else if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return
	}
	res, err := codexcfg.Render(dir, want, dryRun)
	if err != nil {
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: %v\n", ws.Name, err)
		return
	}
	for _, conflict := range res.Conflicts {
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: not changed: %s\n", ws.Name, conflict)
	}
	if len(res.Changed) == 0 {
		return
	}
	verb := "updated"
	if dryRun {
		verb = "would change"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s codex: %s (%s)\n", ws.Name, verb, strings.Join(res.Changed, ", "))
}
