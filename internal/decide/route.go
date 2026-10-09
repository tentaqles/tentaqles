package decide

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Model routing for subagent launches (the Agent tool). Jev picks a tier
// for the task; tq applies it only when every guardrail holds:
//
//   - the caller set no model, and the subagent type has no model of its
//     own (only general-purpose launches are routed);
//   - the pick is strictly cheaper than the parent session's model;
//   - Jev's confidence is at least the policy's block threshold;
//   - the policy is in enforce mode (shadow mode logs the pick only).
//
// Anything else — an error, a timeout, an unknown parent model — leaves the
// launch exactly as Claude asked for it.

var tierRank = map[string]int{"haiku": 1, "sonnet": 2, "opus": 3}

// routableTypes are subagent types without a model of their own.
var routableTypes = map[string]bool{"": true, "general-purpose": true, "claude": true}

// RouteQuestion is the choice question asked about a task prompt.
func RouteQuestion() Question {
	return Question{
		Type:         "choice",
		Instructions: "Which model tier is enough to do this subagent task well? Pick the cheapest tier that will not lower the quality of the result.",
		Criteria: map[string]string{
			"haiku":  "Mechanical work: find files or symbols, read and summarise, run a command and report, apply a precise edit that is fully specified.",
			"sonnet": "Ordinary engineering: implement or fix code across a few files, write tests, review a diff, research a question with several steps.",
			"opus":   "Hard judgment: architecture or security decisions, ambiguous requirements, subtle debugging, large refactors, or anything where a mistake is costly.",
		},
	}
}

// RoutePlan is what the router decided for one launch.
type RoutePlan struct {
	Eligible   bool    // the launch may be routed at all
	Skip       string  // why not, when not eligible
	Parent     string  // parent tier (haiku|sonnet|opus) or ""
	Pick       string  // Jev's tier
	Confidence float64 //
	Apply      bool    // set the model on the launch
	Err        error
}

// RouteEligible checks the guardrails that need no Jev call.
func RouteEligible(input map[string]any, parentTier string) (bool, string) {
	if m, _ := input["model"].(string); strings.TrimSpace(m) != "" {
		return false, "model set by caller"
	}
	st, _ := input["subagent_type"].(string)
	if !routableTypes[st] {
		return false, "subagent type " + st + " has its own model"
	}
	if tierRank[parentTier] == 0 {
		return false, "parent model unknown"
	}
	if parentTier == "haiku" {
		return false, "parent is already the cheapest tier"
	}
	if p, _ := input["prompt"].(string); strings.TrimSpace(p) == "" {
		return false, "no prompt"
	}
	return true, ""
}

// Route asks Jev for a tier and applies the guardrails.
func Route(ctx context.Context, c *Client, p Policy, input map[string]any, parentTier string) RoutePlan {
	plan := RoutePlan{Parent: parentTier}
	plan.Eligible, plan.Skip = RouteEligible(input, parentTier)
	if !plan.Eligible {
		return plan
	}
	prompt, _ := input["prompt"].(string)
	desc, _ := input["description"].(string)
	resp, err := c.Ask(ctx, map[string]string{"description": desc, "task": prompt}, map[string]Question{"tier": RouteQuestion()})
	if err != nil {
		plan.Err = err
		return plan
	}
	a, ok := resp.Answers["tier"]
	if !ok || tierRank[a.Choice] == 0 {
		plan.Err = errMissingTier
		return plan
	}
	plan.Pick, plan.Confidence = a.Choice, a.Confidence
	block, _ := p.Thresholds()
	plan.Apply = p.Enforcing() && tierRank[a.Choice] < tierRank[parentTier] && a.Confidence >= block
	return plan
}

var errMissingTier = &routeErr{"jev: no usable tier answer"}

type routeErr struct{ s string }

func (e *routeErr) Error() string { return e.s }

// TierOf maps a model id or alias to a tier, or "".
func TierOf(model string) string {
	m := strings.ToLower(model)
	for t := range tierRank {
		if strings.Contains(m, t) {
			return t
		}
	}
	return ""
}

// ParentTier reads the newest assistant message's model from a transcript
// (at most its last 2 MiB) and returns its tier, or "".
func ParentTier(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	const tail = 2 << 20
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		_, _ = f.Seek(-tail, io.SeekEnd)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	last := ""
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"model"`) {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Type == "assistant" && rec.Message.Model != "" {
			if t := TierOf(rec.Message.Model); t != "" {
				last = t
			}
		}
	}
	return last
}
