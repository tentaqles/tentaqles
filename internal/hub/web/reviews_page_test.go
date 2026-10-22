package web

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestReviewsPageListsWaitingAndFinished(t *testing.T) {
	e, c, _ := reviewEnv(t)
	svc := e.svcs["acme"]
	ctx := context.Background()
	f, _ := svc.Add(ctx, store.NewCard{Title: "Finished, not closed"}, nil)
	rf, _, _ := svc.Enqueue(ctx, f.ID, "test")
	rf.Status = store.StatusFinished
	svc.Store().SaveRun(rf)
	svc.Store().Release(f.ID, rf.ID)
	svc.Move(ctx, f.ID, "review", false)
	g, _ := svc.Add(ctx, store.NewCard{Title: "Already done"}, nil)
	rg, _, _ := svc.Enqueue(ctx, g.ID, "test")
	rg.Status = store.StatusFinished
	svc.Store().SaveRun(rg)
	svc.Store().Release(g.ID, rg.ID)
	svc.Move(ctx, g.ID, "done", false)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/reviews?ws=acme", nil)
	waiting := between(body, "<h2>Waiting on your review</h2>", "</section>")
	finished := between(body, "<h2>Finished, not closed yet</h2>", "</section>")
	if !strings.Contains(waiting, `href="/cards/`+c.ID+`/review?ws=acme"`) || !strings.Contains(waiting, "Export invoices") {
		t.Fatalf("waiting:\n%s", waiting)
	}
	if !strings.Contains(finished, `href="/cards/`+f.ID+`/review?ws=acme"`) || strings.Contains(body, "Already done") {
		t.Fatalf("finished:\n%s", finished)
	}
	if !strings.Contains(body, `href="/reviews?ws=acme" class="on" aria-current="page"`) {
		t.Error("Reviews is the current page")
	}
	assertCSPSafe(t, "/reviews", body)
}

func TestReviewVerdictBand(t *testing.T) {
	e, c, _ := reviewEnv(t)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	for _, want := range []string{`<div class="verdict ok">`, "Everything the gate recorded says this is done", "1 of 1 checkpoints passed, 1 reviews PASS, DoD ok",
		"nothing here comes from what the agent said", `<div class="actbar">`, `href="/reviews?ws=acme" class="on" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("review lacks %q", want)
		}
	}
	if !strings.Contains(between(body, `<div class="actbar">`, "</div>"), `/review/approve?ws=acme`) {
		t.Error("the approve form sits in the action bar")
	}
	assertCSPSafe(t, "/review", body)
	e.gate.mu.Lock()
	e.gate.states["demo"].Records.DoD.OK = false
	e.gate.mu.Unlock()
	_, body, _ = get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if !strings.Contains(body, `<div class="verdict bad">`) || !strings.Contains(body, "The definition of done is not met") {
		t.Fatal("a failing DoD turns the band red")
	}
}

func TestReviewWithoutRecordsHasAWaitBand(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Status = store.StatusFinished
	svc.Store().SaveRun(r)
	svc.Store().Release(c.ID, r.ID)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/cards/"+c.ID+"/review?ws=acme", nil)
	if !strings.Contains(body, `<div class="verdict wait">`) || !strings.Contains(body, "There are no gate records for this run") || !strings.Contains(body, "/review/done?ws=acme") {
		t.Fatal(body)
	}
}

func TestReviewExportHasNoActionBar(t *testing.T) {
	e, c, r := reviewEnv(t)
	path, err := ExportReview(context.Background(), e.svcs["acme"], c, r)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	html := string(raw)
	for _, bad := range []string{`class="actbar"`, "<form", "<script", "/static/", "<link"} {
		if strings.Contains(html, bad) {
			t.Errorf("export carries %q", bad)
		}
	}
	for _, want := range []string{`<body class="standalone">`, `<h1 class="rv-title">Review: Export invoices</h1>`, `<div class="verdict ok">`, "<style>"} {
		if !strings.Contains(html, want) {
			t.Errorf("export lacks %q", want)
		}
	}
}
