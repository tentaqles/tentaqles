package decide

import (
	"context"
	"sort"
)

// Risk triage decides how hard a change should be reviewed. Jev answers a
// fixed set of yes/no risk questions about a diff in one call:
//
//   - every answer below the warn threshold  -> "low": one light review pass;
//   - any answer at or above it              -> "high": the full review;
//   - Jev unreachable or an answer missing   -> "high" (fail safe: when in
//     doubt, review more, never less).

// TriageQuestions are the risk questions, keyed by flag name.
func TriageQuestions() map[string]Question {
	q := func(s string) Question { return Noul(s) }
	return map[string]Question{
		"auth":        q("Does this change touch authentication, authorization, sessions, or permission checks?"),
		"money":       q("Does this change touch payments, billing, pricing, invoices, or other money movement?"),
		"schema":      q("Does this change alter a database schema (tables, columns, indexes, constraints)?"),
		"data_loss":   q("Could this change delete, overwrite, or corrupt existing data?"),
		"migration":   q("Does this change add or edit a database migration?"),
		"rls":         q("Does this change touch row-level security, database grants, or tenant isolation?"),
		"secrets":     q("Does this change handle secrets, API keys, tokens, or credentials?"),
		"api":         q("Does this change alter a public API, webhook, or contract that other systems depend on?"),
		"state":       q("Does this change alter concurrency, caching, queues, or shared state in a way that could race or go stale?"),
		"destructive": q("Does this change run or add destructive SQL or shell commands (drop, truncate, rm -rf, force push)?"),
		"n8n_creds":   q("Does this change put credentials directly into an n8n workflow or automation config?"),
	}
}

// TriageResult is the outcome for one diff.
type TriageResult struct {
	Risk  string             `json:"risk"` // low | high
	Flags []string           `json:"flags,omitempty"`
	P     map[string]float64 `json:"p,omitempty"`
	Error string             `json:"error,omitempty"`
	// Truncated is set when the diff was cut to fit the request cap.
	Truncated bool `json:"truncated,omitempty"`
}

// maxTriageDiff leaves room for the questions and framing under
// MaxStateBytes.
const maxTriageDiff = 60_000

// Triage classifies a diff. It never returns "low" on an error.
func Triage(ctx context.Context, c *Client, p Policy, diff string) TriageResult {
	res := TriageResult{Risk: "high"}
	if len(diff) > maxTriageDiff {
		// Keep the head: file headers and the first hunks carry the most
		// signal. Mark the cut so the reviewer knows triage saw a prefix.
		diff = diff[:maxTriageDiff] + "\n[diff truncated for triage]"
		res.Truncated = true
	}
	qs := TriageQuestions()
	resp, err := c.Ask(ctx, map[string]string{"diff": diff}, qs)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	_, warn := p.Thresholds()
	res.P = map[string]float64{}
	for id := range qs {
		prob, err := resp.NoulOf(id)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.P[id] = prob
		if prob >= warn {
			res.Flags = append(res.Flags, id)
		}
	}
	sort.Strings(res.Flags)
	// A truncated diff is never "low": the unseen part may hold the risk.
	if len(res.Flags) == 0 && !res.Truncated {
		res.Risk = "low"
	}
	return res
}
