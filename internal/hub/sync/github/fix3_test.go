package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

func projectReplyWith(names ...string) reply {
	opts := []any{}
	for _, n := range names {
		opts = append(opts, map[string]any{"id": "opt-" + strings.ToLower(strings.ReplaceAll(n, " ", "-")), "name": n})
	}
	return data(map[string]any{"repositoryOwner": map[string]any{"projectV2": map[string]any{
		"id": "PVT_1", "title": "Platform", "field": map[string]any{"id": "FLD_status", "name": "Status", "options": opts},
	}}})
}

// A status option added on the board while the hub runs shows up in Statuses and can be moved
// to, without a restart.
func TestProjectCacheRefreshes(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReplyWith("Todo", "Done"), projectReplyWith("Todo", "Blocked", "Done"))
		a := newAdapter(t, f, mode, boardScope())
		ctx := context.Background()
		st, err := a.Statuses(ctx)
		if err != nil || fmt.Sprint(st) != "[{Todo todo} {Done done}]" {
			t.Fatalf("%v %v", st, err)
		}
		st, err = a.Statuses(ctx)
		if err != nil || fmt.Sprint(st) != "[{Todo todo} {Blocked active} {Done done}]" {
			t.Fatalf("statuses not refreshed: %v %v", st, err)
		}

		// Move: the cached project lacks the option; one reload finds it
		f2 := newFixture(t)
		f2.onGQL("Project", projectReplyWith("Todo", "Done"), projectReplyWith("Todo", "Blocked", "Done"))
		f2.onGQL("IssueItems", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_7", "state": "OPEN",
			"projectItems": map[string]any{"nodes": []any{map[string]any{"id": "PVTI_7", "project": map[string]any{"id": "PVT_1"}}}}}}}))
		f2.onGQL("SetStatus", data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_7"}}}))
		f2.onREST("PATCH /repos/acme/web/issues/7", ok(`{"number":7}`))
		b := newAdapter(t, f2, mode, boardScope())
		if err := b.Move(ctx, "acme/web#7", "Done"); err != nil { // loads the project
			t.Fatal(err)
		}
		if err := b.Move(ctx, "acme/web#7", "Blocked"); err != nil {
			t.Fatalf("new option: %v", err)
		}
		if s := f2.find("SetStatus"); len(s) != 2 || s[1].Vars["option"] != "opt-blocked" {
			t.Fatalf("set status %+v", s)
		}
		if n := len(f2.find("Project")); n != 2 {
			t.Fatalf("project loads %d, want 2", n)
		}
		// a status that is really missing reloads once, then fails
		if err := b.Move(ctx, "acme/web#7", "Shipped to the moon"); err == nil || !strings.Contains(err.Error(), "Blocked") {
			t.Fatalf("missing: %v", err)
		}
		if n := len(f2.find("Project")); n != 3 {
			t.Fatalf("project loads %d, want 3", n)
		}

		// Create checks its status against a fresh project too, before the POST
		f3 := newFixture(t)
		f3.onGQL("Project", projectReplyWith("Todo"), projectReplyWith("Todo", "Blocked"))
		c := newAdapter(t, f3, mode, boardScope())
		if _, err := c.Statuses(ctx); err != nil {
			t.Fatal(err)
		}
		f3.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9012,"number":12,"node_id":"I_12","html_url":"https://github.com/acme/web/issues/12","repository_url":"https://api.github.com/repos/acme/web"}`})
		f3.onGQL("AddToProject", data(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": "PVTI_12"}}}))
		f3.onGQL("IssueItems", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_12", "state": "OPEN",
			"projectItems": map[string]any{"nodes": []any{map[string]any{"id": "PVTI_12", "project": map[string]any{"id": "PVT_1"}}}}}}}))
		f3.onGQL("SetStatus", data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_12"}}}))
		f3.onGQL("Issue", reply{Status: 502, Body: `{"message":"bad gateway"}`})
		it, err := c.Create(ctx, hubsync.NewItem{Title: "x", Status: "Blocked"})
		if err != nil || it.Status != "Blocked" {
			t.Fatalf("create with a new option: %+v %v", it, err)
		}
	})
}

// The created id uses GitHub's spelling of owner and repository, as ListChanged and Get do, so
// the next sync matches it to the card instead of importing it again.
func TestCreateIDUsesCanonicalNames(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9013,"number":13,"node_id":"I_13",` +
			`"html_url":"https://github.com/Acme/Web/issues/13","repository_url":"https://api.github.com/repos/Acme/Web"}`})
		f.onGQL("Issue", reply{Status: 502, Body: `{"message":"bad gateway"}`})
		f.onREST("POST /repos/acme/web/issues/4/sub_issues", reply{Status: 429, Header: map[string]string{"Retry-After": "5"}, Body: `{"message":"slow down"}`})
		a := newAdapter(t, f, mode, repoScope())
		ctx := context.Background()
		it, err := a.Create(ctx, hubsync.NewItem{Title: "plain"})
		if err != nil || it.ID != "Acme/Web#13" || it.Key != "Web#13" {
			t.Fatalf("read-back failed: %+v %v", it, err)
		}
		_, err = a.Create(ctx, hubsync.NewItem{Title: "child", ParentID: "Acme/Web#4"})
		got, ok := hubsync.CreatedItem(err)
		if !ok || got.ID != "Acme/Web#13" || got.ParentID != "Acme/Web#4" {
			t.Fatalf("partial: %+v %v", got, err)
		}
		// no repository_url: html_url still names the repository
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9014,"number":14,"node_id":"I_14","html_url":"https://github.com/Acme/Web/issues/14"}`})
		if it, err := a.Create(ctx, hubsync.NewItem{Title: "plain"}); err != nil || it.ID != "Acme/Web#14" {
			t.Fatalf("html_url: %+v %v", it, err)
		}
	})
}

// A POST whose outcome is unknown (a dropped connection, a 5xx, an unreadable answer) may have
// opened the issue: Create says so with ErrCreateUncertain, or with a PartialCreateError when
// the answer still names the issue. A refused POST stays a plain error.
func TestCreateUncertainPost(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		for name, c := range map[string]struct {
			rep       reply
			uncertain bool
			partial   string
		}{
			"dropped":        {reply{Drop: true}, true, ""},
			"502":            {reply{Status: 502, Body: `{"message":"bad gateway"}`}, true, ""},
			"no number":      {reply{Status: 201, Body: `{"id":9012}`}, true, ""},
			"not json":       {reply{Status: 201, Body: `<html>`}, true, ""},
			"half readable":  {reply{Status: 201, Body: `{"id":"x","number":12,"html_url":"https://github.com/acme/web/issues/12"}`}, false, "acme/web#12"},
			"422":            {reply{Status: 422, Body: `{"message":"Validation Failed"}`}, false, ""},
			"rate limited":   {reply{Status: 429, Header: map[string]string{"Retry-After": "5"}, Body: `{"message":"slow"}`}, false, ""},
			"not authorized": {reply{Status: 403, Body: `{"message":"Resource not accessible"}`}, false, ""},
		} {
			f := newFixture(t)
			f.onREST("POST /repos/acme/web/issues", c.rep)
			f.onGQL("Issue", reply{Status: 502, Body: `{"message":"bad gateway"}`})
			a := newAdapter(t, f, mode, repoScope())
			it, err := a.Create(context.Background(), hubsync.NewItem{Title: "x"})
			if err == nil {
				t.Fatalf("%s: no error", name)
			}
			if got := errors.Is(err, hubsync.ErrCreateUncertain); got != c.uncertain {
				t.Fatalf("%s: uncertain %v, want %v: %v", name, got, c.uncertain, err)
			}
			made, ok := hubsync.CreatedItem(err)
			if c.partial != "" {
				if !ok || made.ID != c.partial || it.ID != c.partial {
					t.Fatalf("%s: partial %+v %+v %v", name, made, it, err)
				}
			} else if ok || it.ID != "" {
				t.Fatalf("%s: no id is known, got %+v %v", name, it, err)
			}
		}
	})
}
