package runner

import (
	"context"
	"math"
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

func TestMidRoundBreachStopsOverBudget(t *testing.T) {
	h := newHarness(t)
	var limits []adapter.Limit
	h.ad.steps = []step{
		func(_ context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
			limits = append(limits, req.Limit)
			h.clock.Advance(30 * time.Minute)
			return adapter.RunResult{StopReason: "error_max_turns", Usage: adapter.Usage{Input: 1000, Output: 500, CacheRead: 9999}, CostUSD: 6, CostKnown: true, UsageKnown: true, SessionID: "s1"}, nil
		},
		func(_ context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
			limits = append(limits, req.Limit)
			// the adapter stopped the round at what was left of the budget
			return adapter.RunResult{ExitCode: 1, StopReason: "over budget: usd", OverBudget: "usd", Usage: adapter.Usage{Input: 900}, CostUSD: 4.2,
				CostKnown: true, UsageKnown: true, SessionID: "s-mid"}, nil
		},
	}
	b := store.Budget{Tokens: 10_000, USD: 10, Minutes: 120, Rounds: 12}
	card, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	r, _ := h.svc.Run(context.Background(), card.ID)
	for i := 0; i < 5; i++ {
		h.tick()
	}
	if len(limits) != 2 || limits[0] != (adapter.Limit{Tokens: 10_000, USD: 10, Minutes: 120}) || limits[1] != (adapter.Limit{Tokens: 8_500, USD: 4, Minutes: 90}) {
		t.Fatalf("the adapter gets what is left of the budget: %+v", limits)
	}
	got := h.run(r.ID)
	if got.Status != store.StatusOverBudget || got.StopReason != "budget: usd" || len(h.ad.calls()) != 2 {
		t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
	}
	if got.SessionID != "s-mid" || math.Abs(got.CostUSD-10.2) > 1e-9 || got.Usage.Input != 1900 || len(got.RoundLog) != 2 || got.RoundLog[1].Outcome != "over-budget" {
		t.Fatalf("the partial round is recorded and its session kept: %+v", got)
	}
	pend, _ := h.st.PendingDecisions()
	if len(pend) != 1 || strings.Join(pend[0].Options, ",") != "raise-and-resume,stop" {
		t.Fatalf("pending %+v", pend)
	}
	if cs := h.comments(card.ID); !strings.Contains(cs, "Budget reached (usd)") || !strings.Contains(cs, "Round 2 was stopped inside the round") {
		t.Fatal(cs)
	}
	if strings.Contains(h.comments(card.ID), "agent: ") {
		t.Fatal("a stopped round posts no transcript")
	}
	// raise-and-resume continues the same session
	out, err := h.svc.Answer(context.Background(), card.ID, pend[0].ID, "raise-and-resume", "usd=30")
	if err != nil || out.Resumed == nil || out.Resumed.SessionID != "s-mid" {
		t.Fatalf("resume %+v %v", out.Resumed, err)
	}
}

func TestRoundLimit(t *testing.T) {
	if got := RoundLimit(store.Run{}); got != (adapter.Limit{}) {
		t.Fatalf("no budget, no limit: %+v", got)
	}
	r := store.Run{Budget: store.Budget{Tokens: 100, USD: 1, Minutes: 1}, Usage: store.Usage{Input: 200}, CostUSD: 2, Seconds: 120}
	if got := RoundLimit(r); got.Tokens != 1 || got.USD <= 0 || got.USD > 1e-6 || got.Minutes <= 0 || got.Minutes > 1e-6 {
		t.Fatalf("a used-up limit leaves the smallest share, never none: %+v", got)
	}
}
