package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Owned by WS4 (spec E §12, §17).

// Values of `tq claude-hook pre-tool-use|session-start --agent`.
const (
	hookAgentClaude = "claude"
	hookAgentCodex  = "codex"
)

// codexAskPrefix leads the reason of a tq ask that Codex gets as a deny.
const codexAskPrefix = "tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude"

// codexPromptOS is the OS the prompt-rule path is judged for (a test seam).
// On Windows Codex runs shell calls through PowerShell, whose scripts Codex's
// exec policy is not documented to split, so a prompt rule may not fire:
// there every ask stays a deny.
var codexPromptOS = runtime.GOOS

// codexHookCtx is what the Codex answer depends on besides the decision.
type codexHookCtx struct {
	// CodexHome holds rules/tq.rules (the CODEX_HOME Codex runs with).
	CodexHome string
	// PermissionMode is the payload's permission_mode, "" when unknown. In
	// bypassPermissions a prompt rule may not prompt (A9), so asks deny.
	PermissionMode string
	GOOS           string
}

// codexRulesHome is the CODEX_HOME this hook's Codex runs with: the hook
// inherits Codex's env, so CODEX_HOME, else ~/.codex.
func codexRulesHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// codexHookOutput answers a Codex PreToolUse hook for the hook's final
// decision d on call and returns the process exit code. It never answers
// ask: Codex would continue the call (it fails open on ask). A tq ask
// becomes a deny, or, for a shell call that one of tq's prompt prefix rules
// in rules/tq.rules covers, no output, so that Codex's own rule prompts.
//
// The permission mode is unknown here, so the bypass check of codexAnswer
// does not apply; WS1 can pass p.PermissionMode by calling codexAnswer with
// codexHookCtx{CodexHome: codexRulesHome(), PermissionMode: p.PermissionMode,
// GOOS: codexPromptOS} instead.
func codexHookOutput(w io.Writer, call policy.ToolCall, d policy.Decision) int {
	return codexAnswer(w, call, d, codexHookCtx{CodexHome: codexRulesHome(), GOOS: codexPromptOS})
}

// codexAnswer is codexHookOutput with its inputs explicit.
func codexAnswer(w io.Writer, call policy.ToolCall, d policy.Decision, ctx codexHookCtx) int {
	if d.Action == policy.Allow {
		return 0
	}
	if d.Action == policy.Ask && codexPromptCovers(call, d, ctx) {
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

// codexPromptCovers reports whether Codex itself will prompt for the ask d
// on call: a Bash call, outside Windows and bypass, whose every matched rule
// has one of tq's curated patterns as a prompt rule in tq's own rules file
// (an unreadable or foreign file covers nothing).
func codexPromptCovers(call policy.ToolCall, d policy.Decision, ctx codexHookCtx) bool {
	if call.Tool != "Bash" || ctx.GOOS == "windows" || ctx.CodexHome == "" {
		return false
	}
	if ctx.PermissionMode == "bypassPermissions" {
		return false
	}
	rules, err := codexcfg.ReadRules(ctx.CodexHome)
	if err != nil {
		return false
	}
	ids := make([]string, 0, len(d.Matched))
	for _, r := range d.Matched {
		if r == nil {
			return false
		}
		ids = append(ids, r.ID)
	}
	return codexcfg.PromptCovers(rules, ids, call.Command)
}
