package cmd

import (
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Owned by WS4 (spec E §12, §17): moved here from run.go unchanged by the
// interface commit; WS4 compiles the guard mode into Codex launch flags.

// codexArgs adds tq's unattended profile when cwd is a covered project of a
// projects-only setup and bypass is trusted; workspace-wide setups use the
// top-level keys instead, and an explicit --profile/-p is left alone.
func codexArgs(ws *resolve.Workspace, cwd string, args []string) []string {
	u := ws.Manifest.Guard.Unattended
	if !u.Configured() || u.Workspace || !trust.IsBypassAllowed(ws.Hash) {
		return args
	}
	if !slices.Contains(ws.Manifest.IdentityNames(), "codex") {
		return args
	}
	// Only point Codex at a profile file tq wrote; never at a missing or
	// user-authored one (a conflict leaves that file alone).
	if !codexcfg.OwnsProfile(paths.IdentityDir(ws.Name, "codex")) {
		return args
	}
	if ok, _ := guardcfg.Covers(ws.Manifest, ws.Root, cwd); !ok {
		return args
	}
	for _, a := range args {
		if a == "--profile" || a == "-p" || strings.HasPrefix(a, "--profile=") {
			return args
		}
	}
	return append([]string{"--profile", codexcfg.Profile}, args...)
}
