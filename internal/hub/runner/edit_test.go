package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func strp(s string) *string { return &s }

func lastEntry(t *testing.T, h *harness, verb string) activity.Entry {
	t.Helper()
	es, err := h.st.Activity(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Verb == verb {
			return e
		}
	}
	t.Fatalf("no %s entry in %+v", verb, es)
	return activity.Entry{}
}

func TestUpdateCardHumanEditsEverything(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	tags := []string{"needs-spec", "ops@example.com", "needs-spec"}
	got, err := h.svc.UpdateCard(context.Background(), c.ID, store.CardUpdate{Title: strp("Export as CSV"), Body: strp("Per month."), ProjectDir: strp("apps/billing"), Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Export as CSV" || got.Body != "Per month." || len(got.Tags) != 2 || got.Tags[0] != "needs-spec" || strings.Contains(strings.Join(got.Tags, ","), "@example.com") {
		t.Fatalf("%+v", got)
	}
	if e := lastEntry(t, h, activity.VerbCardUpdate); e.Actor != "tester" {
		t.Fatalf("a human edit is logged as the human: %+v", e)
	}
}

func TestAgentEditsTagsOnlyInPlanningLanes(t *testing.T) {
	h := newHarness(t)
	agent := WithAgent(context.Background())
	c := h.card("feature")
	tags := []string{"needs-spec"}
	got, err := h.svc.UpdateCard(agent, c.ID, store.CardUpdate{Tags: &tags})
	if err != nil || len(got.Tags) != 1 || got.Tags[0] != "needs-spec" {
		t.Fatalf("an agent may tag a backlog card: %+v %v", got, err)
	}
	if e := lastEntry(t, h, activity.VerbCardUpdate); e.Actor != "agent" {
		t.Fatalf("the edit is logged as the agent: %+v", e)
	}
	if _, err := h.svc.UpdateCard(agent, c.ID, store.CardUpdate{Title: strp("renamed")}); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent may not rename a card: %v", err)
	}
	if _, err := h.svc.UpdateCard(agent, c.ID, store.CardUpdate{Body: strp("x"), Tags: &tags}); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("a body edit refuses the whole update: %v", err)
	}
	for _, lane := range []string{"in progress", "review", "done"} {
		if _, err := h.svc.Move(context.Background(), c.ID, lane, true); err != nil {
			t.Fatal(err)
		}
		other := []string{"security"}
		if _, err := h.svc.UpdateCard(agent, c.ID, store.CardUpdate{Tags: &other}); !errors.Is(err, ErrHumanOnly) {
			t.Fatalf("%s: an agent may not tag a card outside the planning lanes: %v", lane, err)
		}
		if cur, _ := h.st.Card(c.ID); len(cur.Tags) != 1 || cur.Tags[0] != "needs-spec" {
			t.Fatalf("%s: the tags are unchanged: %+v", lane, cur.Tags)
		}
	}
}

func TestUpdateCardNeedsAField(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	if _, err := h.svc.UpdateCard(context.Background(), c.ID, store.CardUpdate{}); err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("%v", err)
	}
	if _, err := h.svc.UpdateCard(context.Background(), "c-aaaaaaaaaaaa", store.CardUpdate{Title: strp("x")}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

// TestTaggingACardInProgressStartsNothing: agent-ready acts when a card enters an active lane;
// adding the tag to a card already there starts no run.
func TestTaggingACardInProgressStartsNothing(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	if out, err := h.svc.Move(context.Background(), c.ID, "in progress", false); err != nil || out.Enqueued != nil {
		t.Fatalf("%+v %v", out, err)
	}
	tags := []string{store.TagAgentReady}
	if _, err := h.svc.UpdateCard(context.Background(), c.ID, store.CardUpdate{Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	if runs, _ := h.st.RunsForCard(c.ID); len(runs) != 0 {
		t.Fatalf("a run started: %+v", runs)
	}
}

func intp(n int) *int    { return &n }
func boolp(b bool) *bool { return &b }

func TestUpdateSettingsHumanOnly(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.UpdateSettings(WithAgent(context.Background()), SettingsUpdate{Concurrency: intp(5)})
	if !errors.Is(err, ErrHumanOnly) || h.st.Concurrency() != store.DefaultConcurrency {
		t.Fatalf("%v %d", err, h.st.Concurrency())
	}
}

func TestUpdateSettingsRoundTripAndLog(t *testing.T) {
	h := newHarness(t)
	b := store.Budget{Tokens: 3_000_000, USD: 15, Minutes: 90, Rounds: 8}
	got, err := h.svc.UpdateSettings(context.Background(), SettingsUpdate{Concurrency: intp(3), DefaultBudget: &b, Notify: boolp(false)})
	if err != nil {
		t.Fatal(err)
	}
	if got != (Settings{Concurrency: 3, DefaultBudget: b, Notify: false}) || h.svc.Settings() != got {
		t.Fatalf("%+v", got)
	}
	e := lastEntry(t, h, activity.VerbSettingsUpdate)
	if e.Actor != "tester" || !strings.Contains(strings.Join(anyStrings(e.Data["fields"]), ","), "default_budget") {
		t.Fatalf("%+v", e)
	}
}

func anyStrings(v any) []string {
	var out []string
	xs, _ := v.([]any)
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestUpdateSettingsValidatesBeforeWriting(t *testing.T) {
	h := newHarness(t)
	bad := store.Budget{Tokens: 1000, USD: 0, Minutes: 10, Rounds: 1}
	for _, u := range []SettingsUpdate{
		{Concurrency: intp(0)},
		{Concurrency: intp(store.MaxConcurrency + 1)},
		{Concurrency: intp(4), DefaultBudget: &bad},
		{},
	} {
		if _, err := h.svc.UpdateSettings(context.Background(), u); err == nil {
			t.Fatalf("%+v was accepted", u)
		}
	}
	if h.st.Concurrency() != store.DefaultConcurrency || h.st.DefaultBudget() != store.DefaultBudget {
		t.Fatal("a refused update writes nothing, not even its valid fields")
	}
}

func TestUpdateSettingsFailureChangesNothing(t *testing.T) {
	h := newHarness(t)
	if _, err := h.st.DB.Exec(`CREATE TRIGGER no_activity BEFORE INSERT ON activity BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	b := store.Budget{Tokens: 1000, USD: 2, Minutes: 10, Rounds: 1}
	if _, err := h.svc.UpdateSettings(context.Background(), SettingsUpdate{Concurrency: intp(3), DefaultBudget: &b, Notify: boolp(false)}); err == nil {
		t.Fatal("a failed write was reported as saved")
	}
	if h.st.Concurrency() != store.DefaultConcurrency || h.st.DefaultBudget() != store.DefaultBudget || !h.st.Notify() {
		t.Fatal("a failed update changed some fields")
	}
}
