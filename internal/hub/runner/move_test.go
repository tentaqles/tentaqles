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

func TestMoveIntoActiveEnqueuesAgentReadyOnly(t *testing.T) {
	h := newHarness(t)
	ready := h.card("feature", store.TagAgentReady)
	plain := h.card("feature")
	out, err := h.svc.Move(context.Background(), ready.ID, "in progress", false)
	if err != nil || out.Enqueued == nil || out.Card.Lane != "in progress" {
		t.Fatalf("%+v %v", out, err)
	}
	out, _ = h.svc.Move(context.Background(), plain.ID, "in progress", false)
	if out.Enqueued != nil {
		t.Fatal("a card without agent-ready is not run")
	}
}

func TestMoveOutOfActiveQueuedCancels(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	in, _ := h.svc.Move(context.Background(), c.ID, "in progress", false)
	out, err := h.svc.Move(context.Background(), c.ID, "backlog", false)
	if err != nil || out.Cancelled == nil || out.Cancelled.ID != in.Enqueued.ID || out.Card.Lane != "backlog" {
		t.Fatalf("%+v %v", out, err)
	}
	if got := h.run(in.Enqueued.ID); got.Status != store.StatusCancelled {
		t.Fatalf("%+v", got)
	}
	card, _ := h.st.Card(c.ID)
	if card.LockedByRun != "" {
		t.Fatal("lock released")
	}
}

func TestMoveOutOfActiveRunningNeedsCancelRun(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		close(started)
		<-ctx.Done()
		return adapter.RunResult{ExitCode: -1}, ctx.Err()
	}}
	c := h.card("feature", store.TagAgentReady)
	in, _ := h.svc.Move(context.Background(), c.ID, "in progress", false)
	h.svc.Tick(context.Background())
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("round did not start")
	}
	_, err := h.svc.Move(context.Background(), c.ID, "backlog", false)
	var active *RunActiveError
	if !errors.As(err, &active) || active.Run.ID != in.Enqueued.ID {
		t.Fatalf("want RunActiveError, got %v", err)
	}
	if card, _ := h.st.Card(c.ID); card.Lane != "in progress" {
		t.Fatalf("the card must stay put: %s", card.Lane)
	}
	out, err := h.svc.Move(context.Background(), c.ID, "backlog", true)
	if err != nil || out.Cancelled == nil || out.Cancelled.Status != store.StatusCancelled || out.Card.Lane != "backlog" {
		t.Fatalf("%+v %v", out, err)
	}
	if card, _ := h.st.Card(c.ID); card.LockedByRun != "" {
		t.Fatal("lock released")
	}
}

func TestWaitingCardMayMoveToReviewAndAgentNeverToDone(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	if _, err := h.svc.Move(context.Background(), c.ID, "review", false); err != nil {
		t.Fatalf("waiting → review is the normal flow: %v", err)
	}
	if h.run(r.ID).Status != store.StatusWaiting {
		t.Fatal("the run keeps waiting")
	}
	if _, err := h.svc.Move(WithAgent(context.Background()), c.ID, "done", true); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent → done: %v", err)
	}
}

func TestAgentMovesOnlyBetweenPlanningLanes(t *testing.T) {
	h := newHarness(t)
	agent := WithAgent(context.Background())
	c := h.card("feature", store.TagAgentReady)
	if out, err := h.svc.Move(agent, c.ID, "todo", false); err != nil || out.Card.Lane != "todo" {
		t.Fatalf("agent backlog → todo: %+v %v", out, err)
	}
	if _, err := h.svc.Move(agent, c.ID, "in progress", false); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent todo → in progress: %v", err)
	}
	if runs, _ := h.st.RunsForCard(c.ID); len(runs) != 0 {
		t.Fatalf("a refused agent move created a run: %+v", runs)
	}
	in, err := h.svc.Move(context.Background(), c.ID, "in progress", false)
	if err != nil || in.Enqueued == nil {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err := h.svc.Move(agent, c.ID, "todo", true); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent in progress → todo: %v", err)
	}
	card, _ := h.st.Card(c.ID)
	if got := h.run(in.Enqueued.ID); got.Status != store.StatusQueued || card.Lane != "in progress" || card.LockedByRun != got.ID {
		t.Fatalf("a refused agent move touched the run: %+v lane %q", got, card.Lane)
	}
	plain := h.card("chore")
	if _, err := h.svc.Move(agent, plain.ID, "done", false); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent → done: %v", err)
	}
}

func TestMoveQueuedRunToReviewCancels(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	in, _ := h.svc.Move(context.Background(), c.ID, "in progress", false)
	out, err := h.svc.Move(context.Background(), c.ID, "review", false)
	if err != nil || out.Cancelled == nil || out.Card.Lane != "review" {
		t.Fatalf("%+v %v", out, err)
	}
	card, _ := h.st.Card(c.ID)
	if got := h.run(in.Enqueued.ID); got.Status != store.StatusCancelled || card.LockedByRun != "" || !strings.Contains(h.comments(c.ID), "system: The run was cancelled") {
		t.Fatalf("run %+v lock %q comments %s", got, card.LockedByRun, h.comments(c.ID))
	}
}
