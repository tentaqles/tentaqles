package web

import (
	"fmt"
	"sort"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// tlItem is one line of the card page's timeline, built only from rows the hub holds: the card's
// runs with their round log, and its decisions.
type tlItem struct {
	When   string // RFC3339; the list is sorted by it
	Class  string // ok | r (in flight) | w (asked) | x (failed, cancelled, over budget) | ""
	Text   string
	Code   string // a machine value shown in monospace after the text
	Detail string // shown after the time
}

const timelineMax = 60

func timeline(runs []store.Run, decisions []store.Decision) []tlItem {
	var out []tlItem
	for _, r := range runs {
		queued := "Run queued"
		if r.OnRamp != "" {
			queued += " (" + r.OnRamp + ")"
		}
		out = append(out, tlItem{When: r.QueuedAt, Text: queued, Code: r.ID})
		for _, rr := range r.RoundLog {
			it := tlItem{When: rr.StartedAt, Class: "ok", Text: fmt.Sprintf("Round %d", rr.N)}
			if rr.Outcome != "" {
				it.Text += ": " + words(rr.Outcome)
			}
			if rr.ExitCode != 0 {
				it.Class = "x"
			}
			cost := fmt.Sprintf("$%.2f", rr.CostUSD)
			if rr.CostUnknown {
				cost = "cost unknown"
			}
			it.Detail = fmt.Sprintf("%s · %.0f s", cost, rr.Seconds)
			out = append(out, it)
		}
		if r.Status == store.StatusRunning {
			out = append(out, tlItem{When: r.UpdatedAt, Class: "r", Text: "A round is running", Detail: fmt.Sprintf("$%.2f so far", r.CostUSD)})
		}
		if r.EndedAt != "" {
			it := tlItem{When: r.EndedAt, Class: "x", Text: "Run " + statusLabel(r.Status), Detail: clip(r.StopReason, 120)}
			if r.Status == store.StatusFinished {
				it.Class = "ok"
			}
			out = append(out, it)
		}
	}
	for _, d := range decisions {
		kind := laneTitle(words(d.Kind))
		out = append(out, tlItem{When: d.AskedAt, Class: "w", Text: kind + " asked", Code: d.ID})
		if d.Status == store.DecisionAnswered {
			out = append(out, tlItem{When: d.AnsweredAt, Class: "ok", Text: kind + " answered: " + d.Choice, Detail: d.Actor})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return tsBefore(out[i].When, out[j].When) })
	if len(out) > timelineMax {
		out = out[len(out)-timelineMax:]
	}
	return out
}

// tsBefore orders RFC3339 times (any offset); a missing or unreadable time sorts last.
func tsBefore(a, b string) bool {
	ta, oka := parseTS(a)
	tb, okb := parseTS(b)
	switch {
	case oka && okb:
		return ta.Before(tb)
	case oka:
		return true
	}
	return false
}
