package web

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestClientImplementsOpsOverTheAPI(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	info := serveinfo.Info{Addr: u.Host, Port: port, LocalKey: "lk", Workspaces: []string{"acme"}}
	cl := NewClient(info, "acme", false)
	ctx := context.Background()
	if err := cl.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := cl.Add(ctx, store.NewCard{Title: "via client", Tags: []string{store.TagAgentReady}}, nil)
	if err != nil || c.ID == "" {
		t.Fatalf("%+v %v", c, err)
	}
	out, err := cl.Move(ctx, c.ID, "in progress", false)
	if err != nil || out.Enqueued == nil {
		t.Fatalf("%+v %v", out, err)
	}
	r := *out.Enqueued
	r.Status = store.StatusRunning
	e.svcs["acme"].Store().SaveRun(r)
	_, err = cl.Move(ctx, c.ID, "backlog", false)
	var active *runner.RunActiveError
	if !errors.As(err, &active) || active.Run.ID != r.ID {
		t.Fatalf("want RunActiveError, got %v", err)
	}
	e.pendingDecision("acme", c, store.KindPlanApproval)
	agent := NewClient(info, "acme", true)
	if _, err := agent.Answer(ctx, c.ID, "d1a2b3c4", "approve", ""); !errors.Is(err, runner.ErrHumanOnly) {
		t.Fatalf("want ErrHumanOnly, got %v", err)
	}
	views, err := cl.List(ctx, store.CardFilter{}, true)
	if err != nil || len(views) != 1 || views[0].Waiting != 1 {
		t.Fatalf("%+v %v", views, err)
	}
	items, err := cl.Waiting(ctx)
	if err != nil || len(items) != 1 || items[0].Decision.ID != "d1a2b3c4" {
		t.Fatalf("%+v %v", items, err)
	}
	var _ runner.Ops = cl
}
