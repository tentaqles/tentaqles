package store

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestAddCardDefaultsAndMasking(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, err := s.AddCard(NewCard{Title: "Export invoices", Body: "ask ana@example.com", Tags: []string{"agent-ready", "agent-ready"}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID("c", c.ID) || c.Lane != "backlog" || c.Kind != "feature" || len(c.Tags) != 1 || !c.HasTag(TagAgentReady) {
		t.Fatalf("card %+v", c)
	}
	got, _ := s.Card(c.ID)
	if strings.Contains(got.Body, "ana@example.com") {
		t.Fatalf("body stored unmasked: %q", got.Body)
	}
	acts, err := s.Activity(10)
	if err != nil || len(acts) != 1 || acts[0].Verb != activity.VerbCardCreate || acts[0].Data["lane"] != "backlog" {
		t.Fatalf("activity %+v %v", acts, err)
	}
	raw, _ := os.ReadFile(ActivityPath("acme"))
	if !strings.Contains(string(raw), `"verb":"card.create"`) || strings.Contains(string(raw), "ana@") {
		t.Fatalf("activity.jsonl: %s", raw)
	}
}

func TestAddCardValidation(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	cases := []NewCard{
		{Title: ""},
		{Title: "x", Kind: "epic"},
		{Title: "x", Lane: "nowhere"},
		{Title: "x", ParentID: "c-abcdefghijkl"},
		{Title: "x", ProjectDir: "../outside"},
		{Title: "x", ProjectDir: `C:\abs`},
		{Title: "x", ProjectDir: "/abs"},
	}
	for _, nc := range cases {
		if _, err := s.AddCard(nc, "tester"); err == nil {
			t.Fatalf("%+v accepted", nc)
		}
	}
	c, err := s.AddCard(NewCard{Title: "ok", ProjectDir: `apps\billing`}, "tester")
	if err != nil || c.ProjectDir != "apps/billing" {
		t.Fatalf("project dir %q %v", c.ProjectDir, err)
	}
}

func TestListCardsFilters(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a", Kind: "fix", Tags: []string{"agent-ready"}}, "tester")
	s.AddCard(NewCard{Title: "b", Kind: "feature", Lane: "todo"}, "tester")
	byTag, _ := s.ListCards(CardFilter{Tag: "agent-ready"})
	byKind, _ := s.ListCards(CardFilter{Kind: "feature"})
	byLane, _ := s.ListCards(CardFilter{Lane: "TODO"})
	all, _ := s.ListCards(CardFilter{})
	if len(byTag) != 1 || byTag[0].ID != a.ID || len(byKind) != 1 || len(byLane) != 1 || len(all) != 2 {
		t.Fatalf("tag %d kind %d lane %d all %d", len(byTag), len(byKind), len(byLane), len(all))
	}
	if all[0].Lane != "backlog" {
		t.Fatalf("cards are ordered by lane position first: %+v", all)
	}
}

func TestMoveCardLaneKinds(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	m, err := s.MoveCard(c.ID, "In Progress", "tester")
	if err != nil || !m.EnteredActive() || m.LeftActive() || m.Card.Lane != "in progress" {
		t.Fatalf("move %+v %v", m, err)
	}
	m, _ = s.MoveCard(c.ID, "done", "tester")
	if !m.LeftActive() || m.Card.DoneAt == "" {
		t.Fatalf("done move %+v", m)
	}
	m, _ = s.MoveCard(c.ID, "todo", "tester")
	if m.Card.DoneAt != "" {
		t.Fatal("leaving done clears done_at")
	}
	if _, err := s.MoveCard(c.ID, "nowhere", "tester"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown lane: %v", err)
	}
}

func TestParentCompletionMovesParentToReview(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	p, _ := s.AddCard(NewCard{Title: "parent", Kind: "project"}, "tester")
	c1, _ := s.AddCard(NewCard{Title: "one", ParentID: p.ID}, "tester")
	c2, _ := s.AddCard(NewCard{Title: "two", ParentID: p.ID}, "tester")
	s.MoveCard(c1.ID, "done", "tester")
	if got, _ := s.Card(p.ID); got.Lane != "backlog" {
		t.Fatalf("parent moved too early: %s", got.Lane)
	}
	s.MoveCard(c2.ID, "done", "tester")
	got, _ := s.Card(p.ID)
	if got.Lane != "review" {
		t.Fatalf("parent lane %q, want review (never done)", got.Lane)
	}
	cs, _ := s.Comments(p.ID)
	if len(cs) != 1 || cs[0].AuthorKind != AuthorSystem || !strings.Contains(cs[0].Body, "2 child cards are done") {
		t.Fatalf("comments %+v", cs)
	}
}

func TestCommentsAndAttachments(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	if _, err := s.AddComment(c.ID, "robot", "x", "hi", ""); err == nil {
		t.Fatal("unknown author kind accepted")
	}
	if _, err := s.AddComment(c.ID, AuthorAgent, "agent", "   ", ""); err == nil {
		t.Fatal("empty comment accepted")
	}
	if _, err := s.AddComment(c.ID, AuthorAgent, "agent", "mail bob@example.com", "r-abcdefghijkl"); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.Comments(c.ID)
	if len(cs) != 1 || strings.Contains(cs[0].Body, "bob@") || cs[0].RunID != "r-abcdefghijkl" {
		t.Fatalf("comments %+v", cs)
	}
	if _, err := s.AddAttachment(c.ID, "docs/prd.md", "PRD"); err != nil {
		t.Fatal(err)
	}
	as, _ := s.Attachments(c.ID)
	if len(as) != 1 || as[0].Path != "docs/prd.md" {
		t.Fatalf("attachments %+v", as)
	}
}

func TestCardBudgetFrontMatter(t *testing.T) {
	body := WithBudgetFrontMatter("Do the thing.", Budget{Tokens: 1000, USD: 2, Minutes: 30, Rounds: 3})
	if !strings.HasPrefix(body, "---\nbudget: tokens=1000,usd=2,minutes=30,rounds=3\n---\n") || StripFrontMatter(body) != "Do the thing." {
		t.Fatalf("body %q", body)
	}
	b, err := CardBudget(Card{Body: body}, DefaultBudget)
	if err != nil || b.Rounds != 3 || b.USD != 2 {
		t.Fatalf("%+v %v", b, err)
	}
	if b, _ := CardBudget(Card{Body: "no front matter"}, DefaultBudget); b != DefaultBudget {
		t.Fatal("no front matter means the default budget")
	}
	if _, err := CardBudget(Card{Body: "---\nbudget: dollars=3\n---\nx"}, DefaultBudget); err == nil {
		t.Fatal("bad budget accepted")
	}
}

func TestLogMasksData(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.Log(activity.Entry{Actor: "hub", Verb: activity.VerbCardUpdate, Data: map[string]any{"fields": []any{"ana@example.com"}}}); err != nil {
		t.Fatal(err)
	}
	acts, _ := s.Activity(1)
	if strings.Contains(acts[0].Data["fields"].([]any)[0].(string), "ana@") {
		t.Fatalf("data not masked: %+v", acts[0].Data)
	}
}

func TestMoveCardSurvivesParentCompletionFailure(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	p, _ := s.AddCard(NewCard{Title: "parent", Kind: "project"}, "tester")
	c, _ := s.AddCard(NewCard{Title: "only", ParentID: p.ID}, "tester")
	if _, err := s.DB.Exec(`DELETE FROM lanes WHERE kind = ?`, LaneReview); err != nil {
		t.Fatal(err)
	}
	m, err := s.MoveCard(c.ID, "done", "tester")
	if err != nil || m.From.Name != "backlog" || m.To.Name != "done" || m.Card.Lane != "done" {
		t.Fatalf("move %+v %v", m, err)
	}
}

func TestLockedParentIsNotMovedOnCompletion(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	p, _ := s.AddCard(NewCard{Title: "parent", Kind: "project"}, "tester")
	c, _ := s.AddCard(NewCard{Title: "only", ParentID: p.ID}, "tester")
	if ok, _ := s.Checkout(p.ID, "r-abcdefghijkl"); !ok {
		t.Fatal("checkout")
	}
	if _, err := s.MoveCard(c.ID, "done", "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Card(p.ID)
	cs, _ := s.Comments(p.ID)
	if got.Lane != "backlog" || len(cs) != 1 || cs[0].AuthorKind != AuthorSystem || !strings.Contains(cs[0].Body, "when its run ends") {
		t.Fatalf("parent %q comments %+v", got.Lane, cs)
	}
}

func TestAddCardSurvivesActivityAppendFailure(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	os.Remove(ActivityPath("acme"))
	if err := os.MkdirAll(ActivityPath("acme"), 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := s.AddCard(NewCard{Title: "still stored"}, "tester")
	if err != nil || c.ID == "" {
		t.Fatalf("card %+v %v", c, err)
	}
	if got, err := s.Card(c.ID); err != nil || got.Title != "still stored" {
		t.Fatalf("db row: %+v %v", got, err)
	}
	if acts, _ := s.Activity(5); len(acts) != 1 {
		t.Fatalf("activity row must stay: %+v", acts)
	}
}

// TestTagsAndAttachmentsMasked: tags and attachment labels and paths are stored text too.
func TestTagsAndAttachmentsMasked(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	tok := "ghp_" + strings.Repeat("a1B2", 9) // tq:allow-secret
	c, err := s.AddCard(NewCard{Title: "a", Tags: []string{TagAgentReady, "key-" + tok}}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasTag(TagAgentReady) || strings.Contains(strings.Join(c.Tags, ","), tok) {
		t.Fatalf("tags %v", c.Tags)
	}
	tags := []string{"upd-" + tok}
	u, err := s.UpdateCard(c.ID, CardUpdate{Tags: &tags}, "tester")
	if err != nil || strings.Contains(strings.Join(u.Tags, ","), tok) {
		t.Fatalf("updated tags %v %v", u.Tags, err)
	}
	if _, err := s.AddAttachment(c.ID, "notes/"+tok+".txt", "label "+tok); err != nil {
		t.Fatal(err)
	}
	as, _ := s.Attachments(c.ID)
	if len(as) != 1 || strings.Contains(as[0].Label, tok) || strings.Contains(as[0].Path, tok) {
		t.Fatalf("attachment %+v", as)
	}
}
