package cmd

import (
	"encoding/json"
	"io"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Values of `tq claude-hook pre-tool-use|session-start --agent`.
const (
	hookAgentClaude = "claude"
	hookAgentCodex  = "codex"
)

// codexAskPrefix leads the reason of a tq ask that Codex gets as a deny.
const codexAskPrefix = "tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude"

// codexAnswer answers a Codex PreToolUse hook for the hook's final decision
// d on call and returns the process exit code. It never answers ask: Codex
// would continue the call (it fails open on ask), so every non-allow
// decision is a deny, and an ask says why it became one.
func codexAnswer(w io.Writer, call policy.ToolCall, d policy.Decision) int {
	if d.Action == policy.Allow {
		return 0
	}
	reason := d.Reason()
	if d.Action == policy.Ask {
		reason = codexAskPrefix + "\n" + reason
	}
	writeCodexDeny(w, reason)
	return 0
}

// writeCodexDeny writes Codex's PreToolUse deny answer.
func writeCodexDeny(w io.Writer, reason string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
}
