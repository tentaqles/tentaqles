package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// decideDir holds the Jev cache, cost log, judgment log and breaker state.
func decideDir() string { return filepath.Join(paths.Home(), "decide") }

func jevBreaker() decide.Breaker {
	return decide.Breaker{Path: filepath.Join(decideDir(), "breaker"), CoolDown: time.Minute}
}

func judgmentLog() *decide.JudgmentLog {
	return &decide.JudgmentLog{Path: filepath.Join(decideDir(), "judgments.jsonl")}
}

// jevClientFor builds a client for ws's policy with the shared cache and
// cost log, or an error when the backend is off or no key is found.
func jevClientFor(ws *resolve.Workspace, purpose string, timeout time.Duration) (*decide.Client, error) {
	cl, err := ws.Manifest.Decision.Client(filepath.Dir(ws.ManifestPath), timeout)
	if err != nil {
		return nil, err
	}
	cl.Cache = decide.NewCache(filepath.Join(decideDir(), "cache"))
	cl.Log = &decide.Log{Path: filepath.Join(decideDir(), "log.jsonl")}
	cl.Purpose = purpose
	cl.Workspace = ws.Name
	return cl, nil
}

func policyMode(p decide.Policy) string {
	if p.Enforcing() {
		return "enforce"
	}
	return "shadow"
}

// jevDecision runs the workspace's judgment rules over one tool call. It
// returns an empty decision unless the policy enforces and Jev escalates;
// in shadow mode it only logs. It is bounded by the hook deadline, and the
// breaker turns a Jev outage into one slow call per minute, not one per
// tool call.
func jevDecision(ws *resolve.Workspace, call policy.ToolCall) policy.Decision {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Decision.Enabled() {
		return policy.Decision{}
	}
	pol := ws.Manifest.Decision
	eff := effectiveGuard(ws)
	set := decide.BuildJudgeSet(pol.Rules, eff.Disable)
	for _, r := range set.Rules {
		// `tq guard set jev/<id> deny|ask` sets how far the rule may go.
		if a, ok := eff.Actions[r.ID]; ok {
			r.Max = decide.Action(a)
		}
	}
	sub := decide.Subject{Tool: call.Tool, Path: call.Path, Command: call.Command, Content: call.Content}
	rules := set.Candidates(sub)
	if len(rules) == 0 {
		return policy.Decision{}
	}
	br := jevBreaker()
	if br.Open() {
		return policy.Decision{}
	}
	cl, err := jevClientFor(ws, "guard", decide.HookTimeout)
	if err != nil {
		return policy.Decision{}
	}
	verdicts := decide.Judge(context.Background(), cl, pol, sub, rules)
	failed := false
	for _, v := range verdicts {
		if v.Escalation.Err != nil {
			failed = true
		}
	}
	if failed {
		br.Trip()
	} else {
		br.Reset()
	}
	judgmentLog().Write(decide.VerdictsJudgment(ws.Name, policyMode(pol), sub, verdicts))

	var d policy.Decision
	for _, v := range verdicts {
		var a policy.Action
		switch v.Escalation.Apply {
		case decide.Deny:
			a = policy.Deny
		case decide.Ask:
			a = policy.Ask
		default:
			continue
		}
		reason := fmt.Sprintf("%s (p=%.2f). It may be wrong: confirm only if this is intended", v.Rule.Reason, v.Escalation.P)
		d = policy.Merge(d, ruleDecision(a, v.Rule.ID, reason))
	}
	return d
}

// routeAgent applies subagent model routing to an Agent launch the policy
// already allowed. It writes nothing (Claude Code proceeds unchanged) unless
// the policy enforces and every guardrail holds, in which case it returns
// the original input plus a model as updatedInput — with no
// permissionDecision, so routing never approves anything a permission rule
// would have prompted for.
func routeAgent(w io.Writer, ws *resolve.Workspace, p hookPayload) error {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.Decision.Routing() {
		return nil
	}
	var input map[string]any
	if json.Unmarshal(unwrapToolInput(p.ToolInput), &input) != nil || input == nil {
		return nil
	}
	pol := ws.Manifest.Decision
	parent := decide.ParentTier(p.TranscriptPath)
	if ok, _ := decide.RouteEligible(input, parent); !ok {
		return nil // nothing to judge: no Jev call, no log noise
	}
	br := jevBreaker()
	if br.Open() {
		return nil
	}
	cl, err := jevClientFor(ws, "route", decide.HookTimeout)
	if err != nil {
		return nil
	}
	plan := decide.Route(context.Background(), cl, pol, input, parent)
	if plan.Err != nil {
		br.Trip()
	} else {
		br.Reset()
	}
	desc, _ := input["description"].(string)
	j := decide.Judgment{Workspace: ws.Name, Kind: "route", Mode: policyMode(pol), Tool: "Agent",
		Extra: map[string]string{"parent": plan.Parent, "pick": plan.Pick, "confidence": fmt.Sprintf("%.2f", plan.Confidence),
			"applied": fmt.Sprint(plan.Apply), "description": truncate(desc, 80)}}
	if plan.Err != nil {
		j.Error = plan.Err.Error()
	}
	judgmentLog().Write(j)
	if !plan.Apply {
		return nil
	}
	input["model"] = plan.Pick
	return json.NewEncoder(w).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PreToolUse",
			"updatedInput":      input,
			"additionalContext": fmt.Sprintf("tq routed this subagent to %s (Jev confidence %.2f; parent %s)", plan.Pick, plan.Confidence, plan.Parent),
		},
	})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
