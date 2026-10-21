package activity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndReadSkipOldAndBadLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "acme", "activity.jsonl")
	if err := Append(p, Entry{TS: "2026-01-01T00:00:00Z", Workspace: "acme", Actor: "renato", Verb: VerbCardCreate}); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, Entry{Workspace: "acme", Actor: "renato", Verb: VerbCardMove, CardID: "c-abcdefghijkl", Data: map[string]any{"from": "backlog", "to": "review"}}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("not json\n")
	f.Close()
	got, err := Read(p, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Verb != VerbCardMove || got[0].TS == "" || got[0].Data["to"] != "review" {
		t.Fatalf("entries %+v", got)
	}
	if fi, err := os.Stat(p); err != nil || (fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Fatalf("activity.jsonl must be user-only: %v %v", fi.Mode(), err)
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "none.jsonl"), time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestSummarize(t *testing.T) {
	es := []Entry{
		{TS: "2026-10-20T10:00:00Z", Verb: VerbCardCreate, CardID: "c-a", Data: map[string]any{"lane": "backlog", "kind": "feature"}},
		{TS: "2026-10-20T10:00:00Z", Verb: VerbCardCreate, CardID: "c-b", Data: map[string]any{"lane": "backlog", "kind": "fix"}},
		{TS: "2026-10-20T10:01:00Z", Verb: VerbCardMove, CardID: "c-a", Data: map[string]any{"from": "backlog", "to": "in progress"}},
		{TS: "2026-10-20T10:02:00Z", Verb: VerbRunStart, CardID: "c-a", RunID: "r-1"},
		{TS: "2026-10-20T10:03:00Z", Verb: VerbDecisionAsk, CardID: "c-a", Data: map[string]any{"decision": "d1", "kind": "plan-approval"}},
		{TS: "2026-10-20T10:13:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d1", "kind": "plan-approval", "choice": "approve", "actor_kind": "hub"}},
		{TS: "2026-10-20T10:20:00Z", Verb: VerbDecisionAsk, CardID: "c-a", Data: map[string]any{"decision": "d2", "kind": "question"}},
		{TS: "2026-10-20T10:50:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d2", "kind": "question", "choice": "override", "actor_kind": "terminal"}},
		{TS: "2026-10-20T11:00:00Z", Verb: VerbDecisionAnswer, CardID: "c-a", Data: map[string]any{"decision": "d3", "kind": "result", "choice": "request-changes", "actor_kind": "hub"}},
		{TS: "2026-10-20T11:30:00Z", Verb: VerbRunEnd, CardID: "c-a", RunID: "r-1", Data: map[string]any{"status": "finished", "cost_usd": 1.5, "kind": "feature"}},
		{TS: "2026-10-20T11:31:00Z", Verb: VerbRunEnd, CardID: "c-b", RunID: "r-2", Data: map[string]any{"status": "over-budget", "cost_usd": 10.0, "kind": "fix"}},
		{TS: "2026-10-20T11:31:00Z", Verb: VerbBudgetStop, CardID: "c-b", RunID: "r-2", Data: map[string]any{"limit": "usd"}},
	}
	r := Summarize(es)
	if r.CardsByLane["in progress"] != 1 || r.CardsByLane["backlog"] != 1 {
		t.Fatalf("lanes %v", r.CardsByLane)
	}
	if r.RunsStarted != 1 || r.RunsEnded["finished"] != 1 || r.RunsEnded["over-budget"] != 1 {
		t.Fatalf("runs %d %v", r.RunsStarted, r.RunsEnded)
	}
	if r.CostByCard["c-a"] != 1.5 || r.CostByKind["fix"] != 10 {
		t.Fatalf("cost %v %v", r.CostByCard, r.CostByKind)
	}
	if r.DecisionsAnswered != 3 || r.MedianWaitMin != 20 { // waits 10 and 30 minutes; d3 was never asked in the window
		t.Fatalf("decisions %d median %v", r.DecisionsAnswered, r.MedianWaitMin)
	}
	if r.Overrides != 1 || r.RequestChanges != 1 || r.BudgetStops != 1 {
		t.Fatalf("counts %+v", r)
	}
}

func TestReadAllFiltersWorkspaces(t *testing.T) {
	hub := t.TempDir()
	for _, ws := range []string{"acme", "globex"} {
		if err := Append(filepath.Join(hub, ws, "activity.jsonl"), Entry{Workspace: ws, Verb: VerbRunStart, CardID: "c-x"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ReadAll(hub, time.Time{}, nil)
	if err != nil || all.RunsStarted != 2 || len(all.Workspaces) != 2 {
		t.Fatalf("all %+v %v", all, err)
	}
	one, _ := ReadAll(hub, time.Time{}, func(ws string) bool { return ws == "acme" })
	if one.RunsStarted != 1 || len(one.Workspaces) != 1 || one.Workspaces[0] != "acme" {
		t.Fatalf("filtered %+v", one)
	}
	none, err := ReadAll(filepath.Join(hub, "missing"), time.Time{}, nil)
	if err != nil || len(none.Workspaces) != 0 {
		t.Fatalf("missing hub dir %+v %v", none, err)
	}
}
