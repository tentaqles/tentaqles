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
