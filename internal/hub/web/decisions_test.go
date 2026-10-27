package web

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestDecisionsInboxStructure(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Fix the lock", Tags: []string{store.TagAgentReady}}, nil)
	r, _ := e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	for _, want := range []string{
		`<form class="dec" id="d-d1a2b3c4" method="post" action="/cards/` + c.ID + `/decisions/d1a2b3c4/answer?ws=acme">`,
		`<span class="pill p-wait st-waiting">plan approval</span>`, `<a class="t" href="/cards/` + c.ID + `?ws=acme">Fix the lock</a>`,
		`<span class="ws-label">acme</span>`, `<a href="/runs/` + r.ID + `?ws=acme">` + r.ID + `</a>`,
		`<button class="btn warm" name="choice" value="approve">Approve</button>`, `<button class="btn danger" name="choice" value="stop">Stop</button>`,
		`name="note"`, `placeholder="Note to the agent (optional)"`, "signs it with your hub token",
		`href="/waiting?ws=acme" class="on" aria-current="page"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("inbox lacks %q", want)
		}
	}
	assertCSPSafe(t, "/waiting", body)
}

func TestOverBudgetDecisionShowsTheNumbers(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Budgets"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Status, r.CostUSD, r.Rounds, r.Seconds = store.StatusOverBudget, 10, 9, 3600
	svc.Store().SaveRun(r)
	svc.Store().AskHub(c.ID, r.ID, "", "The run stopped at its usd budget.", []string{"raise-and-resume", "stop"}, map[string]any{"limit": "usd"})
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	for _, want := range []string{`<span class="pill p-budget st-over-budget">over budget</span>`, `<b>$10.00</b> of <b>$10.00</b>`,
		"60 of 120 minutes and 9 of 12 rounds", `value="raise-and-resume">Raise and resume</button>`, `placeholder="new limits, e.g. rounds=6,usd=15`,
		"Answering records your choice in the hub"} {
		if !strings.Contains(body, want) {
			t.Errorf("over-budget decision lacks %q", want)
		}
	}
}

func TestOverBudgetDecisionWithoutRunStillRenders(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Orphan"}, nil)
	svc.Store().AskHub(c.ID, "", "", "Raise the budget?", []string{"raise-and-resume", "stop"}, nil)
	svc.Store().AskHub(c.ID, "r-aaaaaaaaaaaa", "", "Raise it again?", []string{"raise-and-resume", "stop"}, nil)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	if code != 200 || strings.Count(body, `value="raise-and-resume"`) != 2 || strings.Contains(body, "The run used") || strings.Count(body, `name="note"`) != 2 {
		t.Fatalf("%d\n%s", code, body)
	}
}

// TestHubQuestionShowsOnWaitingAndTheCard: a run that asked before its build slug existed shows a
// question with Answer and Stop, a note field that asks for the answer, on /waiting and on the card.
func TestHubQuestionShowsOnWaitingAndTheCard(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Monthly export"}, nil)
	r, _, _ := svc.Enqueue(context.Background(), c.ID, "test")
	r.Status = store.StatusWaiting
	svc.Store().SaveRun(r)
	svc.Store().AskHubQuestion(c.ID, r.ID, "Monthly or weekly?")
	ts := e.server(Options{})
	for _, path := range []string{"/waiting?ws=acme", "/cards/" + c.ID + "?ws=acme"} {
		_, body, _ := get(t, ts, path, nil)
		for _, want := range []string{`<span class="pill p-wait st-waiting">question</span>`, "Monthly or weekly?",
			`<button class="btn warm" name="choice" value="answer">Answer</button>`, `<button class="btn danger" name="choice" value="stop">Stop</button>`,
			`placeholder="Your answer (required for Answer; it is posted on the card for the run)"`, "Answering records your choice in the hub"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
	_, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if !strings.Contains(body, "waiting on you") {
		t.Error("the card shows waiting on you")
	}
}

func TestDecisionsEmptyState(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting?ws=acme", nil)
	if !strings.Contains(body, "Nothing is waiting on you") {
		t.Fatal(body)
	}
}
