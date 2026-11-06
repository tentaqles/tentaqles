package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// scopeLine is layer 1 of the §6.1 scenario (advisory, never relied on; ADR-0008): the one
// line of the session-start preamble, Claude's and Codex's, that states an
// identity's provider scope, so a well-behaved model declines or asks before it reaches for
// another provider. Without the flag it is empty.
func scopeLine(ws *resolve.Workspace) string {
	if ws == nil || ws.Manifest == nil {
		return ""
	}
	ids := ws.Manifest.Scope()
	list := "none"
	if len(ids) > 0 {
		list = strings.Join(ids, ", ")
	}
	return "Scope: " + list + ". Identity " + ws.Name + " may use these providers only: any other provider's CLI, MCP server, variable, credential file or API host is out of scope and tq blocks it (a claude.ai connector of another provider stays listed, tq cannot remove it, but its tools are denied); if a task needs one, stop and tell the developer.\n"
}

// renderScope writes what an identity's earlier layers need on disk (ADR-0008): the
// PATH mask folder (layer 2) and the MCP server lists of its Claude and Codex homes without
// the out-of-scope servers (layer 3). Without the flag it removes a mask folder an earlier
// render wrote and touches nothing else. Only the human-only renders call it (tq allow, tq
// settings render, tq hooks verify --accept): everything it writes is under the identity's
// folder in the protected tq home. It reports whether anything changed (or would, dryRun).
func renderScope(c *cobra.Command, ws *resolve.Workspace, dryRun bool) bool {
	if ws == nil || ws.Manifest == nil {
		return false
	}
	changed := false
	l := envplan.LayerFor(ws)
	switch {
	case l == nil && !dryRun:
		if had, err := envplan.RemoveMask(ws.Name); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: PATH mask not removed: %v\n", ws.Name, err)
		} else if had {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask removed\n", ws.Name)
			changed = true
		}
	case l != nil && dryRun:
		if !l.MaskReady() {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask would be written (%s, %d names)\n", ws.Name, l.MaskDir(), len(l.Masked()))
			changed = true
		}
	case l != nil:
		if ch, err := l.WriteMask(); err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s: PATH mask not written: %v (run tq settings render %s)\n", ws.Name, err, ws.Name)
		} else if ch {
			fmt.Fprintf(c.OutOrStdout(), "%s: PATH mask written (%s, %d names)\n", ws.Name, l.MaskDir(), len(l.Masked()))
			changed = true
		}
	}
	j := mcpscope.For(ws.Name, ws.Manifest)
	if j == nil {
		return changed
	}
	verb := "removed"
	if dryRun {
		verb = "would remove"
	}
	report := func(what string, removed []mcpscope.Removed, err error) {
		if err != nil {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s %s: MCP servers not filtered: %v\n", ws.Name, what, err)
			return
		}
		for _, r := range removed {
			fmt.Fprintf(c.OutOrStdout(), "%s %s: %s MCP server %s (%s); kept in %s\n", ws.Name, what, verb, r.Name, r.Why, mcpscope.RemovedFile)
			changed = true
		}
	}
	claudeDir := paths.IdentityDir(ws.Name, "claude")
	if fi, err := os.Stat(claudeDir); err == nil && fi.IsDir() {
		if !dryRun && bundle.InUse(claudeDir) {
			fmt.Fprintf(c.ErrOrStderr(), "warning: %s claude: Claude Code looks open with this config home; it may write .claude.json back, run tq settings render %s again once it exits\n", ws.Name, ws.Name)
		}
		removed, err := mcpscope.FilterClaude(claudeDir, j, dryRun)
		report("claude", removed, err)
		// A plugin's MCP servers are in the plugin's own files: reported, never edited; the
		// hook denies their tools.
		for _, ps := range mcpscope.PluginServers(claudeDir) {
			if why, out := j.Out(ps.ToolName, ps.Entry); out {
				fmt.Fprintf(c.ErrOrStderr(), "warning: %s claude: plugin %s brings MCP server %s (%s); the hook denies its tools, disable the plugin for this identity to drop them (%s)\n", ws.Name, ps.Plugin, ps.Server, why, ps.File)
			}
		}
	}
	codexDir := paths.IdentityDir(ws.Name, "codex")
	if fi, err := os.Stat(codexDir); err == nil && fi.IsDir() {
		removed, err := mcpscope.FilterCodex(codexDir, j, dryRun)
		report("codex", removed, err)
	}
	return changed
}
