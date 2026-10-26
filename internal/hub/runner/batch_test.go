package runner

import (
	"context"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestPRLinksMatchesPRLink(t *testing.T) {
	h := newHarness(t)
	a, b, none := h.card("feature"), h.card("feature"), h.card("feature")
	h.st.AddComment(a.ID, store.AuthorAgent, "agent", "PR https://example.test/o/r/pull/1", "")
	h.st.AddComment(a.ID, store.AuthorAgent, "agent", "PR https://example.test/o/r/pull/7 opened", "")
	h.st.AddComment(a.ID, store.AuthorHuman, "tester", "looks good", "")
	// The newest comment that says /pull/ has no URL: the older one with a URL still wins, as in PRLink.
	h.st.AddComment(b.ID, store.AuthorAgent, "agent", "PR http://example.test/o/r/pull/3", "")
	h.st.AddComment(b.ID, store.AuthorAgent, "agent", "see /pull/ later", "")
	h.st.AddComment(none.ID, store.AuthorAgent, "agent", "no link", "")
	got := PRLinks(h.st, []string{a.ID, b.ID, none.ID, "c-unknown"})
	want := map[string]string{a.ID: "https://example.test/o/r/pull/7", b.ID: "http://example.test/o/r/pull/3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PRLinks %v, want %v", got, want)
	}
	for _, c := range []store.Card{a, b, none} {
		if got[c.ID] != PRLink(h.st, c.ID) {
			t.Errorf("card %s: batch %q, single %q", c.ID, got[c.ID], PRLink(h.st, c.ID))
		}
	}
	if m := PRLinks(h.st, nil); len(m) != 0 {
		t.Fatalf("empty ids: %v", m)
	}
}

func TestListMatchesPerCardView(t *testing.T) {
	h := newHarness(t)
	a, b, idle := h.card("feature"), h.card("fix"), h.card("chore")
	done, _ := h.st.CreateRun(store.Run{CardID: a.ID, Adapter: store.AdapterClaude, OnRamp: store.OnRampFeature})
	done.Status, done.CostUSD = store.StatusFinished, 1.25
	h.st.SaveRun(done)
	act, _ := h.st.CreateRun(store.Run{CardID: a.ID, Adapter: store.AdapterClaude, OnRamp: store.OnRampFeature})
	act.Status, act.CostUSD = store.StatusRunning, 0.5
	h.st.SaveRun(act)
	w, _ := h.st.CreateRun(store.Run{CardID: b.ID, Adapter: store.AdapterClaude, OnRamp: store.OnRampFeature})
	w.Status = store.StatusWaiting
	h.st.SaveRun(w)
	h.st.UpsertDecision(store.Decision{ID: "d1", CardID: b.ID, Kind: store.KindQuestion})
	got, err := h.svc.List(context.Background(), store.CardFilter{}, false)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	for _, v := range got {
		one, err := h.svc.view(v.Card)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, one) {
			t.Errorf("card %s: List %+v, view %+v", v.ID, v, one)
		}
	}
	byID := map[string]CardView{}
	for _, v := range got {
		byID[v.ID] = v
	}
	if v := byID[a.ID]; v.CostUSD != 1.75 || v.RunID != act.ID || !v.Running {
		t.Errorf("card a %+v", v)
	}
	if v := byID[b.ID]; v.Waiting != 1 || v.RunStatus != store.StatusWaiting || v.Running {
		t.Errorf("card b %+v", v)
	}
	if v := byID[idle.ID]; v.CostUSD != 0 || v.RunID != "" || v.Waiting != 0 {
		t.Errorf("idle card %+v", v)
	}
	waiting, _ := h.svc.List(context.Background(), store.CardFilter{}, true)
	if len(waiting) != 1 || waiting[0].ID != b.ID {
		t.Errorf("waiting only %+v", waiting)
	}
}
