package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/setup"
)

func fakeGuardView(t *testing.T, v setup.GuardView, err error) {
	t.Helper()
	prev := guardViewFor
	guardViewFor = func(ws string) (setup.GuardView, error) {
		if ws != "acme" {
			t.Errorf("view asked for %q", ws)
		}
		return v, err
	}
	t.Cleanup(func() { guardViewFor = prev })
}

func relaxedView() setup.GuardView {
	v := setup.GuardView{Workspace: "acme", Mode: "relaxed", Label: "relaxed", ModeSource: "acme", Ceiling: "relaxed", Granted: true, Looser: true, Confirms: 3, Blocks: 1,
		Locked:    []string{"identity", "secrets", "self-protection", "privacy"},
		Overrides: []setup.GuardOverride{{ID: "tq/iac-apply", Value: "rule", Source: "acme"}}}
	for _, c := range []struct {
		n, v   string
		locked bool
	}{{"identity", "rule", true}, {"privacy", "rule", true}, {"git-history", "ask", false}, {"prod-changes", "skip", false}} {
		v.Categories = append(v.Categories, setup.GuardCategory{Name: c.n, Value: c.v, Source: "acme", Locked: c.locked})
	}
	return v
}

func TestGuardPageIsReadOnly(t *testing.T) {
	e := newEnv(t, "acme")
	fakeGuardView(t, relaxedView(), nil)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/guard?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{"Guard mode", "relaxed", "git-history", "prod-changes", "privacy", "data-locked", "tq/iac-apply",
		"3 confirms and 1 block", "relaxed grant", "change it with <code class=\"m\">tq mode edit</code> or the desktop app"} {
		if !strings.Contains(body, want) {
			t.Errorf("guard page lacks %q", want)
		}
	}
	if !strings.Contains(body, `href="/guard?ws=acme" class="on" aria-current="page"`) {
		t.Error("the nav links the guard page and marks it current")
	}
	main := body[strings.Index(body, `<div id="main">`):strings.Index(body, "</main>")]
	if strings.Contains(main, "<form") || strings.Contains(main, "<input") || strings.Contains(main, "<button") {
		t.Fatal("the guard page has no form: web forms are scriptable from an agent's curl")
	}
	if c, _, _ := postForm(t, ts, "/guard?ws=acme", url.Values{"mode": {"relaxed"}}, origin(ts)); c == http.StatusOK || c == http.StatusSeeOther {
		t.Fatalf("POST /guard must not exist: %d", c)
	}
}

func TestGuardPageNeedsGrantBanner(t *testing.T) {
	e := newEnv(t, "acme")
	v := setup.GuardView{Workspace: "acme", Mode: "default", Label: "default", Requested: "relaxed", Ceiling: "default", NeedsGrant: true, Proposal: "relaxed",
		Locked: []string{"identity", "secrets", "self-protection", "privacy"}}
	fakeGuardView(t, v, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/guard?ws=acme", nil)
	if !strings.Contains(body, "tq allow --mode acme relaxed") || !strings.Contains(body, "relaxed requested") {
		t.Fatalf("banner missing:\n%s", body)
	}
}

func TestGuardPageShowsReadError(t *testing.T) {
	e := newEnv(t, "acme")
	fakeGuardView(t, setup.GuardView{}, errors.New("manifest changed"))
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/guard?ws=acme", nil)
	if code != 200 || !strings.Contains(body, "The guard mode could not be read: manifest changed") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestGuardPageFragment(t *testing.T) {
	e := newEnv(t, "acme")
	fakeGuardView(t, relaxedView(), nil)
	ts := e.server(Options{})
	_, frag, _ := get(t, ts, "/guard?ws=acme&fragment=main", nil)
	if strings.Contains(frag, "<html") || !strings.Contains(frag, "git-history") {
		t.Fatal("fragment=main renders the main section only")
	}
}
