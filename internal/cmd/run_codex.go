package cmd

import (
	"slices"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Owned by WS4 (spec E §12, §17): the guard mode compiled into Codex launch
// flags and CODEX_HOME files.

// codexModeAt returns ws's guard mode at cwd and its effective action per
// rule. Tests replace it while the WS1 engine is a stub.
var codexModeAt = func(ws *resolve.Workspace, cwd string) (string, func(*policy.Rule) policy.Action) {
	r := resolveMode(ws, cwd, "")
	return r.Mode, r.Effective
}

// codexRuleSet is the built-in rule set of ws after guard.disable and
// guard.actions (global file and manifest), the "own action" a category's
// `rule` value means. A locked rule is never left out and never loosened.
func codexRuleSet(ws *resolve.Workspace) []policy.Rule {
	var eff guardcfg.Effective
	if ws == nil {
		eff = guardcfg.ResolveActions(nil, "")
	} else {
		eff = guardcfg.ResolveActions(ws.Manifest, ws.Name)
	}
	var out []policy.Rule
	for _, r := range policy.Builtin() {
		locked := guardcfg.Locked(guardcfg.CategoryOf(&r))
		if eff.Off(r.ID) && !locked {
			continue
		}
		if a, ok := eff.Actions[r.ID]; ok && (a == policy.Ask || a == policy.Deny) {
			if !locked || a.StricterThan(r.Action) {
				r.Action = a
			}
		}
		out = append(out, r)
	}
	return out
}

// codexPlan compiles ws's guard mode at cwd. An untrusted workspace (or a
// nil one) is default: a grant or a looser layer only counts once trusted.
func codexPlan(ws *resolve.Workspace, cwd string) codexcfg.Plan {
	mode, eff := guardcfg.ModeDefault, func(r *policy.Rule) policy.Action { return r.Action }
	sandbox := codexcfg.SandboxWorkspace
	if ws != nil && ws.Manifest != nil {
		sandbox = ws.Manifest.Guard.CodexSandbox()
		if trust.IsTrusted(ws.Hash) {
			mode, eff = codexModeAt(ws, cwd)
		}
	}
	return codexcfg.Compile(mode, codexRuleSet(ws), eff, sandbox)
}

// codexIsolated reports whether ws gives Codex its own CODEX_HOME.
func codexIsolated(ws *resolve.Workspace) bool {
	return ws != nil && ws.Manifest != nil && slices.Contains(ws.Manifest.IdentityNames(), "codex")
}

// codexArgs prepends the launch flags of ws's guard mode at cwd (spec E §12:
// -a/-s, or the bypass flag for relaxed with a danger-full-access sandbox)
// when the workspace isolates a codex identity. Flags the user typed win.
// Per-project modes are carried here, since the config.toml block holds the
// workspace-wide mode only.
func codexArgs(ws *resolve.Workspace, cwd string, args []string) []string {
	if !codexIsolated(ws) {
		return args
	}
	return codexcfg.LaunchArgs(codexPlan(ws, cwd), args)
}
