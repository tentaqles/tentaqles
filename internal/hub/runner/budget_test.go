package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// maxTurns keeps a run going round after round, each round costing what it is given.
func maxTurns(h *harness, usage adapter.Usage, cost float64, known bool, advance time.Duration) step {
	return func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		h.clock.Advance(advance)
		return adapter.RunResult{StopReason: "error_max_turns", Usage: usage, CostUSD: cost, CostKnown: known, SessionID: "s1"}, nil
	}
}

func TestEachBudgetLimitStopsTheRun(t *testing.T) {
	cases := []struct {
		limit  string
		budget store.Budget
		usage  adapter.Usage
		cost   float64
		adv    time.Duration
		rounds int // rounds that run before the stop
	}{
		{"tokens", store.Budget{Tokens: 1000, USD: 100, Minutes: 1000, Rounds: 50}, adapter.Usage{Input: 400, Output: 100, CacheRead: 99999}, 0.01, 0, 2},
		{"usd", store.Budget{Tokens: 1e9, USD: 10, Minutes: 1000, Rounds: 50}, adapter.Usage{}, 6, 0, 2},
		{"minutes", store.Budget{Tokens: 1e9, USD: 100, Minutes: 120, Rounds: 50}, adapter.Usage{}, 0.01, 70 * time.Minute, 2},
		{"rounds", store.Budget{Tokens: 1e9, USD: 100, Minutes: 1000, Rounds: 3}, adapter.Usage{}, 0.01, 0, 3},
	}
	for _, c := range cases {
		t.Run(c.limit, func(t *testing.T) {
			h := newHarness(t)
			h.ad.steps = []step{maxTurns(h, c.usage, c.cost, true, c.adv)}
			card, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &c.budget)
			r, _ := h.svc.Run(context.Background(), card.ID)
			for i := 0; i < 10; i++ {
				h.tick()
			}
			got := h.run(r.ID)
			if got.Status != store.StatusOverBudget || got.StopReason != "budget: "+c.limit || len(h.ad.calls()) != c.rounds {
				t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
			}
			after, _ := h.st.Card(card.ID)
			pend, _ := h.st.PendingDecisions()
			if after.LockedByRun != "" || len(pend) != 1 || pend[0].Kind != store.KindPermission || strings.Join(pend[0].Options, ",") != "raise-and-resume,stop" {
				t.Fatalf("lock %q pending %+v", after.LockedByRun, pend)
			}
			if !strings.Contains(h.comments(card.ID), "Budget reached ("+c.limit+")") {
				t.Fatal(h.comments(card.ID))
			}
			acts, _ := h.st.Activity(100)
			stops := 0
			for _, a := range acts {
				if a.Verb == activity.VerbBudgetStop {
					stops++
				}
			}
			if stops != 1 {
				t.Fatalf("budget.stop logged %d times", stops)
			}
		})
	}
}

func TestBudgetRoundsStopsWithoutCost(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{maxTurns(h, adapter.Usage{}, 0, false, 0)}
	b := store.Budget{Tokens: 1e9, USD: 1, Minutes: 1000, Rounds: 3}
	card, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	r, _ := h.svc.Run(context.Background(), card.ID)
	for i := 0; i < 6; i++ {
		h.tick()
	}
	got := h.run(r.ID)
	if got.Status != store.StatusOverBudget || got.StopReason != "budget: rounds" || got.CostUSD != 0 || len(got.RoundLog) != 3 || !got.RoundLog[0].CostUnknown {
		t.Fatalf("%+v", got)
	}
	if strings.Count(h.comments(card.ID), "reported no cost") != 3 {
		t.Fatal(h.comments(card.ID))
	}
}
