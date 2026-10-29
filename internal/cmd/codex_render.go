package cmd

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Owned by WS4 (spec E §12, §17): moved here from unattended.go unchanged by
// the interface commit; WS4 renders the guard mode into the identity's
// CODEX_HOME (config.toml block, rules/tq.rules, hooks.json).

// renderCodex renders the unattended settings into ws's Codex config when the
// workspace isolates a codex identity. Callers pass a workspace resolved after
// any manifest rewrite so the bypass-trust check sees the current hash.
func renderCodex(c *cobra.Command, ws *resolve.Workspace, dryRun bool) {
	if ws == nil || ws.Manifest == nil || !slices.Contains(ws.Manifest.IdentityNames(), "codex") {
		return
	}
	res, err := codexcfg.Render(paths.IdentityDir(ws.Name, "codex"), codexcfg.Desired(ws.Manifest, trust.IsBypassAllowed(ws.Hash)), dryRun)
	switch {
	case err != nil:
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: %v\n", ws.Name, err)
	case res.Conflict != "":
		fmt.Fprintf(c.ErrOrStderr(), "%s codex: not changed: %s (%s)\n", ws.Name, res.Conflict, res.Path)
	case res.Changed && dryRun:
		fmt.Fprintf(c.OutOrStdout(), "%s codex: would change (%s)\n", ws.Name, res.Path)
	case res.Changed:
		fmt.Fprintf(c.OutOrStdout(), "%s codex: updated (%s)\n", ws.Name, res.Path)
	}
}
