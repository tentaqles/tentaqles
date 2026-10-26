package web

import (
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestPillClassCoversTheStatusVocabulary(t *testing.T) {
	want := map[string]string{
		store.StatusQueued: "p-queue st-queued", store.StatusRunning: "p-run st-running", store.StatusWaiting: "p-wait st-waiting",
		store.StatusFinished: "p-done st-finished", store.StatusFailed: "p-fail st-failed", store.StatusCancelled: "p-fail st-cancelled",
		store.StatusOverBudget: "p-budget st-over-budget",
	}
	for _, st := range store.Statuses {
		if got := pillClass(st); got != want[st] {
			t.Errorf("pillClass(%q) = %q, want %q", st, got, want[st])
		}
	}
	if pillClass("bogus") != "p-queue st-bogus" {
		t.Error("an unknown status falls back to the neutral pill")
	}
	if statusLabel(store.StatusWaiting) != "waiting on you" || statusLabel(store.StatusOverBudget) != "over budget" || statusLabel("running") != "running" {
		t.Error("statusLabel")
	}
}

func TestViewHelpers(t *testing.T) {
	if laneTitle("in progress") != "In progress" || laneTitle("") != "" {
		t.Error("laneTitle")
	}
	if optionLabel("raise-and-resume") != "Raise and resume" || words("plan-approval") != "plan approval" {
		t.Error("optionLabel / words")
	}
	for _, c := range []struct {
		used, total float64
		want        int
	}{{3.1, 10, 31}, {0, 10, 0}, {5, 0, 0}, {20, 10, 100}, {0.01, 10, 1}} {
		if got := pct(c.used, c.total); got != c.want {
			t.Errorf("pct(%v, %v) = %d, want %d", c.used, c.total, got, c.want)
		}
	}
	if prNum("https://github.com/acme/billing/pull/9") != "PR #9" || prNum("https://example.com/x") != "" {
		t.Error("prNum")
	}
	for _, u := range []string{"https://github.com/acme/billing/pull/9/files", "https://github.com/acme/billing/pull/9/commits/abc", "https://github.com/acme/billing/pull/9/"} {
		if got := prNum(u); got != "PR #9" {
			t.Errorf("prNum(%q) = %q", u, got)
		}
	}
	if prNum("https://github.com/acme/billing/pull/") != "" || prNum("https://github.com/acme/billing/pulls") != "" {
		t.Error("prNum without a number")
	}
	if clip("one\ntwo", 80) != "one" || clip("ábcdef", 3) != "ábc…" {
		t.Error("clip keeps the first line and cuts on runes")
	}
	if availableCount([]WorkspaceInfo{{Available: true}, {}, {Available: true}}) != 2 {
		t.Error("availableCount")
	}
	if !planningLane(store.LaneBacklog) || !planningLane(store.LaneOther) || planningLane(store.LaneActive) {
		t.Error("planningLane")
	}
}

func TestClockAndAgo(t *testing.T) {
	fixed := time.Date(2026, 10, 22, 15, 0, 0, 0, time.Local)
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = time.Now })
	ts := func(d time.Duration) string { return fixed.Add(-d).UTC().Format(time.RFC3339) }
	if got := ago(ts(11 * time.Minute)); got != "11 min ago" {
		t.Errorf("ago 11 min: %q", got)
	}
	if got := ago(ts(3 * time.Hour)); got != "3 h ago" {
		t.Errorf("ago 3 h: %q", got)
	}
	if got := ago(ts(-time.Minute)); got != "just now" {
		t.Errorf("a time in the future reads as just now: %q", got)
	}
	if got := clock(ts(time.Hour)); got != "14:00" {
		t.Errorf("clock today: %q", got)
	}
	if got := clock(ts(26 * time.Hour)); got != "Oct 21 13:00" {
		t.Errorf("clock yesterday: %q", got)
	}
	if clock("not a time") != "not a time" || ago("") != "" {
		t.Error("unparseable times are shown as they are")
	}
}
