package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestEnqueueCoalescesWakeups(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	first, created, _ := h.svc.Enqueue(context.Background(), c.ID, "move")
	for i := 0; i < 4; i++ {
		again, made, err := h.svc.Enqueue(context.Background(), c.ID, "wake")
		if err != nil || made || again.ID != first.ID {
			t.Fatalf("wake %d: %+v %v %v", i, again, made, err)
		}
	}
	runs, _ := h.st.RunsForCard(c.ID)
	got, _ := h.st.Card(c.ID)
	if !created || len(runs) != 1 || got.LockedByRun != first.ID || first.OnRamp != store.OnRampFeature {
		t.Fatalf("runs %d lock %q first %+v", len(runs), got.LockedByRun, first)
	}
}

func TestEnqueueReleasesAnOrphanLock(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	h.st.Checkout(c.ID, "r-stalestalest")
	r, made, err := h.svc.Enqueue(context.Background(), c.ID, "requested")
	if err != nil || !made {
		t.Fatalf("an orphan lock does not block a new run: %v %v", made, err)
	}
	if got, _ := h.st.Card(c.ID); got.LockedByRun != r.ID {
		t.Fatalf("lock %q, want %q", got.LockedByRun, r.ID)
	}
	if !strings.Contains(h.comments(c.ID), "released an orphan lock left by run r-stalestalest") {
		t.Fatal(h.comments(c.ID))
	}
	// A lock left by an ended run of this card is an orphan too.
	ended, _ := h.svc.Cancel(context.Background(), r.ID)
	h.st.Checkout(c.ID, ended.ID)
	if _, made, err := h.svc.Enqueue(context.Background(), c.ID, "again"); err != nil || !made {
		t.Fatalf("ended holder: %v %v", made, err)
	}
}

func TestAgentsCannotRunOrCancel(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	if _, err := h.svc.Run(WithAgent(context.Background()), c.ID); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent cannot start a run: %v", err)
	}
	if runs, _ := h.st.RunsForCard(c.ID); len(runs) != 0 {
		t.Fatalf("no run was created: %d", len(runs))
	}
	r := h.enqueue(c)
	if _, err := h.svc.Cancel(WithAgent(context.Background()), r.ID); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent cannot cancel a run: %v", err)
	}
	if got := h.run(r.ID); got.Status != store.StatusQueued {
		t.Fatalf("%+v", got)
	}
}

func TestCancelRacingATickNeverResurrectsTheRun(t *testing.T) {
	for i := 0; i < 15; i++ {
		h := newHarness(t)
		h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{StopReason: "error_max_turns", SessionID: "s1", CostKnown: true}, nil
		}}
		c := h.card("feature", store.TagAgentReady)
		r := h.enqueue(c)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); h.svc.Tick(context.Background()) }()
		go func() { defer wg.Done(); h.svc.Cancel(context.Background(), r.ID) }()
		wg.Wait()
		h.svc.Wait()
		h.tick()
		h.tick()
		got := h.run(r.ID)
		card, _ := h.st.Card(c.ID)
		if got.Status != store.StatusCancelled || got.EndedAt == "" || card.LockedByRun != "" || len(h.ad.calls()) > 1 {
			t.Fatalf("iteration %d: %+v lock %q calls %d", i, got, card.LockedByRun, len(h.ad.calls()))
		}
	}
}

func TestARoundOverAnEndedRunAppliesNothing(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	var id string
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		r, _ := h.st.Run(id)
		r.Status, r.EndedAt, r.StopReason = store.StatusCancelled, "2026-10-21T09:00:00Z", "cancelled elsewhere"
		h.st.SaveRun(r)
		h.st.Release(c.ID, id)
		return adapter.RunResult{StopReason: "error_max_turns", CostKnown: true}, nil
	}}
	id = h.enqueue(c).ID
	h.tick()
	h.tick()
	if got := h.run(id); got.Status != store.StatusCancelled || got.StopReason != "cancelled elsewhere" || len(h.ad.calls()) != 1 {
		t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
	}
}

func TestHubShutdownRequeuesTheRun(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		close(started)
		<-ctx.Done()
		return adapter.RunResult{ExitCode: -1}, ctx.Err()
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	r.Slug, r.State = "demo", []byte(`{"slug":"demo"}`)
	h.st.SaveRun(r)
	ctx, stop := context.WithCancel(context.Background())
	h.svc.Tick(ctx)
	<-started
	stop()
	h.svc.Wait()
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusQueued || got.EndedAt != "" || got.Phase != store.PhaseResume || card.LockedByRun != r.ID {
		t.Fatalf("%+v lock %q", got, card.LockedByRun)
	}
	if !strings.Contains(h.comments(c.ID), "hub stopped") || strings.Contains(h.comments(c.ID), "cancelled") {
		t.Fatal(h.comments(c.ID))
	}
	if err := h.svc.RecoverStale(); err != nil {
		t.Fatal(err)
	}
	if got := h.run(r.ID); got.Status != store.StatusQueued {
		t.Fatalf("RecoverStale failed a requeued run: %+v", got)
	}
}

func TestEndingAnEndedRunChangesNothing(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature")
	old := h.enqueue(c)
	h.svc.Cancel(context.Background(), old.ID)
	next := h.enqueue(c)
	card, _ := h.st.Card(c.ID)
	h.svc.fail(old, card, "late failure", "")
	if got := h.run(old.ID); got.Status != store.StatusCancelled {
		t.Fatalf("an ended run was ended again: %+v", got)
	}
	if got, _ := h.st.Card(c.ID); got.LockedByRun != next.ID {
		t.Fatalf("the next run lost its lock: %q", got.LockedByRun)
	}
}

func TestConcurrentTicksRespectTheLimit(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
	}}
	for i := 0; i < 5; i++ { // one project each: the pre-relocation lock is not what limits them
		h.enqueue(h.projectCard("apps/p" + strconv.Itoa(i)))
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.svc.Tick(context.Background()) }()
	}
	wg.Wait()
	running, _ := h.st.RunsWithStatus(store.StatusRunning)
	close(release)
	h.svc.Wait()
	if len(running) != 2 {
		t.Fatalf("%d runs started, concurrency is 2", len(running))
	}
}

func TestRelocateTargetMustStayInTheWorkspace(t *testing.T) {
	outside := "/tmp/x"
	if runtime.GOOS == "windows" {
		outside = `C:\other-client\repo`
	}
	t.Run("outside", func(t *testing.T) {
		h := newHarness(t)
		h.ad.steps = []step{result("TQ_HUB_RELOCATE " + outside + " demo")}
		c := h.card("feature", store.TagAgentReady)
		r := h.enqueue(c)
		h.tick()
		got := h.run(r.ID)
		if got.Status != store.StatusFailed || got.StopReason != "relocate outside workspace" || got.Worktree != "" || len(h.ad.calls()) != 1 {
			t.Fatalf("%+v", got)
		}
		if !strings.Contains(h.comments(c.ID), outside) {
			t.Fatal(h.comments(c.ID))
		}
	})
	t.Run("wrong sibling", func(t *testing.T) {
		h := newHarness(t)
		h.ad.steps = []step{result("TQ_HUB_RELOCATE " + filepath.Join(filepath.Dir(h.root), "someone-else") + " demo")}
		r := h.enqueue(h.card("feature", store.TagAgentReady))
		h.tick()
		if got := h.run(r.ID); got.StopReason != "relocate outside workspace" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("sibling convention", func(t *testing.T) {
		h := newHarness(t)
		sib := filepath.Join(filepath.Dir(h.root), filepath.Base(h.root)+"-build-demo")
		if err := os.MkdirAll(sib, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(sib) })
		target := sib
		if runtime.GOOS == "windows" {
			target = strings.ToUpper(sib[:1]) + strings.ToLower(sib[1:]) // case does not matter on Windows
		}
		h.ad.steps = []step{result("TQ_HUB_RELOCATE " + target + " demo")}
		c, err := h.st.AddCard(store.NewCard{Title: "Root project", Kind: "feature", Tags: []string{store.TagAgentReady}}, "tester")
		if err != nil {
			t.Fatal(err)
		}
		r := h.enqueue(c)
		h.tick()
		if got := h.run(r.ID); got.Status != store.StatusQueued || got.Phase != store.PhaseRelocated || !strings.EqualFold(got.Worktree, sib) {
			t.Fatalf("%+v", got)
		}
	})
}

func TestNotificationsAreMasked(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", CostKnown: true,
			PermissionDenials: []adapter.PermissionDenial{{Tool: "Bash", Input: map[string]any{"command": "mail jane.doe@example.com"}}}}, nil
	}}
	h.enqueue(h.card("feature", store.TagAgentReady))
	h.tick()
	notes := h.notifications()
	if len(notes) != 1 || strings.Contains(notes[0], "jane.doe@example.com") {
		t.Fatalf("%q", notes)
	}
}

func TestRelocateThenSecondRoundInTheWorktree(t *testing.T) {
	h := newHarness(t)
	wt := filepath.Join(h.root, "billing-build-demo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	h.ad.steps = []step{
		result("Worktree and hooks ready.\nTQ_HUB_RELOCATE " + wt + " demo"),
		withState(h.gt, "demo", loadState(t, "waiting"), result("Plan written; waiting for approval.")),
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	mid := h.run(r.ID)
	if mid.Status != store.StatusQueued || mid.Worktree != wt || mid.Slug != "demo" || mid.Phase != store.PhaseRelocated || mid.Retried != 0 || mid.Rounds != 1 {
		t.Fatalf("after hand-off %+v", mid)
	}
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 2 || calls[0].Dir != filepath.Join(h.root, "apps", "billing") || calls[1].Dir != wt {
		t.Fatalf("dirs %q", []string{calls[0].Dir, calls[len(calls)-1].Dir})
	}
	if calls[1].SessionID != "" || calls[1].Prompt != calls[0].Prompt || !strings.HasPrefix(calls[0].Prompt, "/tentaqles:feature ") {
		t.Fatalf("second round prompt %q session %q", calls[1].Prompt, calls[1].SessionID)
	}
	end := h.run(r.ID)
	pend, _ := h.st.PendingDecisions()
	if end.Status != store.StatusWaiting || len(pend) != 1 || pend[0].ID != "d1a2b3c4" || pend[0].Slug != "demo" || pend[0].RunID != r.ID {
		t.Fatalf("run %+v pending %+v", end, pend)
	}
	if !strings.Contains(h.comments(c.ID), "agent: Plan written") || len(h.notifications()) != 1 {
		t.Fatalf("comments %q notes %v", h.comments(c.ID), h.notifications())
	}
	if strings.Contains(h.comments(c.ID), "TQ_HUB_RELOCATE") {
		t.Fatal("the relocate line is not an agent comment")
	}
}

func TestRunnerRelocateLoopStops(t *testing.T) {
	h := newHarness(t)
	wt := filepath.Join(h.root, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	h.ad.steps = []step{result("TQ_HUB_RELOCATE " + wt + " demo")}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	h.tick()
	h.tick()
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusFailed || got.StopReason != "relocate loop" || len(h.ad.calls()) != 2 || card.LockedByRun != "" {
		t.Fatalf("run %+v calls %d lock %q", got, len(h.ad.calls()), card.LockedByRun)
	}
}

func TestSlugIsDiscoveredFromTheTaskBlock(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	st := loadState(t, "waiting")
	st.Task.Hub = c.ID
	dir := filepath.Join(h.root, "apps", "billing")
	h.gt.slugs[dir] = []string{"other", "demo"}
	other := loadState(t, "waiting")
	other.Task.Hub = "c-someoneelse1"
	h.gt.set("other", other)
	h.gt.set("demo", st)
	r := h.enqueue(c)
	h.tick()
	if got := h.run(r.ID); got.Slug != "demo" || got.Status != store.StatusWaiting {
		t.Fatalf("%+v", got)
	}
}

func TestWaitingOnAResultMovesTheCardToReview(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	h.st.MoveCard(c.ID, "in progress", "tester")
	st := loadState(t, "waiting")
	st.PendingDecisions[0].Kind = "result"
	st.PendingDecisions[0].Options = []string{"approve", "request-changes", "stop"}
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.gt.set("demo", st)
	h.tick()
	if got, _ := h.st.Card(c.ID); got.Lane != "review" || got.LockedByRun != r.ID {
		t.Fatalf("card %+v (a waiting run keeps its lock)", got)
	}
}

func TestFinishedHaltedRolledBack(t *testing.T) {
	for name, want := range map[string]struct{ status, reason, comment string }{
		"finished_build": {store.StatusFinished, "", ""},
		"halted":         {store.StatusFailed, "behavior failed 3x after split", "The build halted at checkpoint 2"},
		"rolled_back":    {store.StatusFailed, "rolled back", "Re-approve in your own terminal"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c := h.card("feature", store.TagAgentReady)
			r := h.enqueue(c)
			r.Slug = "demo"
			h.st.SaveRun(r)
			h.gt.set("demo", loadState(t, name))
			h.tick()
			got := h.run(r.ID)
			card, _ := h.st.Card(c.ID)
			if got.Status != want.status || got.StopReason != want.reason || card.LockedByRun != "" {
				t.Fatalf("%+v lock %q", got, card.LockedByRun)
			}
			if want.comment != "" && !strings.Contains(h.comments(c.ID), want.comment) {
				t.Fatal(h.comments(c.ID))
			}
			if name == "finished_build" && card.Lane != "review" {
				t.Fatalf("a finished run moves the card to review: %s", card.Lane)
			}
		})
	}
}

func TestRetryOnceAfterTwoMinutes(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "", CostKnown: true}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	got := h.run(r.ID)
	if got.Status != store.StatusQueued || got.Retried != 1 || got.NotBefore != h.clock.Now().Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(h.comments(c.ID), "retrying once in 2m0s") || !strings.Contains(h.comments(c.ID), "fake round 0") {
		t.Fatal(h.comments(c.ID))
	}
	h.tick()
	if len(h.ad.calls()) != 1 {
		t.Fatal("no retry before the delay")
	}
	h.clock.Advance(2*time.Minute + time.Second)
	h.tick()
	got = h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if len(h.ad.calls()) != 2 || got.Status != store.StatusFailed || got.StopReason != "exit 1" || card.LockedByRun != "" {
		t.Fatalf("%+v calls %d", got, len(h.ad.calls()))
	}
	if !strings.Contains(h.comments(c.ID), "failed again (exit 1)") {
		t.Fatal(h.comments(c.ID))
	}
}

func TestPermissionDenialBecomesAHubDecision(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
		return adapter.RunResult{ExitCode: 1, StopReason: "error_during_execution", Transcript: "Bash was not allowed", CostKnown: true,
			PermissionDenials: []adapter.PermissionDenial{{Tool: "Bash", Input: map[string]any{"command": "npm test"}}}}, nil
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.tick()
	pend, _ := h.st.PendingDecisions()
	if h.run(r.ID).Status != store.StatusWaiting || len(pend) != 1 || pend[0].Kind != store.KindPermission || pend[0].Data["tool"] != "Bash(npm test)" {
		t.Fatalf("pending %+v", pend)
	}
	if strings.Join(pend[0].Options, ",") != "allow-once,stop" {
		t.Fatal(pend[0].Options)
	}
}

func TestMaxTurnsResumesTheSameSession(t *testing.T) {
	h := newHarness(t)
	h.ad.steps = []step{
		func(context.Context, adapter.RunRequest) (adapter.RunResult, error) {
			return adapter.RunResult{StopReason: "error_max_turns", SessionID: "s-7", CostKnown: true}, nil
		},
		result("done"),
	}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	r.Slug = "demo"
	h.st.SaveRun(r)
	h.gt.set("demo", loadState(t, "in_progress"))
	h.tick()
	if got := h.run(r.ID); got.Status != store.StatusQueued || got.SessionID != "s-7" || got.Phase != store.PhaseResume {
		t.Fatalf("%+v", got)
	}
	h.tick()
	calls := h.ad.calls()
	if calls[1].SessionID != "s-7" {
		t.Fatalf("resume session %q", calls[1].SessionID)
	}
	if !strings.HasPrefix(calls[0].Prompt, "/tentaqles:feature ") || calls[1].Prompt != "/tentaqles:build resume demo --headless" {
		t.Fatalf("once the plan exists the build resumes: %q then %q", calls[0].Prompt, calls[1].Prompt)
	}
}

func TestLaunchEnvironmentReachesTheAdapter(t *testing.T) {
	h := newHarness(t)
	c := h.card("hotfix", store.TagAgentReady)
	h.enqueue(c)
	h.tick()
	env := strings.Join(h.ad.calls()[0].Env, "\n")
	if !strings.Contains(env, "TENTAQLES_HEADLESS=1") || !strings.Contains(env, `TQ_HUB_TASK={"hub":"`+c.ID+`","workspace":"acme","kind":"hotfix","external":null}`) {
		t.Fatal(env)
	}
	for _, bad := range []string{"TQ_BUILD_TOKEN", "TQ_BUILD_ACTOR", "TENTAQLES_HEADLESS_CHILD"} {
		if strings.Contains(env, bad) {
			t.Fatalf("%s reached a run", bad)
		}
	}
}

func TestConcurrencyLimitAndFIFO(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
	}}
	var cards []store.Card
	for i := 0; i < 3; i++ {
		c := h.projectCard("apps/p" + strconv.Itoa(i)) // one project each (pre-relocation lock)
		h.enqueue(c)
		cards = append(cards, c)
	}
	if err := h.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(h.ad.calls()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	h.svc.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if n := len(h.ad.calls()); n != 2 {
		t.Fatalf("%d rounds running, concurrency is 2", n)
	}
	running, _ := h.st.RunsWithStatus(store.StatusRunning)
	ids := map[string]bool{}
	for _, r := range running {
		ids[r.CardID] = true
	}
	if !ids[cards[0].ID] || !ids[cards[1].ID] {
		t.Fatal("the first two queued runs start first")
	}
	close(release)
	h.svc.Wait()
	h.tick()
	if n := len(h.ad.calls()); n != 3 {
		t.Fatalf("third run did not start: %d", n)
	}
}

func TestCancelARunningRound(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	h.ad.steps = []step{func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
		close(started)
		<-ctx.Done()
		return adapter.RunResult{ExitCode: -1}, ctx.Err()
	}}
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	h.svc.Tick(context.Background())
	<-started
	got, err := h.svc.Cancel(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusCancelled || card.LockedByRun != "" || len(h.gt.answers) != 0 {
		t.Fatalf("%+v lock %q answers %v", got, card.LockedByRun, h.gt.answers)
	}
	if !strings.Contains(h.comments(c.ID), "gate state stays as it is") {
		t.Fatal(h.comments(c.ID))
	}
	if _, err := h.svc.Cancel(context.Background(), r.ID); err == nil {
		t.Fatal("cancelling an ended run is an error")
	}
}

func TestRecoverStaleRunningRuns(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	r.Status = store.StatusRunning
	h.st.SaveRun(r)
	if err := h.svc.RecoverStale(); err != nil {
		t.Fatal(err)
	}
	got := h.run(r.ID)
	card, _ := h.st.Card(c.ID)
	if got.Status != store.StatusFailed || !strings.Contains(got.StopReason, "hub stopped") || card.LockedByRun != "" {
		t.Fatalf("%+v", got)
	}
	orphan := h.card("feature")
	h.st.Checkout(orphan.ID, "r-gonegonegone")
	h.svc.RecoverStale()
	if got, _ := h.st.Card(orphan.ID); got.LockedByRun != "" {
		t.Fatal("a lock held by a run that does not exist is released")
	}
}

func TestViewsAndAgentBudget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.Add(WithAgent(ctx), store.NewCard{Title: "x"}, &store.Budget{Rounds: 1}); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("an agent cannot set a budget: %v", err)
	}
	b := store.Budget{Tokens: 1000, USD: 1, Minutes: 5, Rounds: 2}
	c, err := h.svc.Add(ctx, store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}, ProjectDir: "apps/billing"}, &b)
	if err != nil || !strings.HasPrefix(c.Body, "---\nbudget: ") {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := h.svc.Add(WithAgent(ctx), store.NewCard{Title: "agent card"}, nil); err != nil {
		t.Fatal("agents may add cards")
	}
	r, _ := h.svc.Run(ctx, c.ID)
	if r.Budget != b {
		t.Fatalf("budget from front matter: %+v", r.Budget)
	}
	h.st.AskHub(c.ID, r.ID, "", "?", []string{"a"}, nil)
	views, _ := h.svc.List(ctx, store.CardFilter{}, true)
	if len(views) != 1 || views[0].Waiting != 1 || views[0].RunStatus != store.StatusQueued {
		t.Fatalf("%+v", views)
	}
	detail, err := h.svc.Show(ctx, c.ID)
	if err != nil || len(detail.Runs) != 1 || len(detail.Decisions) != 1 {
		t.Fatalf("%+v %v", detail, err)
	}
	if cm, err := h.svc.Comment(WithAgent(ctx), c.ID, "PR: https://github.com/acme/x/pull/7"); err != nil || cm.AuthorKind != store.AuthorAgent {
		t.Fatalf("%+v %v", cm, err)
	}
	items, _ := h.svc.Waiting(ctx)
	if len(items) != 1 || items[0].Workspace != "acme" || items[0].Card.ID != c.ID {
		t.Fatalf("%+v", items)
	}
}

func TestTickWithAnEndedContextStartsNothing(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	r := h.enqueue(c)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := h.svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	got := h.run(r.ID)
	if len(h.ad.calls()) != 0 || got.Status != store.StatusQueued || got.StartedAt != "" || got.Rounds != 0 || h.comments(c.ID) != "" {
		t.Fatalf("a dead-context tick started work: calls %d run %+v comments %q", len(h.ad.calls()), got, h.comments(c.ID))
	}
}

func TestHubShutdownDetectsAPlanWrittenDuringTheRound(t *testing.T) {
	h := newHarness(t)
	c := h.card("feature", store.TagAgentReady)
	st := loadState(t, "waiting")
	st.Task.Hub = c.ID
	h.gt.slugs[filepath.Join(h.root, "apps", "billing")] = []string{"demo"}
	started := make(chan struct{})
	h.ad.steps = []step{
		func(ctx context.Context, req adapter.RunRequest) (adapter.RunResult, error) {
			h.gt.set("demo", st) // the on-ramp wrote the plan, then the hub stopped
			close(started)
			<-ctx.Done()
			return adapter.RunResult{ExitCode: -1}, ctx.Err()
		},
		result("Waiting for you."),
	}
	r := h.enqueue(c)
	ctx, stop := context.WithCancel(context.Background())
	h.svc.Tick(ctx)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		stop()
		t.Fatal("round did not start")
	}
	stop()
	h.svc.Wait()
	got := h.run(r.ID)
	if got.Status != store.StatusQueued || got.Slug != "demo" || got.Phase != store.PhaseResume || len(got.State) == 0 {
		t.Fatalf("%+v", got)
	}
	h.tick()
	calls := h.ad.calls()
	if len(calls) != 2 || calls[1].Prompt != "/tentaqles:build resume demo --headless" {
		t.Fatalf("restart re-sent the on-ramp: %d calls, last %q", len(calls), calls[len(calls)-1].Prompt)
	}
}
