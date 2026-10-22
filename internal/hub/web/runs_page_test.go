package web

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestRunsPageListsInFlightAndRecent(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	ctx := context.Background()
	a, _ := svc.Add(ctx, store.NewCard{Title: "Alpha"}, nil)
	b, _ := svc.Add(ctx, store.NewCard{Title: "Beta"}, nil)
	ra, _, _ := svc.Enqueue(ctx, a.ID, "test")
	rb, _, _ := svc.Enqueue(ctx, b.ID, "test")
	rb.Status, rb.CostUSD = store.StatusFinished, 4.6
	svc.Store().SaveRun(rb)
	svc.Store().Release(b.ID, rb.ID)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/runs?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	inFlight := between(body, "<h2>In flight</h2>", "</section>")
	recent := between(body, "<h2>Recent</h2>", "</section>")
	for _, want := range []string{`href="/runs/` + ra.ID + `?ws=acme"`, ">Alpha</a>", `<span class="pill p-queue st-queued">queued</span>`} {
		if !strings.Contains(inFlight, want) {
			t.Errorf("in flight lacks %q:\n%s", want, inFlight)
		}
	}
	for _, want := range []string{`href="/runs/` + rb.ID + `?ws=acme"`, ">Beta</a>", "$4.60", `<span class="pill p-done st-finished">finished</span>`} {
		if !strings.Contains(recent, want) {
			t.Errorf("recent lacks %q:\n%s", want, recent)
		}
	}
	if !strings.Contains(body, `href="/runs?ws=acme" class="on" aria-current="page"`) {
		t.Error("Runs is the current page")
	}
	assertCSPSafe(t, "/runs", body)
}

func TestRunPageRestyled(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/runs/"+r.ID+"?ws=acme", nil)
	for _, want := range []string{`<dl class="kv">`, `<pre class="log tall" id="log" data-run="` + r.ID + `"`,
		`action="/runs/` + r.ID + `/cancel?ws=acme"`, `href="/runs?ws=acme" class="on" aria-current="page"`, "0% of the dollar budget"} {
		if !strings.Contains(body, want) {
			t.Errorf("run page lacks %q", want)
		}
	}
	assertCSPSafe(t, "/runs/"+r.ID, body)
}

func TestRunPageTimelineOrdersRoundsAndDecisions(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	st := svc.Store()
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.RoundLog = []store.RoundRecord{
		{N: 1, StartedAt: "2026-10-21T10:00:00Z", CostUSD: 1, Outcome: "continue"},
		{N: 2, StartedAt: "2026-10-21T10:20:00Z", CostUSD: 0.5, Outcome: "finished"},
	}
	st.SaveRun(r)
	st.UpsertDecision(store.Decision{ID: "d-timeline1", CardID: c.ID, RunID: r.ID, Kind: store.KindQuestion, Checkpoint: "1",
		Status: store.DecisionAnswered, Choice: "yes", Actor: "hub:tester", AskedAt: "2026-10-21T10:10:00Z", AnsweredAt: "2026-10-21T10:12:00Z"})
	st.UpsertDecision(store.Decision{ID: "d-otherrun1", CardID: c.ID, RunID: "r-somethingelse", Kind: store.KindQuestion,
		AskedAt: "2026-10-21T10:05:00Z"})
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/runs/"+r.ID+"?ws=acme", nil)
	tl := between(body, `<ul class="tl">`, "</ul>")
	if n := strings.Count(tl, "<li"); n != 3 {
		t.Fatalf("want 3 timeline entries, got %d:\n%s", n, tl)
	}
	r1, dec, r2 := strings.Index(tl, "Round 1 started"), strings.Index(tl, "answered yes"), strings.Index(tl, "Round 2 started")
	if r1 < 0 || dec < 0 || r2 < 0 || !(r1 < dec && dec < r2) {
		t.Fatalf("timeline out of order (%d %d %d):\n%s", r1, dec, r2, tl)
	}
	if !strings.Contains(tl, "<code>hub:tester</code>") || !strings.Contains(tl, "$1.00") {
		t.Fatalf("timeline lacks actor or cost:\n%s", tl)
	}
	if strings.Index(body, `<ul class="tl">`) > strings.Index(body, "<h3 class=\"sub\">Rounds</h3>") {
		t.Fatal("the timeline sits above the round table")
	}
	assertCSPSafe(t, "/runs/"+r.ID, body)
}
