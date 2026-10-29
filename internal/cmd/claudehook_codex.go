package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
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
	// PermissionMode is the payload's permission_mode. Only a known mode
	// where Codex prompts (codexPromptModes) lets the prompt path apply: ""
	// (unknown), bypass, or anything else denies every ask, since a Codex
	// started by hand with --yolo still has tq's prompt lines in its rules.
	PermissionMode string
	GOOS           string
	// WS and Cwd are the workspace and cwd of the call. The hook recomputes
	// the plan tq renders for WS and trusts a prompt rule only when the
	// plan has it and the rules file is exactly what tq renders: an agent
	// that edits rules/tq.rules cannot widen what the hook lets through.
	WS  *resolve.Workspace
	Cwd string
}

// codexPromptModes are the permission modes in which Codex itself prompts
// on a prompt rule. Anything else (bypass, unknown, "") fails closed.
var codexPromptModes = map[string]bool{"default": true, "acceptEdits": true, "plan": true}

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
// ask: Codex would continue the call (it fails open on ask). Every tq ask
// becomes a deny here, because the permission mode and workspace are not
// known at this call site and the prompt path must fail closed.
//
// WS1 turns the prompt path on by calling codexAnswer with
// codexHookCtx{CodexHome: codexRulesHome(), PermissionMode: p.PermissionMode,
// GOOS: codexPromptOS, WS: ws, Cwd: cwd} instead.
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
// on call. All of these must hold, else the ask is a deny:
//
//   - a Bash call, outside Windows, in a known prompting permission mode;
//   - a single command (no chain): whether Codex splits a "bash -lc" chain
//     at run time is unverified (A11), so a chain is never left to it;
//   - CODEX_HOME is the workspace's codex identity dir, and the workspace
//     isolates codex;
//   - the plan tq renders for the workspace, and the plan of this launch at
//     cwd, have no danger-full-access sandbox or bypass (a prompt may not
//     fire there, A9);
//   - rules/tq.rules is byte for byte what tq renders for that plan, so a
//     line an agent planted is a mismatch;
//   - every matched rule has one of tq's curated patterns as a prompt in
//     that plan, matched by the command.
func codexPromptCovers(call policy.ToolCall, d policy.Decision, ctx codexHookCtx) bool {
	if call.Tool != "Bash" || ctx.GOOS == "windows" || ctx.CodexHome == "" {
		return false
	}
	if !codexPromptModes[ctx.PermissionMode] {
		return false
	}
	if segs, ok := codexcfg.Segments(call.Command); !ok || len(segs) != 1 {
		return false
	}
	ws := ctx.WS
	if !codexIsolated(ws) || ws.Name == "" {
		return false
	}
	if filepath.Clean(ctx.CodexHome) != filepath.Clean(paths.IdentityDir(ws.Name, "codex")) {
		return false
	}
	plan := codexFilePlan(ws)
	if plan.Bypass || plan.Sandbox == codexcfg.SandboxFull {
		return false
	}
	cwd := ctx.Cwd
	if cwd == "" {
		cwd = ws.Root
	}
	if lp := codexPlan(ws, cwd); lp.Bypass || lp.Sandbox == codexcfg.SandboxFull {
		return false
	}
	b, err := os.ReadFile(filepath.Join(ctx.CodexHome, filepath.FromSlash(codexcfg.RulesFile)))
	if err != nil {
		return false
	}
	if strings.ReplaceAll(string(b), "\r\n", "\n") != codexcfg.RenderRules(plan.Mode, plan.Rules) {
		return false
	}
	ids := make([]string, 0, len(d.Matched))
	for _, r := range d.Matched {
		if r == nil {
			return false
		}
		ids = append(ids, r.ID)
	}
	return codexcfg.PromptCovers(plan.Rules, ids, call.Command)
}
