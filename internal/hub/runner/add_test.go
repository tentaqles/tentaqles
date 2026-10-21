package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// TestAgentCannotSetBudgetThroughFrontMatter: the budget argument is not the only way in; a body
// carrying a budget: front-matter key is refused from an agent too (spec §15).
func TestAgentCannotSetBudgetThroughFrontMatter(t *testing.T) {
	h := newHarness(t)
	agent := WithAgent(context.Background())
	if _, err := h.svc.Add(agent, store.NewCard{Title: "x", Body: "---\nbudget: rounds=999\n---\nDo it."}, nil); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent set a budget through front matter: %v", err)
	}
	if cs, _ := h.st.ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
	if _, err := h.svc.Add(agent, store.NewCard{Title: "y", Body: "---\nnote: hi\n---\nplain"}, nil); err != nil {
		t.Fatalf("other front matter is fine: %v", err)
	}
	if _, err := h.svc.Add(context.Background(), store.NewCard{Title: "z", Body: "---\nbudget: rounds=3\n---\n"}, nil); err != nil {
		t.Fatalf("a human may: %v", err)
	}
}

// TestAgentAddsOnlyIntoPlanningLanes: an agent may create a card in a backlog, todo or other
// lane, never straight into active, review or done.
func TestAgentAddsOnlyIntoPlanningLanes(t *testing.T) {
	h := newHarness(t)
	agent := WithAgent(context.Background())
	for _, lane := range []string{"done", "in progress", "review"} {
		if _, err := h.svc.Add(agent, store.NewCard{Title: "x", Lane: lane, Tags: []string{store.TagAgentReady}}, nil); !errors.Is(err, ErrHumanOnly) {
			t.Fatalf("lane %q: %v", lane, err)
		}
	}
	for _, lane := range []string{"todo", "backlog", ""} {
		if c, err := h.svc.Add(agent, store.NewCard{Title: "x", Lane: lane}, nil); err != nil {
			t.Fatalf("lane %q: %v", lane, err)
		} else if lane == "" && c.Lane != "backlog" {
			t.Fatalf("default lane %q", c.Lane)
		}
	}
	if _, err := h.svc.Add(agent, store.NewCard{Title: "x", Lane: "nope"}, nil); err == nil || errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an unknown lane is the store's error: %v", err)
	}
}

// TestHumanAddIntoActiveLaneEnqueues: creating an agent-ready card straight into an active lane
// is entering it (spec 8.1), so exactly one run is queued.
func TestHumanAddIntoActiveLaneEnqueues(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	c, err := h.svc.Add(ctx, store.NewCard{Title: "x", Lane: "in progress", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := h.st.RunsForCard(c.ID)
	if len(runs) != 1 || runs[0].Status != store.StatusQueued {
		t.Fatalf("want one queued run, got %+v", runs)
	}
	d, err := h.svc.Show(ctx, c.ID)
	if err != nil || len(d.Runs) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	plain, _ := h.svc.Add(ctx, store.NewCard{Title: "y", Lane: "in progress"}, nil)
	if runs, _ := h.st.RunsForCard(plain.ID); len(runs) != 0 {
		t.Fatal("a card without agent-ready is not run")
	}
	backlog, _ := h.svc.Add(ctx, store.NewCard{Title: "z", Tags: []string{store.TagAgentReady}}, nil)
	if runs, _ := h.st.RunsForCard(backlog.ID); len(runs) != 0 {
		t.Fatal("a backlog card is not run")
	}
}
