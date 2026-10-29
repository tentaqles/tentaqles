package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// unattendedGate is the unattended mode of one tool call: it leaves out of
// the decision every rule the session skips (guardcfg.Enforced is false) and
// keeps the ones that matched for the audit. A nil gate, the attended case,
// keeps every rule.
type unattendedGate struct {
	m       *manifest.Manifest
	scope   string
	skipped []*policy.Rule
}

// unattendedGateFor returns the gate for call when the workspace runs this
// folder unattended in a bypass or auto session, else nil.
func unattendedGateFor(ws *resolve.Workspace, cwd, mode string, call policy.ToolCall) *unattendedGate {
	if ws == nil || ws.Manifest == nil || !trust.IsTrusted(ws.Hash) {
		return nil
	}
	on, scope := guardcfg.Unattended(ws.Manifest, ws.Root, cwd, mode, trust.IsBypassAllowed(ws.Hash))
	if !on {
		return nil
	}
	// A file tool may reach outside the covered folder: its target must be
	// covered too. Bash and MCP calls stay judged by the session cwd.
	if call.Path != "" {
		abs := call.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, abs)
		}
		if ok, _ := guardcfg.Covers(ws.Manifest, ws.Root, filepath.Dir(abs)); !ok {
			return nil
		}
	}
	return &unattendedGate{m: ws.Manifest, scope: scope}
}

// skipFunc selects the rules this session skips; nil for a nil gate.
func (g *unattendedGate) skipFunc() func(*policy.Rule) bool {
	if g == nil {
		return nil
	}
	return func(r *policy.Rule) bool { return !guardcfg.Enforced(g.m, r) }
}

// note keeps skipped rules for the audit.
func (g *unattendedGate) note(rs []*policy.Rule) {
	if g != nil {
		g.skipped = append(g.skipped, rs...)
	}
}

// filter removes the skipped rules from a one-source decision and notes them.
func (g *unattendedGate) filter(d policy.Decision) policy.Decision {
	kept, skipped := policy.SplitDecision(d, g.skipFunc())
	g.note(skipped)
	return kept
}

// audit records the skipped rules that changed the outcome (stricter than
// final, the decision the session gets) and returns them, sorted by id with
// denies first. Rules no stricter than final changed nothing.
func (g *unattendedGate) audit(ws *resolve.Workspace, mode string, call policy.ToolCall, final policy.Decision) []*policy.Rule {
	if g == nil || ws == nil {
		return nil
	}
	var out []*policy.Rule
	seen := map[string]bool{}
	action := policy.Allow
	for _, r := range g.skipped {
		if !r.Action.StricterThan(final.Action) || seen[r.ID+"\x00"+string(r.Action)] {
			continue
		}
		seen[r.ID+"\x00"+string(r.Action)] = true
		out = append(out, r)
		if r.Action.StricterThan(action) {
			action = r.Action
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Action != out[j].Action {
			return out[i].Action.StricterThan(out[j].Action)
		}
		return out[i].ID < out[j].ID
	})
	ids := make([]string, 0, len(out))
	named := map[string]bool{}
	for _, r := range out {
		if !named[r.ID] {
			named[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	subject := call.Command
	if subject == "" {
		subject = call.Path
	}
	if subject == "" {
		subject = call.Content // MCP/Agent input: masked and truncated by Append
	}
	guardlog.Append(guardlog.Record{WS: ws.Name, Scope: g.scope, ClaudeMode: mode, Action: string(action), Rules: ids, Tool: call.Tool, Command: subject}, ws.Manifest.Data.Classes)
	return out
}

// skippedReason renders skipped rules as the decision they would have made:
// BLOCKED lines for the denies, CONFIRM lines for the asks.
func skippedReason(rs []*policy.Rule) string {
	var lines []string
	for _, a := range []policy.Action{policy.Deny, policy.Ask} {
		var d policy.Decision
		for _, r := range rs {
			if r.Action == a {
				d.Matched = append(d.Matched, r)
			}
		}
		if len(d.Matched) > 0 {
			d.Action = a
			lines = append(lines, d.Reason())
		}
	}
	return strings.Join(lines, "\n")
}

// unattendedLine is the SessionStart context line for a workspace with a
// guard.unattended block; "" when the block is not configured.
func unattendedLine(ws *resolve.Workspace, cwd string, now time.Time) string {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Guard.Unattended.Configured() {
		return ""
	}
	u := ws.Manifest.Guard.Unattended
	if !trust.IsBypassAllowed(ws.Hash) {
		return "Unattended: configured, not effective (needs tq allow --bypass " + ws.Name + ")"
	}
	on, scope := guardcfg.Covers(ws.Manifest, ws.Root, cwd)
	if !on {
		what := strings.Join(u.Projects, ", ")
		if u.Workspace {
			what = "the workspace"
		}
		return "Unattended: on for " + what + " (not this folder)"
	}
	return fmt.Sprintf("Unattended: on (%s) · %s skipped in 24h · applies in bypass and auto sessions", scope, skippedCounts(ws.Name, now))
}

// skippedCounts renders the confirms and blocks unattended mode skipped for
// ws in the 24 hours before now ("2 confirms and 1 block"). It reads only the
// log's tail, so the hook path stays bounded.
func skippedCounts(ws string, now time.Time) string {
	confirms, blocks := 0, 0
	for _, r := range guardlog.Read(ws, now.Add(-24*time.Hour), 1<<20) {
		if r.IsBlock() {
			blocks++
		} else {
			confirms++
		}
	}
	return plural(confirms, "confirm") + " and " + plural(blocks, "block")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
