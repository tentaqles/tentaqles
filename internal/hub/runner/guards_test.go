package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// pendingHub is the card's one pending hub decision, or a fatal error.
func (h *harness) pendingHub(cardID string) store.Decision {
	h.t.Helper()
	pend, _ := h.st.PendingDecisions()
	var out []store.Decision
	for _, d := range pend {
		if d.CardID == cardID {
			out = append(out, d)
		}
	}
	if len(out) != 1 || !out[0].HubOwned() {
		h.t.Fatalf("want one pending hub decision, got %+v", out)
	}
	return out[0]
}

// wantHeld: the run waits on the human at zero cost with the given stop reason and no round counted.
func wantHeld(t *testing.T, h *harness, runID, reason string) store.Run {
	t.Helper()
	got := h.run(runID)
	if got.Status != store.StatusWaiting || got.StopReason != reason {
		t.Fatalf("status %s reason %q, want waiting %q", got.Status, got.StopReason, reason)
	}
	return got
}

func TestUntrustedWorkspaceStartsNoRound(t *testing.T) {
	h := newHarness(t)
	var trusted atomic.Bool
	h.svc.cfg.Trusted = func() error {
		if trusted.Load() {
			return nil
		}
		return errors.New("workspace acme is untrusted; run: tq allow acme")
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	if n := len(h.ad.calls()); n != 0 {
		t.Fatalf("an untrusted workspace started %d rounds", n)
	}
	got := wantHeld(t, h, r.ID, "untrusted")
	if got.Rounds != 0 || got.CostUSD != 0 {
		t.Fatalf("%+v", got)
	}
	if card, _ := h.st.Card(c.ID); card.LockedByRun != r.ID {
		t.Fatal("a held run keeps the card")
	}
	d := h.pendingHub(c.ID)
	if !strings.Contains(d.Text, "no longer trusted") || !strings.Contains(d.Text, "tq allow acme") || strings.Join(d.Options, ",") != "resume,stop" {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(h.comments(c.ID), "no longer trusted") {
		t.Fatal(h.comments(c.ID))
	}
	// A tick while held starts nothing and asks nothing twice.
	h.tick()
	h.pendingHub(c.ID)
	if len(h.ad.calls()) != 0 {
		t.Fatal("a held run started")
	}

	trusted.Store(true)
	out, err := h.svc.Answer(context.Background(), c.ID, d.ID, "resume", "")
	if err != nil || out.Resumed == nil || out.Resumed.ID != r.ID {
		t.Fatalf("%+v %v", out, err)
	}
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.StopReason != "" {
		t.Fatalf("%+v", got)
	}
	h.tick()
	if len(h.ad.calls()) != 1 {
		t.Fatal("resume after re-trust runs the round")
	}
}

func TestUntrustedMidRunHoldsTheNextRound(t *testing.T) {
	h := newHarness(t)
	var trusted atomic.Bool
	trusted.Store(true)
	h.svc.cfg.Trusted = func() error {
		if trusted.Load() {
			return nil
		}
		return errors.New("untrusted")
	}
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		trusted.Store(false) // a skill edits the manifest during the round
		return adapter.RunResult{StopReason: "error_max_turns", SessionID: "s-1", CostUSD: 1.5, CostKnown: true}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick() // round 1 runs, the run requeues
	h.tick() // round 2 is refused
	if n := len(h.ad.calls()); n != 1 {
		t.Fatalf("calls %d", n)
	}
	got := wantHeld(t, h, r.ID, "untrusted")
	if got.Rounds != 1 || got.CostUSD != 1.5 {
		t.Fatalf("%+v", got)
	}
	h.pendingHub(c.ID)
}

func TestStopOnAHeldRunCancelsIt(t *testing.T) {
	h := newHarness(t)
	h.svc.cfg.Trusted = func() error { return errors.New("untrusted") }
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	d := h.pendingHub(c.ID)
	if _, err := h.svc.Answer(context.Background(), c.ID, d.ID, "stop", ""); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusCancelled || card.LockedByRun != "" {
		t.Fatalf("%+v lock %q", got, card.LockedByRun)
	}
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
}

func (h *harness) cardIn(projectDir string) store.Card {
	h.t.Helper()
	c, err := h.st.AddCard(store.NewCard{Title: "Export invoices", Body: "CSV per month.", Kind: "feature", Tags: []string{store.TagAgentReady}, ProjectDir: projectDir}, "tester")
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func gitHarness(t *testing.T) *harness {
	needGit(t)
	h := newHarness(t)
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(h.root)) // the temp folder is never inside a repo
	h.svc.cfg.RepoCheck = GitPreflight
	return h
}

func TestNoProjectFolderAndRootNotARepoStartsNoRound(t *testing.T) {
	h := gitHarness(t)
	c := h.cardIn("")
	r := h.enqueue(c)
	h.tick()
	if len(h.ad.calls()) != 0 {
		t.Fatal("a round started outside a git repository")
	}
	got := wantHeld(t, h, r.ID, "not a git repository")
	if got.CostUSD != 0 || got.Rounds != 0 {
		t.Fatalf("%+v", got)
	}
	d := h.pendingHub(c.ID)
	if !strings.Contains(d.Text, "no project folder") || !strings.Contains(d.Text, "not a git repository") {
		t.Fatal(d.Text)
	}
	if !strings.Contains(h.comments(c.ID), "not a git repository") {
		t.Fatal(h.comments(c.ID))
	}
}

func TestProjectFolderNotInARepoStartsNoRound(t *testing.T) {
	h := gitHarness(t)
	c := h.cardIn("apps/billing")
	r := h.enqueue(c)
	h.tick()
	if len(h.ad.calls()) != 0 {
		t.Fatal("a round started outside a git repository")
	}
	wantHeld(t, h, r.ID, "not a git repository")
	if d := h.pendingHub(c.ID); !strings.Contains(d.Text, "apps") || !strings.Contains(d.Text, "not inside a git repository") {
		t.Fatal(d.Text)
	}
}

func TestMissingProjectFolderStartsNoRound(t *testing.T) {
	h := newHarness(t)
	c := h.cardIn("apps/missing")
	r := h.enqueue(c)
	h.tick()
	if len(h.ad.calls()) != 0 {
		t.Fatal("a round started in a missing folder")
	}
	wantHeld(t, h, r.ID, "project folder missing")
	if d := h.pendingHub(c.ID); !strings.Contains(d.Text, "does not exist") {
		t.Fatal(d.Text)
	}
	// The human creates the folder and resumes: the round runs.
	if err := os.MkdirAll(filepath.Join(h.root, "apps", "missing"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := h.pendingHub(c.ID)
	if _, err := h.svc.Answer(context.Background(), c.ID, d.ID, "resume", ""); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if len(h.ad.calls()) != 1 {
		t.Fatal("resume runs the round")
	}
}

func TestInsideARepoRunsTheRound(t *testing.T) {
	h := gitHarness(t)
	gitInit(t, h.root)
	for _, pd := range []string{"", "apps/billing"} {
		c := h.cardIn(pd)
		h.enqueue(c)
		h.tick()
	}
	if n := len(h.ad.calls()); n != 2 {
		t.Fatalf("calls %d", n)
	}
}

func TestGitPreflightNeedsGitOnPath(t *testing.T) {
	dir := testutil.TempDir(t)
	t.Setenv("PATH", dir)
	err := GitPreflight(context.Background(), dir, "")
	if err == nil || !strings.Contains(err.Error(), "git is not on PATH") {
		t.Fatalf("%v", err)
	}
}

const outOfCredits = "You're out of usage credits. Switch to another model, or manage usage credits at https://claude.ai/settings/usage"

func TestUsageLimitIsAResumableStop(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{ExitCode: 1, StopReason: "success", Transcript: outOfCredits, SessionID: "s-9", CostUSD: 4.81, CostKnown: true}, nil
		},
		result("done"),
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	got := wantHeld(t, h, r.ID, "usage limit")
	if got.CostUSD != 4.81 || got.Rounds != 1 || got.Retried != 0 || got.SessionID != "s-9" {
		t.Fatalf("%+v", got)
	}
	if n := len(got.RoundLog); n != 1 || got.RoundLog[0].Outcome != string(VerdictUsageLimit) {
		t.Fatalf("%+v", got.RoundLog)
	}
	for _, n := range h.notifications() {
		if strings.Contains(n, "failed") {
			t.Fatal("a usage limit is not a failure: " + n)
		}
	}
	d := h.pendingHub(c.ID)
	if !strings.Contains(d.Text, "usage limit") || strings.Join(d.Options, ",") != "resume,stop" {
		t.Fatalf("%+v", d)
	}
	h.tick()
	if len(h.ad.calls()) != 1 {
		t.Fatal("a held run started")
	}
	if out, err := h.svc.Answer(context.Background(), c.ID, d.ID, "resume", ""); err != nil || out.Resumed == nil {
		t.Fatalf("%+v %v", out, err)
	}
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 2 || calls[1].SessionID != "s-9" {
		t.Fatalf("resume continues the session: %+v", calls)
	}
}

func TestClassifyUsageLimit(t *testing.T) {
	inProgress := loadState(t, "in_progress")
	hits := []string{
		outOfCredits,
		"You’re out of usage credits. Switch to another model.",
		"Claude AI usage limit reached|1761500000",
		"Usage limit reached. Your limit resets at 5pm.",
		"5-hour limit reached ∙ resets 3pm",
		`API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"..."}}`,
		"Rate limit reached. Please try again later.",
		"Credit balance is too low",
		"Some partial work.\n\nYou're out of usage credits. Switch to another model.",
	}
	for _, text := range hits {
		for _, exit := range []int{0, 1} {
			got := Classify(Observation{ExitCode: exit, FinalText: text, State: inProgress, PrevStateKey: StateKey(inProgress)})
			if got.Verdict != VerdictUsageLimit || got.Status != store.StatusWaiting || got.StopReason != "usage limit" {
				t.Errorf("%q exit %d: %+v", text, exit, got)
			}
		}
	}
	misses := []string{
		"Added a rate limit to the login endpoint; tests pass.",
		"The API now returns 429 when the rate limit is exceeded.",
		"Done. I documented the usage limit for the export.",
		strings.Repeat("Long summary. ", 80) + "\nRate limit reached.",
	}
	for _, text := range misses {
		if got := Classify(Observation{FinalText: text, State: inProgress, PrevStateKey: StateKey(inProgress)}); got.Verdict == VerdictUsageLimit {
			t.Errorf("%q: %+v", text, got)
		}
	}
	// A gate stop read after the round still wins.
	if got := Classify(Observation{FinalText: outOfCredits, State: loadState(t, "waiting")}); got.Verdict != VerdictWaiting {
		t.Errorf("%+v", got)
	}
}
