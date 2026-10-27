package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

const preSlugQuestion = "FR-03 says exports are monthly but the card says weekly. Which one?"

// askedQuestion runs one pre-slug round that ends on a question and returns the run and its decision.
func askedQuestion(t *testing.T, h *harness) (store.Card, store.Run, store.Decision) {
	t.Helper()
	h.ad.steps = []step{result(preSlugQuestion), result("done")}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if len(pend) != 1 {
		t.Fatalf("pending %+v\ncomments:\n%s", pend, h.comments(c.ID))
	}
	return c, h.run(r.ID), pend[0]
}

func TestPreSlugQuestionWaitsOnTheHuman(t *testing.T) {
	h := newHarness(t)
	c, r, d := askedQuestion(t, h)
	if r.Status != store.StatusWaiting || r.Retried != 0 || r.Slug != "" {
		t.Fatalf("the run waits, no retry: %+v", r)
	}
	if n := len(r.RoundLog); n != 1 || r.RoundLog[0].Outcome != string(VerdictQuestion) {
		t.Fatalf("round log %+v", r.RoundLog)
	}
	if d.Kind != store.KindQuestion || !d.HubOwned() || d.RunID != r.ID || d.Text != preSlugQuestion || strings.Join(d.Options, ",") != "answer,stop" {
		t.Fatalf("decision %+v", d)
	}
	if card, _ := h.st.Card(c.ID); card.LockedByRun != r.ID {
		t.Fatal("a waiting run keeps the card")
	}
	if cm := h.comments(c.ID); strings.Contains(cm, "without a stop") || strings.Contains(cm, "retrying") {
		t.Fatal(cm)
	}
	if n := h.notifications(); len(n) == 0 || !strings.Contains(n[len(n)-1], "waiting on you") {
		t.Fatalf("notified: %v", n)
	}
	h.clock.Advance(10 * time.Minute)
	h.tick()
	if len(h.ad.calls()) != 1 || h.run(r.ID).Status != store.StatusWaiting {
		t.Fatal("nothing starts while the question is pending")
	}
	if _, err := h.svc.Answer(WithAgent(context.Background()), c.ID, d.ID, "answer", "monthly"); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent session cannot answer: %v", err)
	}
}

func TestAnsweringAPreSlugQuestionCommentsAndRequeues(t *testing.T) {
	h := newHarness(t)
	c, r, d := askedQuestion(t, h)
	if _, err := h.svc.Answer(context.Background(), c.ID, d.ID, "answer", "   "); err == nil {
		t.Fatal("answer needs a note")
	}
	if cur, _ := h.st.Decision(d.ID); cur.Status != store.DecisionPending {
		t.Fatal("a refused answer keeps the decision pending")
	}
	out, err := h.svc.Answer(context.Background(), c.ID, d.ID, "answer", "Monthly, as FR-03 says.")
	if err != nil {
		t.Fatal(err)
	}
	if out.Resumed == nil || out.Resumed.ID != r.ID || out.Resumed.Status != store.StatusQueued {
		t.Fatalf("the same run is requeued: %+v", out)
	}
	cs, _ := h.st.Comments(c.ID)
	last := cs[len(cs)-1]
	if last.AuthorKind != store.AuthorHuman || last.Author != "tester" || last.Body != "Monthly, as FR-03 says." || last.RunID != r.ID {
		t.Fatalf("the note is a human comment: %+v", last)
	}
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 2 || calls[1].SessionID != "s1" {
		t.Fatalf("the session continues: %+v", calls)
	}
	if !strings.HasPrefix(calls[1].Prompt, "/tentaqles:feature Export invoices") ||
		!strings.Contains(calls[1].Prompt, "## Notes from the human on this card") ||
		!strings.Contains(calls[1].Prompt, "Monthly, as FR-03 says.") {
		t.Fatalf("the answer reaches the next prompt: %q", calls[1].Prompt)
	}
	if strings.Contains(calls[1].Prompt, preSlugQuestion) {
		t.Fatal("the agent's own comment is not a human note")
	}
}

func TestStopOnAPreSlugQuestionEndsTheRun(t *testing.T) {
	h := newHarness(t)
	c, r, d := askedQuestion(t, h)
	if _, err := h.svc.Answer(context.Background(), c.ID, d.ID, "stop", ""); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusCancelled || got.StopReason != "stopped by human" || card.LockedByRun != "" {
		t.Fatalf("%+v lock %q", got, card.LockedByRun)
	}
}

// TestQuestionThatCannotBeRecordedFailsTheRun: a run never waits with no decision to answer.
func TestQuestionThatCannotBeRecordedFailsTheRun(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		if _, err := h.st.DB.Exec(`ALTER TABLE decisions RENAME TO decisions_gone`); err != nil {
			t.Error(err)
		}
		return result(preSlugQuestion)(ctx, req)
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusFailed || got.StopReason != "question not recorded" || card.LockedByRun != "" {
		t.Fatalf("%+v lock %q", got, card.LockedByRun)
	}
	if cm := h.comments(c.ID); !strings.Contains(cm, "could not record the run's question") || !strings.Contains(cm, "decisions") {
		t.Fatal(cm)
	}
}

// TestNoStopAfterASlugStillFails: once a build slug exists, a round with no stop fails as before.
func TestNoStopAfterASlugStillFails(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("I did some things.")}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.gt.set("demo", loadState(t, "in_progress"))
	h.tick()
	got := h.run(r.ID)
	if got.Status != store.StatusFailed || got.StopReason != "ended without a stop" {
		t.Fatalf("%+v", got)
	}
	if pend, _ := h.st.PendingDecisions(); len(pend) != 0 {
		t.Fatalf("no question: %+v", pend)
	}
	if !strings.Contains(h.comments(c.ID), "The run ended without a stop.") {
		t.Fatal(h.comments(c.ID))
	}
}

// TestNonZeroExitBeforeASlugStillRetries: a failed round is not a question.
func TestNonZeroExitBeforeASlugStillRetries(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", Transcript: "Which one?", CostKnown: true}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.Retried != 1 {
		t.Fatalf("%+v", got)
	}
	if pend, _ := h.st.PendingDecisions(); len(pend) != 0 {
		t.Fatalf("no question: %+v", pend)
	}
}
