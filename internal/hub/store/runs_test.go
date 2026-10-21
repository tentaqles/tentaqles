package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newRunFor(t *testing.T, s *Store, c Card) Run {
	t.Helper()
	r, err := s.CreateRun(Run{CardID: c.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, ProjectDir: c.ProjectDir, Kind: c.Kind})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateAndSaveRun(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	r := newRunFor(t, s, c)
	if !ValidID("r", r.ID) || r.Status != StatusQueued || r.Budget != s.DefaultBudget() || r.Phase != PhaseStart || r.Workspace != "acme" {
		t.Fatalf("run %+v", r)
	}
	if !strings.HasSuffix(r.LogPath, r.ID+".log") {
		t.Fatalf("log path %q", r.LogPath)
	}
	r.Status = StatusRunning
	r.Usage = r.Usage.Add(Usage{Input: 10, Output: 5, CacheRead: 1000, CacheWrite: 2})
	r.RoundLog = append(r.RoundLog, RoundRecord{N: 1, CostUSD: 0.5, CostUnknown: true})
	r.Relocations = []string{"demo"}
	r.StopReason = "mail ana@example.com"
	if err := s.SaveRun(r); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Run(r.ID)
	if got.Status != StatusRunning || got.Usage.Tokens() != 17 || len(got.RoundLog) != 1 || !got.RoundLog[0].CostUnknown || got.Relocations[0] != "demo" {
		t.Fatalf("saved %+v", got)
	}
	if strings.Contains(got.StopReason, "ana@") {
		t.Fatal("stop reason must be masked")
	}
	if _, err := s.CreateRun(Run{CardID: c.ID, Adapter: "codex", OnRamp: OnRampFeature}); err == nil {
		t.Fatal("unknown adapter accepted")
	}
}

func TestExceeded(t *testing.T) {
	b := Budget{Tokens: 100, USD: 1, Minutes: 10, Rounds: 2}
	cases := map[string]Run{
		"":        {Budget: b, Rounds: 1, Usage: Usage{Input: 10}, CostUSD: 0.5, Seconds: 60},
		"rounds":  {Budget: b, Rounds: 2},
		"tokens":  {Budget: b, Usage: Usage{Input: 60, Output: 40}},
		"usd":     {Budget: b, CostUSD: 1},
		"minutes": {Budget: b, Seconds: 600},
	}
	for want, r := range cases {
		if got := r.Exceeded(); got != want {
			t.Fatalf("want %q got %q for %+v", want, got, r)
		}
	}
	if (Run{Budget: b, Usage: Usage{CacheRead: 10_000}}).Exceeded() != "" {
		t.Fatal("cache reads do not count against the token budget")
	}
}

func TestDueRunsFIFOAndNotBefore(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c1, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	c2, _ := s.AddCard(NewCard{Title: "b"}, "renato")
	now := time.Now().UTC()
	r1, _ := s.CreateRun(Run{CardID: c1.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, QueuedAt: now.Add(-2 * time.Minute).Format(time.RFC3339)})
	r2, _ := s.CreateRun(Run{CardID: c2.ID, Adapter: AdapterClaude, OnRamp: OnRampFeature, QueuedAt: now.Add(-1 * time.Minute).Format(time.RFC3339),
		NotBefore: now.Add(2 * time.Minute).Format(time.RFC3339)})
	due, err := s.DueRuns(now, 10)
	if err != nil || len(due) != 1 || due[0].ID != r1.ID {
		t.Fatalf("due %+v %v", due, err)
	}
	later, _ := s.DueRuns(now.Add(3*time.Minute), 10)
	if len(later) != 2 || later[0].ID != r1.ID || later[1].ID != r2.ID {
		t.Fatalf("FIFO %+v", later)
	}
	if act, ok, _ := s.ActiveRun(c1.ID); !ok || act.ID != r1.ID {
		t.Fatal("active run")
	}
}

func TestLocksReport(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "renato")
	r := newRunFor(t, s, c)
	s.Checkout(c.ID, r.ID)
	r.Status = StatusFailed
	s.SaveRun(r)
	c2, _ := s.AddCard(NewCard{Title: "b"}, "renato")
	s.Checkout(c2.ID, "r-gonegonegone")
	locks, err := s.Locks()
	if err != nil || len(locks) != 2 {
		t.Fatalf("locks %+v %v", locks, err)
	}
	got := map[string]string{}
	for _, l := range locks {
		got[l.CardID] = l.RunStatus
	}
	if got[c.ID] != StatusFailed || got[c2.ID] != "missing" {
		t.Fatalf("%v", got)
	}
}

func TestSaveRunUnknownIsNotFound(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.SaveRun(Run{ID: "r-nonenonenone"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
