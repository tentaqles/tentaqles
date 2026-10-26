package web

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// TestAPIRefusesInvalidBudgetFrontMatter: POST /api/cards and POST /api/cards/{id} answer 400
// bad_request for a body whose budget front matter a run could not read, naming the bad value.
func TestAPIRefusesInvalidBudgetFrontMatter(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": store.NewCard{Title: "x", Body: "---\nbudget: rounds=0\n---\nDo it."}}, nil)
	if code != http.StatusBadRequest || m["code"] != "bad_request" || !strings.Contains(raw, "rounds") {
		t.Fatalf("add: %d %s", code, raw)
	}
	code, _, raw = apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": store.NewCard{Title: "x"}, "budget": map[string]any{"usd": 15}}, nil)
	if code != http.StatusBadRequest || !strings.Contains(raw, "positive") {
		t.Fatalf("partial budget argument: %d %s", code, raw)
	}
	svc := e.svcs["acme"]
	if cs, _ := svc.Store().ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
	c, err := svc.Add(context.Background(), store.NewCard{Title: "y", Body: "---\nbudget: usd=15\n---\nkeep"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, m, raw = apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"?ws=acme", map[string]any{"body": "---\nbudget: usd=abc\n---\nnew"}, nil)
	if code != http.StatusBadRequest || m["code"] != "bad_request" || !strings.Contains(raw, "usd") {
		t.Fatalf("update: %d %s", code, raw)
	}
	if got, _ := svc.Store().Card(c.ID); got.Body != c.Body {
		t.Fatalf("the body is unchanged: %q", got.Body)
	}
	if code, _, raw := apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": store.NewCard{Title: "z", Body: "---\nbudget: usd=15,rounds=8\n---\nok"}}, nil); code != 200 {
		t.Fatalf("a valid budget is accepted: %d %s", code, raw)
	}
}

// TestFormAddShowsInvalidBudgetLikeOtherAddErrors: the new-card form gets the same error page as
// any other refused add (an empty title: 400; an unknown lane: the same page as a 404), with the
// message, and no card.
func TestFormAddShowsInvalidBudgetLikeOtherAddErrors(t *testing.T) {
	e := newEnv(t, "acme")
	page := e.server(Options{})
	titleCode, titleBody, _ := postForm(t, page, "/cards?ws=acme", map[string][]string{"title": {" "}}, origin(page))
	laneCode, laneBody, _ := postForm(t, page, "/cards?ws=acme", map[string][]string{"title": {"x"}, "lane": {"nope"}}, origin(page))
	code, body, _ := postForm(t, page, "/cards?ws=acme", map[string][]string{"title": {"x"}, "body": {"---\nbudget: dollars=5\n---\nDo it."}}, origin(page))
	if code != http.StatusBadRequest || titleCode != http.StatusBadRequest || laneCode != http.StatusNotFound {
		t.Fatalf("budget %d, empty title %d, unknown lane %d", code, titleCode, laneCode)
	}
	if !strings.Contains(html.UnescapeString(body), `budget key "dollars"`) {
		t.Fatalf("the page names the bad key: %s", body)
	}
	if !strings.Contains(titleBody, "a card needs a title") || !strings.Contains(titleBody, "<title>Bad Request") != !strings.Contains(body, "<title>Bad Request") {
		t.Fatalf("same page as the empty-title error:\n%s\n---\n%s", titleBody, body)
	}
	if !strings.Contains(laneBody, `class="flash"`) != !strings.Contains(body, `class="flash"`) {
		t.Fatalf("same message page as the unknown-lane error:\n%s\n---\n%s", laneBody, body)
	}
	if cs, _ := e.svcs["acme"].Store().ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
}
