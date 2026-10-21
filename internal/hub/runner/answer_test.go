package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// waitingRun leaves a card with a run waiting on the decision in states/<fixture>.json (slug demo).
func waitingRun(t *testing.T, h *harness, kind string) (store.Card, store.Run) {
	t.Helper()
	if h.ad.steps == nil {
		h.ad.steps = []step{result("Waiting for you.")} // session s1
	}
	c := h.card("feature", store.TagAgentReady)
	st := loadState(t, "waiting")
	if kind != "" {
		st.PendingDecisions[0].Kind = kind
		if kind == store.KindResult {
			st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
		}
	}
	h.gt.set("demo", st)
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.tick()
	if got := h.run(r.ID); got.Status != store.StatusWaiting {
		t.Fatalf("setup: %+v", got)
	}
	return c, h.run(r.ID)
}

func TestAnswerResumesTheRun(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	out, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", "looks right")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.gt.answers) != 1 || h.gt.answers[0] != "demo d1a2b3c4 approve hub:tester" {
		t.Fatalf("gate answers %v", h.gt.answers)
	}
	if out.Decision.Status != store.DecisionAnswered || out.Decision.Actor != "hub:tester" || out.Resumed == nil || out.AlreadyAnswered {
		t.Fatalf("%+v", out)
	}
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.Phase != store.PhaseResume {
		t.Fatalf("%+v", got)
	}
	h.tick()
	calls := h.ad.calls()
	last := calls[len(calls)-1]
	if last.Prompt != "/tentaqles:build resume demo --headless" || last.SessionID != "s1" {
		t.Fatalf("resume round %q %q", last.Prompt, last.SessionID)
	}
}

func TestAnswerRaceWithTerminal(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve") // the human answered in the terminal first
	out, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", "")
	if err != nil {
		t.Fatalf("the hub must not error when the terminal answered first: %v", err)
	}
	if !out.AlreadyAnswered || !strings.Contains(out.Message, "already answered in the terminal") || out.Decision.Actor != "terminal" || out.Resumed == nil {
		t.Fatalf("%+v", out)
	}
	again, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "stop", "")
	if err != nil || !again.AlreadyAnswered || again.Resumed != nil {
		t.Fatalf("second click: %+v %v", again, err)
	}
	runs, _ := h.st.RunsForCard(c.ID)
	queued := 0
	for _, x := range runs {
		if x.Status == store.StatusQueued {
			queued++
		}
	}
	if len(runs) != 1 || queued != 1 || h.run(r.ID).Status != store.StatusQueued {
		t.Fatalf("exactly one resume: %d runs, %d queued", len(runs), queued)
	}
	before := len(h.ad.calls())
	h.tick()
	if len(h.ad.calls()) != before+1 {
		t.Fatal("one resumed round")
	}
}

func TestAnswerRefusals(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	if _, err := h.svc.Answer(WithAgent(context.Background()), c.ID, "d1a2b3c4", "approve", ""); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent: %v", err)
	}
	if _, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "yes please", ""); err == nil || !strings.Contains(err.Error(), "approve, edit, stop") {
		t.Fatalf("bad choice: %v", err)
	}
	rb := loadState(t, "rolled_back")
	r.State = rb.Raw
	h.st.SaveRun(r)
	if _, err := h.svc.Answer(context.Background(), c.ID, "d1a2b3c4", "approve", ""); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("rolled back: %v", err)
	}
	if len(h.gt.answers) != 0 {
		t.Fatalf("the gate was called: %v", h.gt.answers)
	}
}

func TestApproveFinishesMovesDoneAndCommentsTheParent(t *testing.T) {
	h := newHarness(t)
	parent := h.card("project")
	h.ad.steps = nil
	child, _ := h.st.AddCard(store.NewCard{Title: "Export invoices", ParentID: parent.ID, ProjectDir: "apps/billing", Tags: []string{store.TagAgentReady}}, "tester")
	st := loadState(t, "waiting")
	st.PendingDecisions[0].Kind = store.KindResult
	st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
	h.gt.set("demo", st)
	r := h.enqueue(child)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.tick()
	h.st.AddComment(child.ID, store.AuthorAgent, "agent", "PR: https://github.com/acme/billing/pull/7", r.ID)
	var approved []string
	h.svc.OnApprove = func(c store.Card, run store.Run) { approved = append(approved, c.ID+" "+run.ID) }
	if _, err := h.svc.Approve(context.Background(), child.ID); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(child.ID)
	if got.Status != store.StatusFinished || card.Lane != "done" || card.LockedByRun != "" || len(approved) != 1 {
		t.Fatalf("run %+v card %+v approved %v", got, card, approved)
	}
	if !strings.Contains(h.comments(parent.ID), "Export invoices: approved, PR https://github.com/acme/billing/pull/7") {
		t.Fatal(h.comments(parent.ID))
	}
	acts, _ := h.st.Activity(100)
	found := false
	for _, a := range acts {
		found = found || a.Verb == activity.VerbReviewApprove
	}
	if !found {
		t.Fatal("review.approve not logged")
	}
}

func TestRequestChangesCommentsAnswersAndResumes(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, store.KindResult)
	if _, err := h.svc.RequestChanges(context.Background(), c.ID, ""); err == nil {
		t.Fatal("request changes needs a note")
	}
	out, err := h.svc.RequestChanges(context.Background(), c.ID, "Use ISO dates in the file name.")
	if err != nil || out.Resumed == nil {
		t.Fatalf("%+v %v", out, err)
	}
	card, _ := h.st.Card(c.ID)
	if card.Lane != "in progress" || !strings.Contains(h.comments(c.ID), "human: Use ISO dates") || h.gt.answers[0] != "demo d1a2b3c4 request-changes hub:tester" {
		t.Fatalf("lane %q answers %v comments %s", card.Lane, h.gt.answers, h.comments(c.ID))
	}
	if h.run(r.ID).Status != store.StatusQueued {
		t.Fatal("the run resumes into the engine's request-changes path")
	}
}

func TestMarkDone(t *testing.T) {
	h := newHarness(t)
	c, _ := waitingRun(t, h, store.KindResult)
	if _, err := h.svc.MarkDone(context.Background(), c.ID); err == nil {
		t.Fatal("a pending result must be approved, not marked done")
	}
	plain := h.card("chore")
	got, err := h.svc.MarkDone(context.Background(), plain.ID)
	if err != nil || got.Lane != "done" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestAllowOnceAppliesToOneRound(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", CostKnown: true,
				PermissionDenials: []adapter.PermissionDenial{{Tool: "Bash", Input: map[string]any{"command": "npm test"}}}}, nil
		},
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{StopReason: "error_max_turns", CostKnown: true}, nil
		},
	}
	c := h.card("feature", store.TagAgentReady)
	h.enqueue(c)
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if _, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "allow-once", ""); err != nil {
		t.Fatal(err)
	}
	h.tick()
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 3 || strings.Join(calls[1].AllowedTools, ",") != "Bash(npm test)" || len(calls[2].AllowedTools) != 0 {
		t.Fatalf("allowed tools per round: %v / %v", calls[1].AllowedTools, calls[2].AllowedTools)
	}
}

func TestRaiseAndResumeStartsANewRun(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{maxTurns(h, adapter.Usage{}, 0.01, true, 0)}
	b := store.Budget{Tokens: 1e9, USD: 100, Minutes: 1000, Rounds: 1}
	c, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	first, _ := h.svc.Run(context.Background(), c.ID)
	first.Slug = "demo"
	h.st.SaveRun(first)
	h.tick()
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if len(pend) != 1 {
		t.Fatalf("over-budget decision %+v", pend)
	}
	out, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "raise-and-resume", "rounds=5")
	if err != nil || out.Resumed == nil || out.Resumed.ID == first.ID {
		t.Fatalf("%+v %v", out, err)
	}
	nr := h.run(out.Resumed.ID)
	card, _ := h.st.Card(c.ID)
	if nr.Budget.Rounds != 5 || nr.OnRamp != store.OnRampResume || nr.Slug != "demo" || card.LockedByRun != nr.ID {
		t.Fatalf("%+v lock %q", nr, card.LockedByRun)
	}
}

func TestFinishApprovedOnlyOnce(t *testing.T) {
	h := newHarness(t)
	parent := h.card("project")
	child, _ := h.st.AddCard(store.NewCard{Title: "Export invoices", ParentID: parent.ID, ProjectDir: "apps/billing", Tags: []string{store.TagAgentReady}}, "tester")
	r := h.enqueue(child)
	approved := 0
	h.svc.OnApprove = func(store.Card, store.Run) { approved++ }
	h.svc.finishApproved(child, r) // the hub answer
	h.svc.finishApproved(child, r) // the terminal poll seeing the same approval
	if approved != 1 || strings.Count(h.comments(parent.ID), "approved") != 1 || h.run(r.ID).Status != store.StatusFinished {
		t.Fatalf("approved %d, parent comments %s", approved, h.comments(parent.ID))
	}
}

func TestRaiseAndResumeRefusesABadNoteAndKeepsTheDecision(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{maxTurns(h, adapter.Usage{}, 0.01, true, 0)}
	b := store.Budget{Tokens: 1e9, USD: 100, Minutes: 1000, Rounds: 1}
	c, _ := h.svc.Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	first, _ := h.svc.Run(context.Background(), c.ID)
	h.tick()
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if len(pend) != 1 {
		t.Fatalf("over-budget decision %+v", pend)
	}
	if _, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "raise-and-resume", "rounds=lots"); err == nil || !strings.Contains(err.Error(), "rounds") {
		t.Fatalf("bad note: %v", err)
	}
	d, _ := h.st.Decision(pend[0].ID)
	runs, _ := h.st.RunsForCard(c.ID)
	if d.Status != store.DecisionPending || len(runs) != 1 || runs[0].ID != first.ID {
		t.Fatalf("decision %+v runs %d", d, len(runs))
	}
	if out, err := h.svc.Answer(context.Background(), c.ID, pend[0].ID, "raise-and-resume", "rounds=3"); err != nil || out.Resumed == nil {
		t.Fatalf("a good note after a bad one: %+v %v", out, err)
	}
}
