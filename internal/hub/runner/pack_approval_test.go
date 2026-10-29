package runner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
)

// A pack approval (permission with data.pack, spec C §2.11) never resumes,
// requeues or holds a run.
func TestPackApprovalNeverDrivesTheRun(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), ".tentaqles"))
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	before, err := h.svc.pendingRunDecisions(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.st.AskHub(c.ID, r.ID, "", "ml approval: promote churn@7 -> production", []string{"approve", "reject"},
		map[string]any{"pack": "ml", "action": "promote", "subject": "churn@7", "target": "production"})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := h.svc.pendingRunDecisions(c.ID); n != before {
		t.Fatalf("a pack approval counts as a run decision: %d vs %d", n, before)
	}
	if h.svc.hubPending(h.run(r.ID)) {
		t.Fatal("a pack approval holds the run")
	}
	// R2-5: without the terminal path's human-presence marker, the service refuses.
	if _, err := h.svc.Answer(context.Background(), c.ID, d.ID, "approve", ""); !errors.Is(err, store.ErrPackAnswer) {
		t.Fatalf("answered without presence: %v", err)
	}
	sign := func(shown ledger.Shown, id, choice, kind, ts string) (ledger.Entry, error) {
		return ledger.Entry{Event: ledger.Answered, Decision: id, Pack: shown.Pack, Choice: choice, ActorKind: kind, TS: ts}, nil
	}
	// R4-5: a store row that changed after it was shown is not signed.
	stale := d.Shown()
	stale.Target = "staging"
	if _, err := h.svc.Answer(WithHumanPresence(context.Background(), HumanPresence{Sign: sign, Shown: &stale}), c.ID, d.ID, "approve", ""); !errors.Is(err, ledger.ErrShownMismatch) {
		t.Fatalf("a changed row was signed: %v", err)
	}
	if got, _ := h.st.Decision(d.ID); got.Status != store.DecisionPending {
		t.Fatalf("answered over a changed row: %+v", got)
	}
	if _, err := h.svc.Answer(WithHumanPresence(context.Background(), HumanPresence{Sign: sign}), c.ID, d.ID, "approve", ""); !errors.Is(err, store.ErrPackAnswer) {
		t.Fatalf("answered with no shown binding: %v", err)
	}
	shown := d.Shown()
	out, err := h.svc.Answer(WithHumanPresence(context.Background(), HumanPresence{Sign: sign, Shown: &shown}), c.ID, d.ID, "approve", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Resumed != nil || out.Decision.Status != store.DecisionAnswered {
		t.Fatalf("%+v", out)
	}
	if got := h.run(r.ID); got.Status != store.StatusWaiting {
		t.Fatalf("the run moved: %+v", got)
	}
}

// M3: a gate record carrying a pack approval's id never answers it.
func TestMirrorSkipsHubOwnedDecisions(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), ".tentaqles"))
	h := newHarness(t)
	c, r := waitingRun(t, h, "")
	d, err := h.st.AskHub(c.ID, "", "", "ml approval", []string{"approve", "reject"}, map[string]any{"pack": "ml"})
	if err != nil {
		t.Fatal(err)
	}
	h.svc.mirror(c, r, &gate.State{Slug: "demo", Decisions: []gate.Decision{{ID: d.ID, Kind: store.KindPermission, Status: store.DecisionAnswered, Choice: "approve", Actor: "tester"}}})
	if got, _ := h.st.Decision(d.ID); got.Status != store.DecisionPending {
		t.Fatalf("the mirror answered a hub-owned decision: %+v", got)
	}
}
