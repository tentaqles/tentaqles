package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestBoardCardAnatomy(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	svc.Add(context.Background(), store.NewCard{Title: "Export invoices", Kind: "feature", Tags: []string{store.TagAgentReady}, Body: "---\nbudget: usd=15\n---\nCSV."}, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range []string{
		`<div class="lane" data-lane="in progress" data-kind="active" data-title="In progress"`,
		`In progress <span class="c">0</span>`, `data-new="backlog"`, `data-new="todo"`,
		`<span class="chip">feature</span><span class="chip ready">agent-ready</span>`,
		`budget <span class="num">$15.00</span>`, `data-ready`, `Finished runs wait here for your review.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board lacks %q", want)
		}
	}
	if strings.Contains(body, `data-new="in progress"`) || strings.Contains(body, `data-new="done"`) {
		t.Error("only planning lanes offer the + button")
	}
	assertCSPSafe(t, "/", body)
}

func TestBoardRunningCardShowsRoundAndBudgetBar(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Add --json", Tags: []string{store.TagAgentReady}}, nil)
	out, err := svc.Move(context.Background(), c.ID, "in progress", false)
	if err != nil || out.Enqueued == nil {
		t.Fatalf("%+v %v", out, err)
	}
	r := *out.Enqueued
	r.Status, r.Rounds, r.CostUSD, r.Seconds = store.StatusRunning, 3, 3.1, 840
	svc.Store().SaveRun(r)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range []string{`<span class="pill p-run st-running">running</span><span>round 3 of 12</span>`, `style="width:31%"`,
		`<span class="id">` + r.ID + `</span><span class="num">$3.10</span><span>14 min</span>`, `data-status="running"`, `data-status-text="round 3 of 12"`} {
		if !strings.Contains(body, want) {
			t.Errorf("running card lacks %q", want)
		}
	}
	_, tileBody, _ := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	if !strings.Contains(tileBody, `class="bar"`) || !strings.Contains(tileBody, `<span class="id">`+r.ID+`</span>`) {
		t.Errorf("a running card's tile shows the bar and the run meta:\n%s", tileBody)
	}
}

func TestBoardDoneCardShowsCostOnly(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	ctx := context.Background()
	c, _ := svc.Add(ctx, store.NewCard{Title: "Mask notification bodies", Kind: "feature", Tags: []string{store.TagAgentReady}, Body: "---\nbudget: usd=15\n---\nx"}, nil)
	r, _, err := svc.Enqueue(ctx, c.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.CostUSD = store.StatusFinished, 0.9
	svc.Store().SaveRun(r)
	if _, err := svc.Move(ctx, c.ID, "done", false); err != nil {
		t.Fatal(err)
	}
	ts := e.server(Options{})
	_, tileBody, _ := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	if !strings.Contains(tileBody, `data-lane="done"`) || !strings.Contains(tileBody, `<div class="meta"><span class="num">$0.90</span></div>`) {
		t.Fatalf("a done card's meta is its PR and cost:\n%s", tileBody)
	}
	for _, not := range []string{`class="chip`, `<span class="id">`, `budget <span`, `class="status"`} {
		if strings.Contains(tileBody, not) {
			t.Errorf("an idle done card shows no %q:\n%s", not, tileBody)
		}
	}
}

func TestBoardDoneCardWithoutPRAndCostHasNoMeta(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	ctx := context.Background()
	c, _ := svc.Add(ctx, store.NewCard{Title: "Nothing to show", Tags: []string{store.TagAgentReady}}, nil)
	if _, err := svc.Move(ctx, c.ID, "done", false); err != nil {
		t.Fatal(err)
	}
	ts := e.server(Options{})
	_, tileBody, _ := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	if !strings.Contains(tileBody, `data-lane="done"`) || strings.Contains(tileBody, `class="meta"`) {
		t.Fatalf("a done card with no PR and no cost renders no empty meta:\n%s", tileBody)
	}
}

func TestBoardWaitingCardIsHotWithActions(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Fix the lock", Tags: []string{store.TagAgentReady}}, nil)
	run, _ := e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	_, tileBody, _ := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	if strings.Contains(tileBody, `class="bar"`) || strings.Contains(tileBody, run.ID) || strings.Contains(tileBody, `class="meta"`) {
		t.Errorf("a waiting card shows its status and actions only, no budget bar or run meta:\n%s", tileBody)
	}
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range []string{`class="card hot"`, `<span class="pill p-wait st-waiting">waiting on you</span>`,
		`action="/cards/` + c.ID + `/decisions/d1a2b3c4/answer?ws=acme"`, `name="choice" value="approve">Approve plan</button>`,
		`href="/waiting?ws=acme#d-d1a2b3c4">Review it</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("waiting card lacks %q", want)
		}
	}
}

func TestBoardResultDecisionLinksTheReview(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Retry once", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindResult)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	if !strings.Contains(body, `href="/cards/`+c.ID+`/review?ws=acme">Open review</a>`) || strings.Contains(body, ">Approve plan<") {
		t.Fatal("a result decision sends the human to the review page, never approves from the card")
	}
}

func TestCardTileFragment(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Tile me"}, nil)
	ts := e.server(Options{})
	code, body, hdr := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	if code != 200 || !strings.HasPrefix(strings.TrimSpace(body), `<article class="card"`) || strings.Contains(body, "<html") ||
		!strings.Contains(body, `data-lane="backlog"`) || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", code, body)
	}
	for _, path := range []string{"/cards/c-aaaaaaaaaaaa/tile?ws=acme", "/cards/not-an-id/tile?ws=acme"} {
		if code, _, _ := get(t, ts, path, nil); code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, code)
		}
	}
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts2 := e.server(Options{Backend: b})
	if code, _, _ := get(t, ts2, "/cards/"+c.ID+"/tile?ws=zeta", nil); code != http.StatusForbidden {
		t.Fatalf("an unavailable workspace: %d", code)
	}
}

func TestBoardEscapesTitleEverywhere(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: `"><img src=x onerror=alert(1)>`}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/?ws=acme", "/cards/" + c.ID + "/tile?ws=acme"} {
		_, body, _ := get(t, ts, path, nil)
		if strings.Contains(body, "<img src=x") || !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
			t.Fatalf("%s is not escaped:\n%s", path, body)
		}
		assertCSPSafe(t, path, body)
	}
}

func TestNewBoardCardStatus(t *testing.T) {
	def := store.DefaultBudget
	done := newBoardCard(cardView("done"), store.LaneDone, []store.Run{{Status: store.StatusFinished}, {Status: store.StatusOverBudget}}, nil, "", def)
	if done.Status() != store.StatusOverBudget || done.PillLabel() != "budget raised once" || done.StatusText() != "" {
		t.Fatalf("%q %q %q", done.Status(), done.PillLabel(), done.StatusText())
	}
	review := newBoardCard(cardView("review"), store.LaneReview, []store.Run{{Status: store.StatusFinished, Rounds: 4, CostUSD: 4.6}}, nil, "", def)
	if review.Status() != store.StatusFinished || !review.OpenReview() || review.StatusText() != "4 rounds, $4.60" {
		t.Fatalf("%q %v %q", review.Status(), review.OpenReview(), review.StatusText())
	}
	failed := newBoardCard(cardView("todo"), store.LaneTodo, []store.Run{{Status: store.StatusFailed, StopReason: "relocate loop\nmore"}}, nil, "", def)
	if failed.PillLabel() != "failed" || failed.StatusText() != "relocate loop" {
		t.Fatalf("%q %q", failed.PillLabel(), failed.StatusText())
	}
	queued := newBoardCard(cardView("in progress"), store.LaneActive, []store.Run{{Status: store.StatusQueued, Budget: def}}, nil, "", def)
	if queued.StatusText() != "starts when a slot is free" || queued.Pct() != 0 {
		t.Fatalf("%q %d", queued.StatusText(), queued.Pct())
	}
	idle := newBoardCard(cardView("backlog"), store.LaneBacklog, nil, nil, "", def)
	if idle.Status() != "" || idle.CanApprove() || idle.OpenReview() {
		t.Fatal("an idle card has no status")
	}
}

func cardView(lane string) runner.CardView {
	return runner.CardView{Card: store.Card{ID: "c-aaaaaaaaaaaa", Title: "x", Lane: lane, Kind: "feature", Tags: []string{}}}
}
