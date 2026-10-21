package web

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestRunPageShowsBudgetUsageAndLogTail(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Rounds, r.CostUSD = 2, 1.25
	r.Usage = store.Usage{Input: 1500, Output: 500}
	r.RoundLog = []store.RoundRecord{{N: 1, CostUSD: 1, Seconds: 60}, {N: 2, CostUSD: 0.25, Seconds: 30, CostUnknown: true}}
	svc.Store().SaveRun(r)
	os.WriteFile(r.LogPath, []byte("=== round 2\nlast line of the log\n"), 0o600)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/runs/"+r.ID+"?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{`data-run="` + r.ID + `"`, "last line of the log", `id="log"`, "$1.25", "$10.00", "2.0k", "cost unknown", "st-queued"} {
		if !strings.Contains(body, want) {
			t.Fatalf("run page lacks %q", want)
		}
	}
}

func TestActivityPageListsVerbs(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "x"}, nil)
	svc.Comment(context.Background(), c.ID, "hello")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/activity?ws=acme", nil)
	if !strings.Contains(body, "card.create") || !strings.Contains(body, "comment.add") || !strings.Contains(body, c.ID) {
		t.Fatal(body)
	}
}
