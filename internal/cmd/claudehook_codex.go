package cmd

import (
	"encoding/json"
	"io"
)

// Values of `tq claude-hook pre-tool-use|session-start --agent`.
const (
	hookAgentClaude = "claude"
	hookAgentCodex  = "codex"
)

// writeCodexDeny writes Codex's PreToolUse deny answer. Under --agent codex
// every deny writes it and exits 2 as well, so either half blocks.
func writeCodexDeny(w io.Writer, reason string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
}
