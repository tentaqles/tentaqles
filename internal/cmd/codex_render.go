package cmd

import (
	"fmt"
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
// when tq wrote it. Callers pass a workspace resolved after any manifest
// rewrite so the trust check sees the current hash. It prints nothing when
// everything is up to date.
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) {
	if !codexIsolated(ws) {
		return
	}
	p := codexPlan(ws, ws.Root)
	res, err := codexcfg.Render(paths.IdentityDir(ws.Name, "codex"), codexcfg.Want{Plan: &p, Hook: true}, dryRun)
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
