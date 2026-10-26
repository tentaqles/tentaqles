package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

var badBudgets = []struct{ fm, want string }{
	{"budget: rounds=0", "rounds"},
	{"budget: rounds=1.5", "rounds"},
	{"budget: usd=abc", "usd"},
	{"budget: usd=-3", "usd"},
	{"budget: usd=NaN", "usd"},
	{"budget: minutes=Inf", "minutes"},
	{"budget: tokens=0", "tokens"},
	{"budget: dollars=5", "dollars"},
	{"budget: usd", "usd"},
}

// TestAddRejectsInvalidBudgetFrontMatter: a card whose budget front matter would not parse at
// run time is refused at create, naming the bad key, and no card is stored.
func TestAddRejectsInvalidBudgetFrontMatter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, b := range badBudgets {
		_, err := h.svc.Add(ctx, store.NewCard{Title: "x", Body: "---\n" + b.fm + "\n---\nDo it."}, nil)
		if err == nil || errors.Is(err, ErrHumanOnly) || !strings.Contains(err.Error(), b.want) {
			t.Fatalf("%q: %v", b.fm, err)
		}
	}
	if cs, _ := h.st.ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
	for _, body := range []string{"no front matter", "---\nnote: hi\n---\nx", "---\nbudget: usd=15,rounds=8\n---\nx",
		"---\nbudget: tokens=4_000_000,minutes=30.5\n---\n", "---\nbudget:\n---\nx"} {
		if _, err := h.svc.Add(ctx, store.NewCard{Title: "ok", Body: body}, nil); err != nil {
			t.Fatalf("%q is valid: %v", body, err)
		}
	}
}

// TestAddRejectsInvalidBudgetArgument: an explicit budget must hold four positive limits.
func TestAddRejectsInvalidBudgetArgument(t *testing.T) {
	h := newHarness(t)
	bad := store.DefaultBudget
	bad.Rounds = 0
	if _, err := h.svc.Add(context.Background(), store.NewCard{Title: "x"}, &bad); err == nil || !strings.Contains(err.Error(), "rounds") {
		t.Fatalf("%v", err)
	}
	if cs, _ := h.st.ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
	good := store.Budget{Tokens: 1000, USD: 2, Minutes: 3, Rounds: 4}
	if _, err := h.svc.Add(context.Background(), store.NewCard{Title: "x"}, &good); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateCardRejectsInvalidBudgetFrontMatter: a body edit with an invalid budget is refused and
// the card stays as it was; other fields in the same update are not applied either.
func TestUpdateCardRejectsInvalidBudgetFrontMatter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	c, err := h.svc.Add(ctx, store.NewCard{Title: "x", Body: "---\nbudget: rounds=3\n---\nkeep"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range badBudgets {
		_, err := h.svc.UpdateCard(ctx, c.ID, store.CardUpdate{Title: strp("renamed"), Body: strp("---\n" + b.fm + "\n---\nnew")})
		if err == nil || !strings.Contains(err.Error(), b.want) {
			t.Fatalf("%q: %v", b.fm, err)
		}
	}
	if got, _ := h.st.Card(c.ID); got.Body != c.Body || got.Title != "x" {
		t.Fatalf("the card is unchanged: %+v", got)
	}
	if _, err := h.svc.UpdateCard(ctx, c.ID, store.CardUpdate{Body: strp("---\nbudget: usd=15,rounds=8\n---\nnew")}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UpdateCard(ctx, c.ID, store.CardUpdate{Body: strp("plain")}); err != nil {
		t.Fatal(err)
	}
}
