package store

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestUpsertDecisionMirrorsGateRecords(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	d := Decision{ID: "d1a2b3c4", CardID: c.ID, RunID: "r-x", Slug: "demo", Kind: KindPlanApproval,
		Text: "approve? reply to ana@example.com", Options: []string{"approve", "edit", "stop"}, Status: DecisionPending, AskedAt: "2026-10-21T10:00:00Z"}
	res, err := s.UpsertDecision(d)
	if err != nil || !res.Inserted || res.NowAnswered {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ := s.UpsertDecision(d); res.Inserted {
		t.Fatal("second mirror of the same pending record is a no-op")
	}
	if n, _ := s.PendingCount(c.ID); n != 1 {
		t.Fatalf("pending %d", n)
	}
	got, _ := s.Decision("d1a2b3c4")
	if strings.Contains(got.Text, "ana@") {
		t.Fatal("decision text must be masked")
	}
	answered := Decision{ID: "d1a2b3c4", CardID: c.ID, Kind: KindPlanApproval, Status: DecisionAnswered, Choice: "approve", Actor: "terminal", AnsweredAt: "2026-10-21T10:05:00Z"}
	res, _ = s.UpsertDecision(answered)
	if !res.NowAnswered {
		t.Fatal("answered record must flip the row")
	}
	got, _ = s.Decision("d1a2b3c4")
	if got.Status != DecisionAnswered || got.Actor != "terminal" || got.Text == "" {
		t.Fatalf("answered row %+v (the text of the pending record is kept)", got)
	}
	again := answered
	again.Choice = "stop"
	s.UpsertDecision(again)
	if got, _ := s.Decision("d1a2b3c4"); got.Choice != "approve" {
		t.Fatal("an answer is final")
	}
	acts, _ := s.Activity(10)
	verbs := map[string]int{}
	for _, a := range acts {
		verbs[a.Verb]++
		if a.Verb == activity.VerbDecisionAnswer && a.Data["actor_kind"] != "terminal" {
			t.Fatalf("actor kind %v", a.Data)
		}
	}
	if verbs[activity.VerbDecisionAsk] != 1 || verbs[activity.VerbDecisionAnswer] != 1 {
		t.Fatalf("verbs %v", verbs)
	}
}

func TestHubOwnedDecisions(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	d, err := s.AskHub(c.ID, "r-x", "demo", "The run was refused Bash(npm test). Allow it for one round?", []string{"allow-once", "stop"}, map[string]any{"tool": "Bash(npm test)"})
	if err != nil || !ValidID("d", d.ID) || !d.HubOwned() || d.Kind != KindPermission {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.AnswerHub(d.ID, "approve", "", "tester"); err == nil {
		t.Fatal("a choice outside the options is refused")
	}
	got, err := s.AnswerHub(d.ID, "allow-once", "", "tester")
	if err != nil || got.Status != DecisionAnswered || got.Data["tool"] != "Bash(npm test)" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.AnswerHub(d.ID, "stop", "", "tester"); err == nil {
		t.Fatal("answering twice is refused")
	}
	if _, err := s.AnswerHub("d1a2b3c4", "x", "", "tester"); err == nil {
		t.Fatal("a gate decision cannot be answered as a hub decision")
	}
	pend, _ := s.PendingDecisions()
	if len(pend) != 0 {
		t.Fatalf("pending %+v", pend)
	}
}

func TestActorKind(t *testing.T) {
	for in, want := range map[string]string{"hub:tester": "hub", "terminal": "terminal", "tester": "other", "": "other"} {
		if got := ActorKind(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}

func TestDecisionDataAndChoiceMasked(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	tok := "ghp_" + strings.Repeat("a1B2", 9) // tq:allow-secret
	d, err := s.AskHub(c.ID, "r-x", "demo", "allow?", []string{"allow-once", "stop"}, map[string]any{"tool": "Bash(curl -H " + tok + ")"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jsonText(d.Data), tok) {
		t.Fatalf("data not masked: %v", d.Data)
	}
	got, err := s.AnswerHub(d.ID, "allow-once", "note "+tok, "tester")
	if err != nil || strings.Contains(got.Note, tok) {
		t.Fatalf("note not masked: %+v %v", got, err)
	}
}

func TestMirroredAnswerMasked(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	tok := "ghp_" + strings.Repeat("a1B2", 9) // tq:allow-secret
	s.UpsertDecision(Decision{ID: "d9", CardID: c.ID, Kind: KindQuestion, Text: "q", Status: DecisionPending})
	s.UpsertDecision(Decision{ID: "d9", CardID: c.ID, Kind: KindQuestion, Status: DecisionAnswered, Choice: "x " + tok, Note: "n " + tok, Actor: "terminal"})
	got, _ := s.Decision("d9")
	if strings.Contains(got.Choice, tok) || strings.Contains(got.Note, tok) {
		t.Fatalf("not masked: %+v", got)
	}
}
