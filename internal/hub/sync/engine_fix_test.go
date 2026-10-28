package sync

import (
	"context"
	"errors"
	gosync "sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// A local title edit survives every later pull, not only the first one.
func TestLocalTextEditSurvivesTwoPulls(t *testing.T) {
	r := newRig(t)
	c := r.linked("200", "To Do", CategoryTodo)
	mine := "My title"
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Title: &mine}, "tester"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	r.fake.remoteMove("200", "In Progress") // the tracker changes, its text does not
	r.tick()
	if got := r.card("github", "200").Title; got != "My title" {
		t.Fatalf("first pull: %q", got)
	}
	time.Sleep(1100 * time.Millisecond) // the activity log has second timestamps
	r.fake.remoteMove("200", "QA")      // a second change
	r.tick()
	if got := r.card("github", "200").Title; got != "My title" {
		t.Fatalf("the second pull overwrote the local edit: %q", got)
	}
	// An untouched card keeps following the tracker across pulls.
	o := r.linked("201", "To Do", CategoryTodo)
	r.fake.remoteMove("201", "In Progress")
	r.tick()
	r.fake.mu.Lock()
	r.fake.items["201"].Title = "Renamed upstream"
	r.fake.changed = []string{"201"}
	r.fake.mu.Unlock()
	r.tick()
	if got, _ := r.st.Card(o.ID); got.Title != "Renamed upstream" {
		t.Fatalf("an unedited card did not follow the tracker: %q", got.Title)
	}
}

// Two moves of one card queued while the tracker is unreachable both go out in one pass: the
// second is not a "conflict" against the status the first one just set.
func TestTwoQueuedMovesBothSent(t *testing.T) {
	var mu gosync.Mutex
	down := false
	var r *rig
	r = newRig(t, func(c *EngineConfig) {
		c.Adapter = func(context.Context) (Adapter, error) {
			mu.Lock()
			defer mu.Unlock()
			if down {
				return nil, errors.New("dial tcp: network is unreachable")
			}
			return r.fake, nil
		}
		c.Now = func() time.Time { return time.Now().Add(time.Minute) } // past the approve grace
	})
	c := r.linked("210", "To Do", CategoryTodo)
	mu.Lock()
	down = true
	mu.Unlock()
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	approve(t, r.st, c)
	if _, err := r.eng.Flush(context.Background()); err == nil {
		t.Fatal("the adapter is down")
	}
	rows := r.outbox("")
	for _, o := range rows {
		if o.Attempts != 0 {
			t.Fatalf("an adapter error counted against the row: %+v", o)
		}
	}
	mu.Lock()
	down = false
	mu.Unlock()
	rep := r.flush()
	if rep.Sent != 3 || rep.Skipped != 0 {
		t.Fatalf("%+v %+v", rep, r.outbox(""))
	}
	calls := r.fake.callLog()
	if len(calls) != 3 || calls[0] != "move 210 In Progress" || calls[1] != "move 210 In Review" || calls[2] != "comment 210" {
		t.Fatalf("%v", calls)
	}
	if hasComment(t, r.st, c.ID, "not moving") {
		t.Fatal("a false conflict comment")
	}
}

// A transient failure backs off and the row stays pending for a long while; it never fails after
// a minute of quick flushes. A failed row can be put back with Retry.
func TestTransientErrorBacksOffAndRetry(t *testing.T) {
	var mu gosync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	r := newRig(t, func(c *EngineConfig) { c.Now = clock })
	c := r.linked("220", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	r.fake.moveErr = errors.New("502 bad gateway")
	for i := 0; i < 20; i++ { // a minute and a half of flushes every few seconds
		_, _ = r.eng.Flush(context.Background())
		advance(5 * time.Second)
	}
	rows := r.outbox(store.OutboxMoveOnStart)
	if len(rows) != 1 || rows[0].Status != store.OutboxPending {
		t.Fatalf("a short outage failed the row: %+v", rows)
	}
	if rows[0].Attempts > 4 {
		t.Fatalf("no back-off: %d attempts in 100s", rows[0].Attempts)
	}
	// A day later, still failing: the row is failed.
	for i := 0; i < 60; i++ {
		advance(30 * time.Minute)
		_, _ = r.eng.Flush(context.Background())
	}
	rows = r.outbox(store.OutboxMoveOnStart)
	if rows[0].Status != store.OutboxFailed || rows[0].LastError == "" {
		t.Fatalf("%+v", rows)
	}
	// The tracker is back; a retry sends it.
	r.fake.moveErr = nil
	if _, err := r.eng.Retry(rows[0].ID); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if _, err := r.eng.Retry(rows[0].ID); err == nil {
		t.Fatal("retried a sent row")
	}
}

// A store error while queueing a hub event keeps the cursor: the next pass queues it.
func TestProduceErrorKeepsCursor(t *testing.T) {
	r := newRig(t)
	c := r.linked("230", "To Do", CategoryTodo)
	if _, err := r.st.DB.Exec(`CREATE TRIGGER no_outbox BEFORE INSERT ON sync_outbox BEGIN SELECT RAISE(ABORT, 'database is locked'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	_, _ = r.eng.Flush(context.Background())
	if rows := r.outbox(""); len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}
	if _, err := r.st.DB.Exec(`DROP TRIGGER no_outbox`); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Queued != 1 || rep.Sent != 1 {
		t.Fatalf("the event was lost: %+v", rep)
	}
}

// A remote move that arrives while a run holds the card is applied when the run ends, and an
// approval meanwhile does not reopen the item.
func TestDeferredMoveWhileLocked(t *testing.T) {
	r := newRig(t)
	c := r.linked("240", "In Progress", CategoryActive)
	if ok, err := r.st.Checkout(c.ID, "r-test"); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	r.fake.remoteMove("240", "Done")
	if rep := r.tick(); rep.Moved != 0 {
		t.Fatalf("%+v", rep)
	}
	got := r.card("github", "240")
	if got.Lane != "in progress" || got.ExternalLane != "In Progress" {
		t.Fatalf("a deferred move must keep the old external lane: %+v", got)
	}
	r.tick() // nothing new on the tracker; still locked
	if got := r.card("github", "240"); got.Lane != "in progress" {
		t.Fatalf("%+v", got)
	}
	if err := r.st.Release(c.ID, "r-test"); err != nil {
		t.Fatal(err)
	}
	if rep := r.tick(); rep.Moved != 1 {
		t.Fatalf("the deferred move was not applied after the run: %+v", rep)
	}
	got = r.card("github", "240")
	if got.Lane != "done" || got.ExternalLane != "Done" {
		t.Fatalf("%+v", got)
	}
	if rep := r.tick(); rep.Moved != 0 {
		t.Fatalf("applied twice: %+v", rep)
	}

	// Locked again, remote moves to Done, the card is approved meanwhile: the review move is skipped.
	d := r.linked("241", "In Progress", CategoryActive)
	if ok, _ := r.st.Checkout(d.ID, "r-two"); !ok {
		t.Fatal("checkout")
	}
	r.fake.remoteMove("241", "Done")
	r.tick()
	_ = r.st.Release(d.ID, "r-two")
	approveAt(t, r, r.card("github", "241"))
	for _, call := range r.fake.callLog() {
		if call == "move 241 In Review" {
			t.Fatalf("reopened a closed item: %v", r.fake.callLog())
		}
	}
}

func approveAt(t *testing.T, r *rig, c store.Card) {
	t.Helper()
	approve(t, r.st, c)
	r.eng.cfg.Now = func() time.Time { return time.Now().Add(time.Minute) }
	_, _ = r.eng.Flush(context.Background())
}

// A local planning move (no outbound row) is not a conflict forever: once a pull has seen the item
// after it, a later remote move is a plain inbound move.
func TestPlanningMoveIsNotAConflictForever(t *testing.T) {
	r := newRig(t)
	c := r.linked("250", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "backlog", "tester"); err != nil {
		t.Fatal(err)
	}
	if rows := r.outbox(""); len(rows) != 0 {
		t.Fatalf("a planning move queued %+v", rows)
	}
	r.fake.mu.Lock()
	r.fake.items["250"].Body = "new body"
	r.fake.changed = []string{"250"}
	r.fake.mu.Unlock()
	time.Sleep(1100 * time.Millisecond) // the pull is seen after the move (second timestamps)
	r.tick()
	time.Sleep(1100 * time.Millisecond)
	r.fake.remoteMove("250", "In Progress")
	rep := r.tick()
	if rep.Conflicts != 0 || rep.Moved != 1 {
		t.Fatalf("%+v", rep)
	}
}
