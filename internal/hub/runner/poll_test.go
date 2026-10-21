package runner

import (
	"context"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestTerminalAnswerIsNoticedByThePoll(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve")
	h.clock.Advance(2 * time.Second)
	h.tick() // polls, mirrors, resumes, starts the round
	d, _ := h.st.Decision("d1a2b3c4")
	if d.Status != store.DecisionAnswered || d.Actor != "terminal" {
		t.Fatalf("%+v", d)
	}
	calls := h.ad.calls()
	if calls[len(calls)-1].Prompt != "/tentaqles:build resume demo --headless" {
		t.Fatalf("no resumed round: %v", calls[len(calls)-1].Prompt)
	}
	_ = c
	_ = r
}

func TestTerminalFinishIsNoticedByThePoll(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, store.KindResult)
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve") // the fake marks the state finished
	h.clock.Advance(2 * time.Second)
	if err := h.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	card, _ := h.st.Card(c.ID)
	if got := h.run(r.ID); got.Status != store.StatusFinished || card.Lane != "done" {
		t.Fatalf("run %+v lane %s", got, card.Lane)
	}
}

func TestPollSkipsRunsWaitingOnHubDecisions(t *testing.T) {
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	h.st.AskHub(c.ID, r.ID, "demo", "permission?", []string{"allow-once", "stop"}, map[string]any{"tool": "Bash"})
	h.gt.terminalAnswer("demo", "d1a2b3c4", "approve")
	h.clock.Advance(2 * time.Second)
	h.tick()
	if h.run(r.ID).Status != store.StatusWaiting {
		t.Fatal("a pending hub decision keeps the run waiting")
	}
}

func TestPollLeavesRolledBackOrHaltedRunsWaiting(t *testing.T) {
	for _, name := range []string{"rolled back", "halted"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			_, r := waitingRun(t, h, "")
			h.gt.terminalAnswer("demo", "d1a2b3c4", "approve") // answered, then the state went bad
			h.gt.mu.Lock()
			if name == "rolled back" {
				h.gt.states["demo"].RolledBack = true
			} else {
				h.gt.states["demo"].Halted = &gate.Halted{Reason: "review failed three times"}
			}
			h.gt.mu.Unlock()
			before := len(h.ad.calls())
			h.clock.Advance(2 * time.Second)
			h.tick()
			if got := h.run(r.ID); got.Status != store.StatusWaiting || len(h.ad.calls()) != before {
				t.Fatalf("the human must act first: %+v, %d new rounds", got, len(h.ad.calls())-before)
			}
		})
	}
}
