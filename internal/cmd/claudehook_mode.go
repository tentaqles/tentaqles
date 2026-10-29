package cmd

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Owned by WS1 (spec E §11.2): the mode gate replaces unattendedGate
// (claudehook_unattended.go), which the hook still uses until WS1 switches.

// launchMode returns the per-launch override (TQ_MODE in tq's own env, never
// from a tool call), "" when unset.
func launchMode() string { return os.Getenv(guardcfg.EnvMode) }

// resolveMode resolves the guard mode for ws at cwd; target is a file tool's
// absolute path ("" otherwise). ws nil is a neutral folder.
//
// STUB (interface commit): guardcfg.DefaultResolved(). WS1 gathers the global
// file, the repo file and the grant and calls guardcfg.Resolve.
func resolveMode(ws *resolve.Workspace, cwd, target string) guardcfg.Resolved {
	return guardcfg.DefaultResolved()
}

// modeGate applies the resolved mode to one tool call and keeps the rules it
// changed for the audit. A nil gate changes nothing.
type modeGate struct {
	ws      *resolve.Workspace
	res     guardcfg.Resolved
	changed []policy.Mapped
}

// modeGateFor returns the gate for call in ws at cwd under the launch
// override launch; nil when everything resolves to default with no
// overrides (the fast path).
//
// STUB (interface commit): always nil.
func modeGateFor(ws *resolve.Workspace, cwd string, call policy.ToolCall, launch string) *modeGate {
	return nil
}

// resolved returns the gate's mode, DefaultResolved for a nil gate.
func (g *modeGate) resolved() guardcfg.Resolved {
	if g == nil {
		return guardcfg.DefaultResolved()
	}
	return g.res
}

// mapFunc is the mapper for policy.Set.EvaluateMapped and policy.MapDecision:
// Resolved.Effective. nil for a nil gate.
func (g *modeGate) mapFunc() func(*policy.Rule) policy.Action {
	if g == nil {
		return nil
	}
	return g.res.Effective
}

// note keeps changed rules for the audit.
func (g *modeGate) note(ms []policy.Mapped) {
	if g != nil {
		g.changed = append(g.changed, ms...)
	}
}

// apply maps a one-source decision (commit scan, PII, commit message,
// cross-client MCP, Jev, a pack guard) and notes what changed. A nil gate
// returns d.
func (g *modeGate) apply(d policy.Decision) policy.Decision {
	if g == nil {
		return d
	}
	out, changed := policy.MapDecision(d, g.mapFunc())
	g.note(changed)
	return out
}

// audit records the skipped or lowered rules that changed the outcome
// (stricter than final) and returns them.
//
// STUB (interface commit): records nothing, returns nil. WS1 writes the
// guardlog record (Mode, ClaudeMode, To).
func (g *modeGate) audit(claudeMode string, call policy.ToolCall, final policy.Decision) []policy.Mapped {
	return nil
}
