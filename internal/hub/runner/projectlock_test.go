package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// projectCard is a card of another project folder (created under the harness root).
func (h *harness) projectCard(dir string) store.Card {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.root, filepath.FromSlash(dir)), 0o755); err != nil {
		h.t.Fatal(err)
	}
	c, err := h.st.AddCard(store.NewCard{Title: "Export invoices", Body: "CSV per month.", Kind: "feature",
		Tags: []string{store.TagAgentReady}, ProjectDir: dir}, "tester")
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func projectLockRows(t *testing.T, st *store.Store) map[string]string {
	t.Helper()
	rows, err := st.DB.Query(`SELECT key, value FROM settings WHERE key LIKE ?`, settingProjectLock+"%")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

// byDir answers each round by its folder: the project folder relocates, a worktree round ends.
func byDir(project, wt string) step {
	return func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		if req.Dir == project {
			return result("Worktree ready.\nTQ_HUB_RELOCATE "+wt+" demo")(ctx, req)
		}
		return result("Done in the worktree.")(ctx, req)
	}
}

// Two cards of one project: the second run waits (queued) until the first one relocated.
func TestProjectLockWaitsUntilRelocation(t *testing.T) {
	h := newHarness(t)
	project := filepath.Join(h.root, "apps", "billing")
	wt := filepath.Join(h.root, "billing-build-demo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	h.ad.steps = []step{byDir(project, wt)}
	a, b := h.card("feature", store.TagAgentReady), h.card("feature", store.TagAgentReady)
	ra, rb := h.enqueue(a), h.enqueue(b)

	h.tick()
	if n := len(h.ad.calls()); n != 1 {
		t.Fatalf("%d rounds started; the second card of the project must wait", n)
	}
	gotA, gotB := h.run(ra.ID), h.run(rb.ID)
	if gotA.Worktree != wt || gotA.Status != store.StatusQueued {
		t.Fatalf("first run did not relocate: %+v", gotA)
	}
	if gotB.Status != store.StatusQueued || gotB.StartedAt != "" || gotB.Rounds != 0 {
		t.Fatalf("second run should still be queued and unstarted: %+v", gotB)
	}
	if !strings.Contains(h.comments(b.ID), "waits for run "+ra.ID) {
		t.Fatalf("no wait comment on the second card: %q", h.comments(b.ID))
	}
	if rows := projectLockRows(t, h.st); len(rows) != 0 {
		t.Fatalf("the relocation must release the project lock: %v", rows)
	}

	h.tick() // A runs in its worktree, B takes the project folder
	dirs := map[string]bool{}
	for _, c := range h.ad.calls()[1:] {
		dirs[c.Dir] = true
	}
	if !dirs[wt] || !dirs[project] {
		t.Fatalf("second tick dirs %v, want the worktree and the project", dirs)
	}
	if n := strings.Count(h.comments(b.ID), "waits for run"); n != 1 {
		t.Fatalf("the wait comment is written once, got %d", n)
	}
}

// A run that ends before it relocates releases the project to the next run.
func TestProjectLockReleasedWhenTheRunEnds(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("")} // ends without a stop: failed, released
	a, b := h.card("feature", store.TagAgentReady), h.card("feature", store.TagAgentReady)
	ra, rb := h.enqueue(a), h.enqueue(b)
	h.tick()
	if got := h.run(ra.ID); got.Active() {
		t.Fatalf("first run should have ended: %+v", got)
	}
	if got := h.run(rb.ID); got.StartedAt != "" {
		t.Fatalf("second run started beside the first: %+v", got)
	}
	h.tick()
	if got := h.run(rb.ID); got.StartedAt == "" || len(h.ad.calls()) != 2 {
		t.Fatalf("second run did not start after the first ended: %+v (%d calls)", got, len(h.ad.calls()))
	}
	if rows := projectLockRows(t, h.st); len(rows) != 0 {
		t.Fatalf("locks left: %v", rows)
	}
}

// A run waiting on the human before it relocated keeps the project; a cancel frees it.
func TestProjectLockHeldWhileWaitingThenCancel(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a, b := h.card("feature", store.TagAgentReady), h.card("feature", store.TagAgentReady)
	ra, rb := h.enqueue(a), h.enqueue(b)
	h.tick()
	if got := h.run(ra.ID); got.Status != store.StatusWaiting {
		t.Fatalf("first run should wait on its question: %+v", got)
	}
	h.tick()
	if got := h.run(rb.ID); got.StartedAt != "" {
		t.Fatalf("second run started while the first waits in the project folder: %+v", got)
	}
	if rows := projectLockRows(t, h.st); len(rows) != 1 {
		t.Fatalf("want one lock row, got %v", rows)
	}
	if _, err := h.svc.Cancel(context.Background(), ra.ID); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.run(rb.ID); got.StartedAt == "" {
		t.Fatalf("second run did not start after the cancel: %+v", got)
	}
}

// Cards of different projects never wait for each other.
func TestProjectLockOtherProjectsRunTogether(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a, b := h.projectCard("apps/billing"), h.projectCard("apps/payroll")
	h.enqueue(a)
	h.enqueue(b)
	h.tick()
	if n := len(h.ad.calls()); n != 2 {
		t.Fatalf("%d rounds; two projects run at once", n)
	}
}

// A blocked run does not use a concurrency slot: a later run of another project starts.
func TestProjectLockDoesNotStarveOtherProjects(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a, b := h.card("feature", store.TagAgentReady), h.card("feature", store.TagAgentReady)
	h.enqueue(a)
	h.tick() // a waits on its question, holding apps/billing
	h.enqueue(b)
	c := h.projectCard("apps/payroll")
	rc := h.enqueue(c)
	if err := h.st.SetConcurrency(1); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.run(rc.ID); got.StartedAt == "" {
		t.Fatalf("the payroll run is starved behind the blocked billing run: %+v", got)
	}
}

// A restart clears locks of runs that ended or are gone, and keeps the one an active
// pre-relocation run holds.
func TestRecoverStaleClearsProjectLocks(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a := h.card("feature", store.TagAgentReady)
	ra := h.enqueue(a)
	h.tick() // waiting, holds the lock
	if err := h.st.SetSetting(settingProjectLock+"c:/gone", "r-gonegonegone"); err != nil {
		t.Fatal(err)
	}
	other := h.projectCard("apps/payroll")
	ro := h.enqueue(other)
	ro.Status, ro.EndedAt = store.StatusFailed, h.svc.stamp()
	h.st.SaveRun(ro)
	if err := h.st.SetSetting(settingProjectLock+"c:/ended", ro.ID); err != nil {
		t.Fatal(err)
	}

	// A new service on the same store (the hub restarted).
	svc, err := New(h.svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverStale(); err != nil {
		t.Fatal(err)
	}
	rows := projectLockRows(t, h.st)
	if len(rows) != 1 {
		t.Fatalf("want only the waiting run's lock, got %v", rows)
	}
	for _, v := range rows {
		if v != ra.ID {
			t.Fatalf("kept %s, want %s", v, ra.ID)
		}
	}
	if got := svc.ProjectLocks(); len(got) != 1 {
		t.Fatalf("the restarted service knows the kept lock in memory: %v", got)
	}
}

// Drain returns when the only due run waits for a project lock (it never spins).
func TestDrainReturnsWhenOnlyBlockedRunsAreDue(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a, b := h.card("feature", store.TagAgentReady), h.card("feature", store.TagAgentReady)
	h.enqueue(a)
	rb := h.enqueue(b)
	ctx, cancel := context.WithTimeout(context.Background(), 10e9)
	defer cancel()
	if err := h.svc.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.run(rb.ID); got.Status != store.StatusQueued || got.StartedAt != "" {
		t.Fatalf("%+v", got)
	}
}

// The project lock's write transaction runs without s.mu: a DB write lock held by another
// process would otherwise stall Cancel and the flight bookkeeping for the whole busy timeout.
func TestProjectLockTakenWithoutServiceMutex(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a := h.card("feature", store.TagAgentReady)
	h.enqueue(a)
	called, free := false, false
	h.svc.beforeProjectLock = func() {
		called = true
		if h.svc.mu.TryLock() {
			free = true
			h.svc.mu.Unlock()
		}
	}
	h.tick()
	if !called || !free {
		t.Fatalf("project lock taken under s.mu (called %v, mu free %v)", called, free)
	}
	if rows := projectLockRows(t, h.st); len(rows) != 1 {
		t.Fatalf("lock rows %v", rows)
	}
}

// A run cancelled while its project lock is being taken does not start, and leaves no lock.
func TestProjectLockCancelledWhileTaking(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{result("Which export format, CSV or XLSX?")}
	a := h.card("feature", store.TagAgentReady)
	ra := h.enqueue(a)
	h.svc.beforeProjectLock = func() {
		h.svc.beforeProjectLock = nil
		if _, err := h.svc.Cancel(context.Background(), ra.ID); err != nil {
			t.Error(err)
		}
	}
	h.tick()
	if got := h.run(ra.ID); got.StartedAt != "" || got.Active() {
		t.Fatalf("a cancelled run started: %+v", got)
	}
	if n := len(h.ad.calls()); n != 0 {
		t.Fatalf("%d rounds ran", n)
	}
	if rows := projectLockRows(t, h.st); len(rows) != 0 {
		t.Fatalf("lock left by a cancelled run: %v", rows)
	}
}
