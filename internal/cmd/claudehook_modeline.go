package cmd

import (
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Owned by WS3 (spec E §10.4, §17).

// modeLineInput is what SessionStart knows when it renders the guard line.
type modeLineInput struct {
	WS         *resolve.Workspace // the trusted workspace; nil in a neutral folder
	Cwd        string
	Resolved   guardcfg.Resolved // the session's resolved mode (resolveMode)
	ClaudeMode string            // the payload's permission_mode ("" unknown)
	Agent      string            // hookAgentClaude | hookAgentCodex
	Now        time.Time
}

// modeLine is the SessionStart context line for the guard mode, e.g.
// "Guard: relaxed (workspace, granted) · 3 confirms and 1 block skipped in 24h
// · locked: identity, secrets, self-protection, privacy"; "" prints nothing.
// It replaces unattendedLine.
//
// STUB (interface commit): always "". WS3 implements the variants of §10.4.
func modeLine(in modeLineInput) string { return "" }
