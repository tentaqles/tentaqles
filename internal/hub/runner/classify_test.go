package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func loadState(t *testing.T, name string) *gate.State {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "states", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var st gate.State
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	st.Raw = raw
	return &st
}

func absPath(t *testing.T, parts ...string) string {
	p, err := filepath.Abs(filepath.Join(append([]string{t.TempDir()}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClassifyTable(t *testing.T) {
	inProgress := loadState(t, "in_progress")
	cases := []struct {
		name string
		obs  Observation
		v    Verdict
		st   string
		why  string
	}{
		{"pending decisions wait", Observation{State: loadState(t, "waiting")}, VerdictWaiting, store.StatusWaiting, ""},
		{"finished feature with DoD ok", Observation{State: loadState(t, "finished_feature")}, VerdictFinished, store.StatusFinished, ""},
		{"finished plain build without DoD", Observation{State: loadState(t, "finished_build")}, VerdictFinished, store.StatusFinished, ""},
		{"finished feature with a failed DoD is no stop", Observation{State: loadState(t, "finished_feature_dod_failed")}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"halted beats pending", Observation{State: loadState(t, "halted")}, VerdictHalted, store.StatusFailed, "behavior failed 3x after split"},
		{"rolled back beats pending", Observation{State: loadState(t, "rolled_back")}, VerdictRolledBack, store.StatusFailed, "rolled back"},
		{"permission denial waits", Observation{ExitCode: 1, Subtype: "error_during_execution", PermissionTool: "Bash(npm test)", State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictPermission, store.StatusWaiting, "needs permission: Bash(npm test)"},
		{"max turns is a round boundary", Observation{Subtype: "error_max_turns", State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictMaxTurns, store.StatusQueued, ""},
		{"exit non-zero, state unchanged, first time: retry", Observation{ExitCode: 1, State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictRetry, store.StatusQueued, ""},
		{"exit non-zero, no state at all: retry", Observation{ExitCode: 2}, VerdictRetry, store.StatusQueued, ""},
		{"exit non-zero again: failed", Observation{ExitCode: 1, Retried: 1, State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictExitFailed, store.StatusFailed, "exit 1"},
		{"exit non-zero but state changed: no stop", Observation{ExitCode: 1, State: inProgress, PrevStateKey: "older"}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"exit zero, nothing to show: no stop", Observation{State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"exit zero before a slug, with a final message: a question", Observation{Subtype: "success", FinalText: "Which one?", PreSlug: true}, VerdictQuestion, store.StatusWaiting, "question"},
		{"process adapter exit 0 before a slug: a question", Observation{Subtype: "exit 0", FinalText: "Which one?", PreSlug: true}, VerdictQuestion, store.StatusWaiting, "question"},
		{"exit zero before a slug, no final message: no stop", Observation{Subtype: "success", PreSlug: true}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"exit zero after a slug: no stop", Observation{Subtype: "success", FinalText: "Which one?", State: inProgress, PrevStateKey: StateKey(inProgress)}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
		{"exit non-zero before a slug: retry, not a question", Observation{ExitCode: 1, Subtype: "error_during_execution", FinalText: "Which one?", PreSlug: true}, VerdictRetry, store.StatusQueued, ""},
		{"an error subtype with exit zero before a slug: no stop", Observation{Subtype: "error", FinalText: "Which one?", PreSlug: true}, VerdictNoStop, store.StatusFailed, "ended without a stop"},
	}
	for _, c := range cases {
		got := Classify(c.obs)
		if got.Verdict != c.v || got.Status != c.st || got.StopReason != c.why {
			t.Errorf("%s: got %+v, want %s/%s/%q", c.name, got, c.v, c.st, c.why)
		}
	}
}

func TestClassifyRelocate(t *testing.T) {
	wt := absPath(t, "billing build demo") // a path with spaces
	got := Classify(Observation{FinalText: "Worktree ready.\nTQ_HUB_RELOCATE " + wt + " demo\n", State: loadState(t, "waiting")})
	if got.Verdict != VerdictRelocate || got.Status != store.StatusRunning || got.Relocate.Worktree != wt || got.Relocate.Slug != "demo" {
		t.Fatalf("%+v", got)
	}
}

func TestClassifyRelocateLoop(t *testing.T) {
	wt := absPath(t, "wt")
	got := Classify(Observation{FinalText: "TQ_HUB_RELOCATE " + wt + " demo", Relocations: []string{"demo"}})
	if got.Verdict != VerdictRelocateLoop || got.Status != store.StatusFailed || got.StopReason != "relocate loop" {
		t.Fatalf("%+v", got)
	}
	other := Classify(Observation{FinalText: "TQ_HUB_RELOCATE " + wt + " other", Relocations: []string{"demo"}})
	if other.Verdict != VerdictRelocate {
		t.Fatalf("a different slug is a new relocation: %+v", other)
	}
}

func TestParseRelocateOnlyLastLine(t *testing.T) {
	a, b := absPath(t, "a"), absPath(t, "b")
	r, ok := ParseRelocate("TQ_HUB_RELOCATE " + a + " first\nsome words\nTQ_HUB_RELOCATE " + b + " second\n\n")
	if !ok || r.Worktree != b || r.Slug != "second" {
		t.Fatalf("%+v %v", r, ok)
	}
	if _, ok := ParseRelocate("TQ_HUB_RELOCATE " + a + " demo\nand then more text"); ok {
		t.Fatal("a relocate line that is not the last line does not count")
	}
	for _, bad := range []string{"TQ_HUB_RELOCATE relative/path demo", "TQ_HUB_RELOCATE " + a + " Bad_Slug", "TQ_HUB_RELOCATE " + a, "tq_hub_relocate " + a + " demo"} {
		if _, ok := ParseRelocate(bad); ok {
			t.Fatalf("%q accepted", bad)
		}
	}
	if StripRelocate("done\nTQ_HUB_RELOCATE "+a+" demo") != "done" {
		t.Fatal("StripRelocate")
	}
	if runtime.GOOS == "windows" {
		r, ok := ParseRelocate("TQ_HUB_RELOCATE /c/repos/acme-build-demo demo")
		if !ok || r.Worktree != `C:\repos\acme-build-demo` {
			t.Fatalf("Git Bash path: %+v %v", r, ok)
		}
	}
}

func TestStateKeyChangesWithProgress(t *testing.T) {
	a := loadState(t, "in_progress")
	b := loadState(t, "in_progress")
	b.Checkpoints[0].Status = "passed"
	if StateKey(a) == StateKey(b) || StateKey(a) != StateKey(loadState(t, "in_progress")) || StateKey(nil) != "" {
		t.Fatal("StateKey must be stable and change with a checkpoint status")
	}
}
