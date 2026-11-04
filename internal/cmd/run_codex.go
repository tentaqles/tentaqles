package cmd

import (
	"slices"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// codexIsolated reports whether ws gives Codex its own CODEX_HOME.
func codexIsolated(ws *resolve.Workspace) bool {
	return ws != nil && ws.Manifest != nil && slices.Contains(ws.Manifest.IdentityNames(), "codex")
}

// codexArgs prepends the pinned Codex flags (codexcfg.Pinned: -a on-request
// -s workspace-write) when the workspace isolates a codex identity. Flags
// the user typed win.
func codexArgs(ws *resolve.Workspace, args []string) []string {
	if !codexIsolated(ws) {
		return args
	}
	return codexcfg.LaunchArgs(codexcfg.Pinned, args)
}
