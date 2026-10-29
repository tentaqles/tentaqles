package cmd

import (
	"os"
	"slices"
	"sync"

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

// codexPlan compiles ws's guard mode at cwd for a launch: the per-launch
// override (TQ_MODE) counts. An untrusted workspace (or a nil one) is
// default: a grant or a looser layer only counts once trusted.
func codexPlan(ws *resolve.Workspace, cwd string) codexcfg.Plan {
	return codexPlanAt(ws, cwd, true)
}

// codexFilePlan compiles ws's workspace-wide mode for the files tq writes
// into CODEX_HOME (config.toml block, rules/tq.rules). A one-launch override
// never reaches them: TQ_MODE and TENTAQLES_HEADLESS are ignored, so a
// render run from a launched shell writes the persistent mode.
func codexFilePlan(ws *resolve.Workspace) codexcfg.Plan {
	root := ""
	if ws != nil {
		root = ws.Root
	}
	return codexPlanAt(ws, root, false)
}

func codexPlanAt(ws *resolve.Workspace, cwd string, launch bool) codexcfg.Plan {
	sandbox := codexcfg.SandboxWorkspace
	if ws == nil || ws.Manifest == nil || !trust.IsTrusted(ws.Hash) {
		if ws != nil && ws.Manifest != nil {
			sandbox = ws.Manifest.Guard.CodexSandbox()
		}
		return codexcfg.Compile(guardcfg.ModeDefault, codexRuleSet(ws), func(r *policy.Rule) policy.Action { return r.Action }, sandbox)
	}
	sandbox = ws.Manifest.Guard.CodexSandbox()
	var p codexcfg.Plan
	compile := func() {
		mode, eff := codexModeAt(ws, cwd)
		p = codexcfg.Compile(mode, codexRuleSet(ws), eff, sandbox)
	}
	if launch {
		compile()
	} else {
		withoutLaunchEnv(compile)
	}
	return p
}

// launchEnv are the env vars that carry a one-launch mode (spec E §6.1).
var launchEnv = []string{guardcfg.EnvMode, "TENTAQLES_HEADLESS"}

var launchEnvMu sync.Mutex

// withoutLaunchEnv runs f with the launch env vars unset, restoring them
// afterwards. tq is a short-lived CLI and resolveMode reads TQ_MODE itself,
// so this is the one way to resolve the persistent layers only.
// TODO(merge with WS1): call resolveModeFor(ws, cwd, "", "") with the
// headless default skipped instead.
func withoutLaunchEnv(f func()) {
	launchEnvMu.Lock()
	defer launchEnvMu.Unlock()
	saved := map[string]string{}
	for _, k := range launchEnv {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
			os.Unsetenv(k)
		}
	}
	defer func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}()
	f()
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
