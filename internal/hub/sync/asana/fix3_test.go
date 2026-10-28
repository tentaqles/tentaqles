package asana

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

func anyone(c *hubsync.Config) { c.AssignedTo = "anyone" }

func ids(items []hubsync.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func subtaskPolls(reqs []string) int {
	n := 0
	for _, r := range reqs {
		if strings.HasPrefix(r, "GET ") && strings.HasSuffix(r, "/subtasks") {
			n++
		}
	}
	return n
}

// A child that changes under an unchanged parent is found with no SetMirroredParents call: the
// adapter remembers the parents it returned.
func TestChildOfUnchangedParentWithoutWatch(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, anyone)
	clock := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }
	ctx := context.Background()
	items, wm, err := a.ListChanged(ctx, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	if err != nil || !slices.Contains(ids(items), "1001") {
		t.Fatalf("first tick: %v %v", ids(items), err)
	}
	f.mu.Lock()
	f.tasks["1005"].Name = "Child step v2"
	f.tasks["1005"].Modified = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	f.mu.Unlock()
	clock = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	items, _, err = a.ListChanged(ctx, wm)
	if err != nil || !slices.Equal(ids(items), []string{"1005"}) || items[0].Title != "Child step v2" {
		t.Fatalf("child of an unchanged parent: %v %v", ids(items), err)
	}
}

// The watermark never passes the tick's start (minus the skew): a task read early in the tick that
// changes later in it must not be skipped because a later read saw a newer modified_at.
func TestWatermarkCappedAtTickStart(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	start := time.Date(2026, 10, 20, 13, 0, 0, 0, time.UTC) // 1008 (14:00) reads as modified mid-tick
	a.now = func() time.Time { return start }
	since := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	_, wm, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if want := start.Add(-2 * time.Minute); !wm.Equal(want) {
		t.Fatalf("watermark %v, want %v", wm, want)
	}
	// a clock behind since never moves the watermark back
	a.now = func() time.Time { return since.Add(-time.Hour) }
	if _, wm, err = a.ListChanged(context.Background(), since); err != nil || !wm.Equal(since) {
		t.Fatalf("behind: %v %v", wm, err)
	}
}

// Parent polls are bounded per tick and rotate, so every watched parent is polled in turn and a
// child changed while its parent waited is still found.
func TestParentPollsAreBudgeted(t *testing.T) {
	f := newFake(t)
	since := time.Date(2026, 10, 20, 10, 30, 0, 0, time.UTC)
	var parents, children []string
	f.mu.Lock()
	for i := range 5 {
		p, c := strconv.Itoa(2000+i), strconv.Itoa(2100+i)
		f.tasks[p] = &fakeTask{GID: p, Name: "Parent " + p, Modified: since.Add(-48 * time.Hour), Section: "301"}
		f.tasks[c] = &fakeTask{GID: c, Name: "Child " + c, Modified: since.Add(30 * time.Minute), Parent: p}
		parents, children = append(parents, p), append(children, c)
	}
	f.mu.Unlock()
	a := newTest(t, f, anyone)
	a.pollBudget = 2
	clock := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return clock }
	if err := a.SetMirroredParents(parents); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	wm := since
	for tick := range 3 {
		f.reset()
		items, next, err := a.ListChanged(context.Background(), wm)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if n := subtaskPolls(f.requests()); n > 2 {
			t.Fatalf("tick %d polled %d parents", tick, n)
		}
		for _, it := range items {
			seen[it.ID] = true
		}
		wm = next
		clock = clock.Add(24 * time.Hour)
	}
	for _, c := range children {
		if !seen[c] {
			t.Fatalf("child %s never listed: %v", c, seen)
		}
	}
}

// A 429 on a parent poll keeps the tick: the items read come back, and the parent is polled next.
func TestPollRateLimitKeepsTheTick(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, anyone)
	a.now = func() time.Time { return time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) }
	since := time.Date(2026, 10, 20, 11, 30, 0, 0, time.UTC) // 1004 (12:00) is listed
	if err := a.SetMirroredParents([]string{"1001"}); err != nil {
		t.Fatal(err)
	}
	f.limitOn = "GET /api/1.0/tasks/1001/subtasks"
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil || !slices.Equal(ids(items), []string{"1004"}) {
		t.Fatalf("poll 429: %v %v", ids(items), err)
	}
	items, _, err = a.ListChanged(context.Background(), wm) // modified_since is inclusive: 1004 again
	if got := ids(items); err != nil || !slices.Contains(got, "1005") || !slices.Contains(got, "1008") {
		t.Fatalf("next tick: %v %v", ids(items), err)
	}
}

// A subtask outside the project only follows the status's completion: it is never added to the
// project. A top-level task outside the project is refused.
func TestMoveSubtaskOutsideTheProject(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	ctx := context.Background()
	if err := a.Move(ctx, "1005", "Done"); err != nil {
		t.Fatal(err)
	}
	if tk := f.tasks["1005"]; !tk.Completed || tk.Section != "" {
		t.Fatalf("subtask to done: %+v", tk)
	}
	if err := a.Move(ctx, "1005", "Doing"); err != nil || f.tasks["1005"].Completed {
		t.Fatalf("subtask reopen: %+v %v", f.tasks["1005"], err)
	}
	for _, r := range f.requests() {
		if strings.HasSuffix(r, "/addTask") {
			t.Fatalf("a subtask was added to the project: %v", f.requests())
		}
	}
	f.reset()
	err := a.Move(ctx, "1003", "Doing")
	if err == nil || !strings.Contains(err.Error(), "not in project") {
		t.Fatalf("top-level task outside the project: %v", err)
	}
	for _, r := range f.requests() {
		if !strings.HasPrefix(r, "GET") {
			t.Fatalf("sent %s", r)
		}
	}
}

// A failure after the task exists is a PartialCreateError carrying the task, and the task is
// created once.
func TestCreatePartial(t *testing.T) {
	f := newFake(t)
	f.failOn = "POST /api/1.0/tasks/5001/addTag"
	it, err := newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "Signup", Labels: []string{"bug"}})
	var pc *hubsync.PartialCreateError
	if !errors.As(err, &pc) || pc.Item.ID != "5001" || it.ID != "5001" || len(it.Labels) != 0 {
		t.Fatalf("labels failed: %+v %v", it, err)
	}
	if got, ok := hubsync.CreatedItem(err); !ok || got.ID != "5001" {
		t.Fatalf("CreatedItem: %+v %v", got, ok)
	}
	n := 0
	for _, r := range f.requests() {
		if r == "POST /api/1.0/tasks" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d creates", n)
	}
	f.failOn = "GET /api/1.0/tasks/5002"
	it, err = newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "Other"})
	if !errors.As(err, &pc) || pc.Item.ID != "5002" || pc.Item.Title != "Other" || it.ID != "5002" {
		t.Fatalf("read-back failed: %+v %v", it, err)
	}
	// nothing created: a plain error
	f.failOn = "POST /api/1.0/tasks"
	if _, err = newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "x"}); err == nil || errors.As(err, &pc) {
		t.Fatalf("failed create: %v", err)
	}
}
