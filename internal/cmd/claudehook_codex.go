package cmd

import (
	"encoding/json"
	"io"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Owned by WS4 (spec E §12, §17).

// Values of `tq claude-hook pre-tool-use|session-start --agent`.
const (
	hookAgentClaude = "claude"
	hookAgentCodex  = "codex"
)

// codexHookOutput answers a Codex PreToolUse hook for the hook's final
// decision d (Ask or Deny; the caller never passes Allow) on call, and
// returns the process exit code. It never answers ask: Codex would continue
// the call (it fails open on ask). A tq ask becomes a deny, or, for a shell
// call that a `prompt` prefix rule in rules/tq.rules covers, an allow so
// that Codex's own rule prompts.
//
// STUB (interface commit): every non-Allow decision becomes a
// permissionDecision "deny" JSON on w with d's reason, exit 0 (fail closed).
// WS4 implements the prefix-rule case and the Codex payload details (A7).
func codexHookOutput(w io.Writer, call policy.ToolCall, d policy.Decision) int {
	if d.Action == policy.Allow {
		return 0
	}
	reason := d.Reason()
	if d.Action == policy.Ask {
		reason = "tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude\n" + reason
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	})
	return 0
}
