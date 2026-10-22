package web

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestSplitTags(t *testing.T) {
	if got := splitTags(" needs-spec, security ,,agent-ready "); !reflect.DeepEqual(got, []string{"needs-spec", "security", "agent-ready"}) {
		t.Fatalf("%q", got)
	}
	if got := splitTags(""); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

func TestNewCardDialogOnEveryPage(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	project, _ := svc.Add(context.Background(), store.NewCard{Title: "<b>Release 2</b>", Kind: "project"}, nil)
	feature, _ := svc.Add(context.Background(), store.NewCard{Title: "Export", Kind: "feature"}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/?ws=acme", "/waiting?ws=acme", "/cards/" + feature.ID + "?ws=acme", "/activity?ws=acme"} {
		_, body, _ := get(t, ts, path, nil)
		assertCSPSafe(t, path, body)
		body = between(body, `<div class="scrim" id="new-card"`, "</form>")
		for _, want := range []string{
			`role="dialog" aria-modal="true" aria-labelledby="nc-heading">`,
			`<form class="dlg" method="post" action="/cards?ws=acme" id="nc-form">`,
			`name="title" required`, `name="tags"`, `name="parent"`, `name="project"`,
			`<option value="` + project.ID + `">&lt;b&gt;Release 2&lt;/b&gt;</option>`,
			`<option value="in progress">In progress (starts a run if agent-ready)</option>`,
			`name="agent_ready" value="1" checked`, `<option>hotfix</option>`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the dialog lacks %q", path, want)
			}
		}
		if strings.Contains(body, `<option value="`+feature.ID+`">`) || strings.Contains(body, `<option value="review">`) || strings.Contains(body, `<option value="done">`) {
			t.Errorf("%s: parents are project cards only; review and done are not offered", path)
		}
	}
}

func TestFormAddAcceptsTagsParentAndProject(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	project, _ := svc.Add(context.Background(), store.NewCard{Title: "Release 2", Kind: "project"}, nil)
	ts := e.server(Options{})
	form := url.Values{"title": {"Export invoices"}, "kind": {"feature"}, "lane": {"todo"}, "project": {"apps/billing"},
		"parent": {project.ID}, "tags": {"needs-spec, security"}, "agent_ready": {"1"}}
	code, _, hdr := postForm(t, ts, "/cards?ws=acme", form, origin(ts))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "flash=added") {
		t.Fatalf("%d %v", code, hdr)
	}
	cs, _ := svc.Store().ListCards(store.CardFilter{Kind: "feature"})
	if len(cs) != 1 {
		t.Fatalf("%+v", cs)
	}
	c := cs[0]
	if c.ParentID != project.ID || c.ProjectDir != "apps/billing" || c.Lane != "todo" || !reflect.DeepEqual(c.Tags, []string{"needs-spec", "security", store.TagAgentReady}) {
		t.Fatalf("%+v", c)
	}
}

func TestFormAddRejectsABadParent(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	for _, parent := range []string{"../etc", "c-AAAA", "c-aaaaaaaaaaaa"} {
		code, _, hdr := postForm(t, ts, "/cards?ws=acme", url.Values{"title": {"x"}, "parent": {parent}}, origin(ts))
		if code != http.StatusNotFound || hdr.Get("Location") != "" {
			t.Fatalf("%q: %d %v", parent, code, hdr)
		}
	}
	if cs, _ := e.svcs["acme"].Store().ListCards(store.CardFilter{}); len(cs) != 0 {
		t.Fatalf("no card is stored: %+v", cs)
	}
}

func TestAPIAddAcceptsTagsParentAndProject(t *testing.T) {
	e := newEnv(t, "acme")
	project, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Release 2", Kind: "project"}, nil)
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/cards?ws=acme", map[string]any{"card": map[string]any{
		"title": "Export", "tags": []string{"security"}, "parent_id": project.ID, "project_dir": "apps/billing", "lane": "todo"}}, nil)
	if code != 200 || m["parent_id"] != project.ID || m["project_dir"] != "apps/billing" || !strings.Contains(raw, `"tags":["security"]`) {
		t.Fatalf("%d %s", code, raw)
	}
}

func TestAgentFormAddKeepsTheLaneRestriction(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	hdr := origin(ts)
	hdr[HeaderAgent] = "1"
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", url.Values{"title": {"x"}, "lane": {"in progress"}, "agent_ready": {"1"}}, hdr); code != http.StatusForbidden {
		t.Fatalf("an agent cannot add into an active lane: %d", code)
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", url.Values{"title": {"y"}, "lane": {"todo"}, "tags": {"needs-spec"}}, hdr); code != http.StatusSeeOther {
		t.Fatalf("an agent may add into todo: %d", code)
	}
}
