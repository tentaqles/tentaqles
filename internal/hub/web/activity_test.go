package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestWindowStart(t *testing.T) {
	at := time.Date(2026, 10, 22, 15, 4, 0, 0, time.Local)
	if got := windowStart(1, at); !got.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.Local)) {
		t.Fatal(got)
	}
	if got := windowStart(7, at); !got.Equal(time.Date(2026, 10, 16, 0, 0, 0, 0, time.Local)) {
		t.Fatal(got)
	}
}

func TestActivityWindowAndFilters(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Export invoices"}, nil)
	real := time.Now()
	fixed := time.Date(real.Year(), real.Month(), real.Day(), 15, 0, 0, 0, time.Local)
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = time.Now })
	midnight := windowStart(1, fixed)
	at := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	st := svc.Store()
	for _, en := range []activity.Entry{
		{TS: at(midnight.Add(-time.Minute)), Actor: "tester", Verb: activity.VerbCardCreate, CardID: "c-yyyyyyyyyyyy", Data: map[string]any{"lane": "backlog"}},
		{TS: at(midnight.Add(9 * time.Hour)), Actor: "hub", Verb: activity.VerbRunEnd, CardID: c.ID, RunID: "r-aaaaaaaaaaaa",
			Data: map[string]any{"status": "finished", "cost_usd": 4.6, "tokens": 1900000, "kind": "feature"}},
		{TS: at(midnight.Add(10 * time.Hour)), Actor: "hub", Verb: activity.VerbDecisionAsk, CardID: c.ID, Data: map[string]any{"decision": "d1", "kind": "plan-approval"}},
		{TS: at(midnight.Add(10*time.Hour + 11*time.Minute)), Actor: "hub:tester", Verb: activity.VerbDecisionAnswer, CardID: c.ID,
			Data: map[string]any{"decision": "d1", "kind": "plan-approval", "choice": "approve", "actor_kind": "hub"}},
		{TS: at(midnight.Add(-10 * 24 * time.Hour)), Actor: "hub", Verb: activity.VerbRunEnd, CardID: c.ID, RunID: "r-bbbbbbbbbbbb",
			Data: map[string]any{"status": "failed", "cost_usd": 1.0, "kind": "fix"}},
		{TS: at(midnight.Add(11 * time.Hour)), Actor: "tester", Verb: activity.VerbCommentAdd, CardID: c.ID, Data: map[string]any{"author_kind": "human"}},
	} {
		if err := st.Log(en); err != nil {
			t.Fatal(err)
		}
	}
	ts := e.server(Options{})

	_, body, _ := get(t, ts, "/activity?ws=acme", nil)
	for _, want := range []string{
		`<div class="v">$4.60<small>1.9M tok</small></div>`, `1<small>1 finished</small>`, `<div class="v">11 min</div>`, `1<small>1 page</small>`,
		`feature <span class="num">$4.60</span>`, `<h2 class="day">Today</h2>`, `data-verb="run.end"`, `data-verb="comment.add"`,
		`<code>r-aaaaaaaaaaaa</code>`, `<a href="/cards/` + c.ID + `?ws=acme">Export invoices</a>`, "hub:tester answered plan approval on ",
		`class="on" aria-current="true">All</a>`, `class="on" aria-current="true">Today</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("today lacks %q", want)
		}
	}
	if strings.Contains(body, "c-yyyyyyyyyyyy") || strings.Contains(body, "r-bbbbbbbbbbbb") {
		t.Error("yesterday 23:59 and ten days ago are not today")
	}
	assertCSPSafe(t, "/activity", body)

	_, body, _ = get(t, ts, "/activity?ws=acme&days=7", nil)
	if !strings.Contains(body, `<h2 class="day">Yesterday</h2>`) || !strings.Contains(body, "c-yyyyyyyyyyyy") || strings.Contains(body, "r-bbbbbbbbbbbb") {
		t.Error("seven days: yesterday is in, ten days ago is not")
	}
	_, body, _ = get(t, ts, "/activity?ws=acme&days=30", nil)
	if !strings.Contains(body, "r-bbbbbbbbbbbb") || !strings.Contains(body, "1 finished, 1 failed") || !strings.Contains(body, "$5.60") {
		t.Error("thirty days include the old failed run")
	}
	_, body, _ = get(t, ts, "/activity?ws=acme&kind=runs", nil)
	if strings.Contains(body, `data-verb="comment.add"`) || strings.Contains(body, `data-verb="decision.answer"`) || !strings.Contains(body, `data-verb="run.end"`) {
		t.Error("kind=runs keeps run verbs only")
	}
	code, body, _ := get(t, ts, "/activity?ws=acme&kind=bogus&days=99", nil)
	if code != 200 || !strings.Contains(body, `class="on" aria-current="true">All</a>`) || !strings.Contains(body, `class="on" aria-current="true">Today</a>`) {
		t.Errorf("unknown filters fall back to All and Today: %d", code)
	}
}
