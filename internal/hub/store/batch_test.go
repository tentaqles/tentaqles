package store

import (
	"reflect"
	"testing"
)

func TestRunsForCardsMatchesRunsForCard(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	b, _ := s.AddCard(NewCard{Title: "b"}, "tester")
	idle, _ := s.AddCard(NewCard{Title: "idle"}, "tester")
	a1, a2, a3 := newRunFor(t, s, a), newRunFor(t, s, a), newRunFor(t, s, a)
	b1 := newRunFor(t, s, b)
	// a2 is the newest by queued_at; a1 and a3 tie and fall back to rowid (a3 first).
	for id, at := range map[string]string{a1.ID: "2026-10-20T10:00:00Z", a2.ID: "2026-10-22T10:00:00Z", a3.ID: "2026-10-20T10:00:00Z", b1.ID: "2026-10-21T10:00:00Z"} {
		if _, err := s.DB.Exec(`UPDATE runs SET queued_at = ? WHERE id = ?`, at, id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.RunsForCards([]string{a.ID, b.ID, idle.ID, "c-unknown"})
	if err != nil {
		t.Fatal(err)
	}
	ids := func(rs []Run) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	if want := []string{a2.ID, a3.ID, a1.ID}; !reflect.DeepEqual(ids(got[a.ID]), want) {
		t.Fatalf("card a runs %v, want %v", ids(got[a.ID]), want)
	}
	for _, c := range []Card{a, b} {
		one, _ := s.RunsForCard(c.ID)
		if !reflect.DeepEqual(got[c.ID], one) {
			t.Errorf("card %s: batch %v differs from RunsForCard %v", c.Title, ids(got[c.ID]), ids(one))
		}
	}
	if _, ok := got[idle.ID]; ok {
		t.Error("a card without runs has no entry")
	}
	if _, ok := got["c-unknown"]; ok {
		t.Error("an unknown id has no entry")
	}
	if len(got) != 2 {
		t.Errorf("entries %d, want 2", len(got))
	}
	for _, empty := range [][]string{nil, {}} {
		m, err := s.RunsForCards(empty)
		if err != nil || len(m) != 0 {
			t.Fatalf("empty ids: %v %v", m, err)
		}
	}
}

func TestRunsForCardsManyIDs(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	r := newRunFor(t, s, c)
	ids := make([]string, 0, 1200)
	for i := 0; i < 1199; i++ {
		ids = append(ids, NewID("c"))
	}
	ids = append(ids, c.ID) // past the first chunk
	got, err := s.RunsForCards(ids)
	if err != nil || len(got[c.ID]) != 1 || got[c.ID][0].ID != r.ID {
		t.Fatalf("%v %v", got, err)
	}
}

func TestPendingCounts(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	b, _ := s.AddCard(NewCard{Title: "b"}, "tester")
	for _, d := range []Decision{
		{ID: "d1", CardID: a.ID, Kind: KindQuestion, Status: DecisionPending},
		{ID: "d2", CardID: a.ID, Kind: KindQuestion, Status: DecisionPending},
		{ID: "d3", CardID: b.ID, Kind: KindQuestion, Status: DecisionAnswered},
	} {
		if _, err := s.UpsertDecision(d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.PendingCounts([]string{a.ID, b.ID, "c-unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]int{a.ID: 2}) {
		t.Fatalf("counts %v", got)
	}
	for _, c := range []Card{a, b} {
		n, _ := s.PendingCount(c.ID)
		if got[c.ID] != n {
			t.Errorf("card %s: batch %d, single %d", c.Title, got[c.ID], n)
		}
	}
	if m, err := s.PendingCounts(nil); err != nil || len(m) != 0 {
		t.Fatalf("empty ids: %v %v", m, err)
	}
}

func TestCommentsContainingNewestFirst(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	b, _ := s.AddCard(NewCard{Title: "b"}, "tester")
	s.AddComment(a.ID, AuthorAgent, "agent", "PR https://example.test/o/r/pull/1", "")
	s.AddComment(a.ID, AuthorAgent, "agent", "no link here", "")
	s.AddComment(a.ID, AuthorAgent, "agent", "PR https://example.test/o/r/pull/2", "")
	s.AddComment(b.ID, AuthorAgent, "agent", "see /PULL/ upper case", "")
	got, err := s.CommentsContaining([]string{a.ID, b.ID, "c-unknown"}, "/pull/")
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, c := range got {
		bodies = append(bodies, c.Body)
	}
	want := []string{"PR https://example.test/o/r/pull/2", "PR https://example.test/o/r/pull/1"}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("bodies %q, want %q (newest first, case-sensitive)", bodies, want)
	}
	if m, err := s.CommentsContaining(nil, "/pull/"); err != nil || len(m) != 0 {
		t.Fatalf("empty ids: %v %v", m, err)
	}
}
