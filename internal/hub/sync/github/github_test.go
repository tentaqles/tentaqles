package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

const secret = "s3cr3t-tok"

func js(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func data(v any) reply { return ok(js(map[string]any{"data": v})) }

type issueOpt func(map[string]any)

func withRepo(r string) issueOpt {
	return func(m map[string]any) { m["repository"] = map[string]any{"nameWithOwner": r} }
}
func withState(s string) issueOpt { return func(m map[string]any) { m["state"] = s } }
func withAssignee(l string) issueOpt {
	return func(m map[string]any) { m["assignees"] = map[string]any{"nodes": []any{map[string]any{"login": l}}} }
}
func withLabels(ls ...string) issueOpt {
	return func(m map[string]any) {
		var n []any
		for _, l := range ls {
			n = append(n, map[string]any{"name": l})
		}
		m["labels"] = map[string]any{"nodes": n}
	}
}
func withParent(repo string, n int, assignees ...string) issueOpt {
	return func(m map[string]any) {
		var as []any
		for _, l := range assignees {
			as = append(as, map[string]any{"login": l})
		}
		m["parent"] = map[string]any{"number": n, "repository": map[string]any{"nameWithOwner": repo},
			"assignees": map[string]any{"nodes": as}}
	}
}
func withComments(cs ...map[string]any) issueOpt {
	return func(m map[string]any) { m["comments"] = map[string]any{"nodes": cs} }
}
func withChildren(cs ...map[string]any) issueOpt {
	return func(m map[string]any) { m["subIssues"] = map[string]any{"nodes": cs} }
}
func withProjectItems(items ...map[string]any) issueOpt {
	return func(m map[string]any) { m["projectItems"] = map[string]any{"nodes": items} }
}

func issue(n int, title, updated string, opts ...issueOpt) map[string]any {
	m := map[string]any{
		"__typename": "Issue",
		"id":         fmt.Sprintf("I_%d", n), "databaseId": 9000 + n, "number": n, "title": title,
		"body": "body of " + title, "url": fmt.Sprintf("https://github.com/acme/web/issues/%d", n),
		"state": "OPEN", "updatedAt": updated,
		"repository": map[string]any{"nameWithOwner": "acme/web"},
		"assignees":  map[string]any{"nodes": []any{map[string]any{"login": "tester"}}},
		"labels":     map[string]any{"nodes": []any{}},
		"parent":     nil,
		"comments":   map[string]any{"nodes": []any{}},
		"subIssues":  map[string]any{"nodes": []any{}},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

func statusVal(name string) map[string]any {
	return map[string]any{"name": name, "optionId": "opt-" + strings.ToLower(strings.ReplaceAll(name, " ", "-"))}
}

func projectReply() reply {
	opts := []any{}
	for _, n := range []string{"Backlog", "Todo", "In Progress", "In Review", "Done"} {
		opts = append(opts, map[string]any{"id": "opt-" + strings.ToLower(strings.ReplaceAll(n, " ", "-")), "name": n})
	}
	return data(map[string]any{"repositoryOwner": map[string]any{"projectV2": map[string]any{
		"id": "PVT_1", "title": "Platform", "field": map[string]any{"id": "FLD_status", "name": "Status", "options": opts},
	}}})
}

func viewerReply() reply { return data(map[string]any{"viewer": map[string]any{"login": "tester"}}) }

// newAdapter builds the adapter in a transport mode: "cli" runs the gh double, "http" talks to the
// fixture server with a keychain token.
func newAdapter(t *testing.T, f *fixture, mode string, scope map[string]string) hubsync.Adapter {
	t.Helper()
	cfg := hubsync.Config{Workspace: "acme", Provider: "github", Scope: scope, AssignedTo: "me"}
	switch mode {
	case "cli":
		useFakeGH(t, f)
		cfg.Auth = cred.SourceCLI
		cfg.Env = []string{"GH_CONFIG_DIR=" + t.TempDir()}
	case "http":
		cfg.Auth = cred.SourceKeychain
		cfg.Transport = cred.Static(cred.SourceKeychain, cred.Token{Value: secret})
		cfg.BaseURL = f.srv.URL
	default:
		t.Fatal(mode)
	}
	a, err := hubsync.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

var modes = []string{"cli", "http"}

func repoScope() map[string]string { return map[string]string{"org": "acme", "project": "web"} }
func boardScope() map[string]string {
	return map[string]string{"org": "acme", "project": "web", "board": "3"}
}

func each(t *testing.T, fn func(t *testing.T, mode string)) {
	for _, m := range modes {
		t.Run(m, func(t *testing.T) { isolate(t); fn(t, m) })
	}
}

func TestRegistered(t *testing.T) {
	if _, ok := hubsync.Lookup("github"); !ok {
		t.Fatal("github adapter not registered")
	}
	if _, ok := cred.CLIHookFor("github"); !ok {
		t.Fatal("github cli hook not registered")
	}
	isolate(t)
	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope()})
	if err != nil || a.Name() != "github" {
		t.Fatal(a, err)
	}
}

func TestCLIHookUsesIdentityEnv(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	useFakeGH(t, f)
	t.Setenv("FAKE_GH_TOKEN", secret)
	h, _ := cred.CLIHookFor("github")
	tok, err := h(context.Background(), []string{"GH_CONFIG_DIR=" + t.TempDir()})
	if err != nil || tok.Value != secret {
		t.Fatalf("%v", err)
	}
	// an identity with no gh config dir never runs gh: it would use the shell's login
	if tok, err := h(context.Background(), nil); !errors.Is(err, cred.ErrNoCredential) || tok.Value != "" {
		t.Fatalf("unpinned: %v", err)
	}
	t.Setenv("FAKE_GH_LOGGEDOUT", "1")
	if _, err := h(context.Background(), []string{"GH_CONFIG_DIR=" + t.TempDir()}); !errors.Is(err, cred.ErrNoCredential) {
		t.Fatalf("logged out: %v", err)
	}
}

func TestCheckCLIRunsGHWithIdentityEnv(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Repo", data(map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}))
	useFakeGH(t, f)
	t.Setenv("GH_TOKEN", "ambient-token")
	dir := t.TempDir()
	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(),
		Env: []string{"GH_CONFIG_DIR=" + dir}})
	if err != nil {
		t.Fatal(err)
	}
	acct, err := a.Check(context.Background())
	if err != nil || acct != "tester" {
		t.Fatalf("%q %v", acct, err)
	}
	cs := f.all()
	if len(cs) != 2 {
		t.Fatalf("calls %+v", cs)
	}
	for _, c := range cs {
		if !c.ViaGH || c.ConfigDir != dir || c.AmbientToken || c.Auth != "" {
			t.Fatalf("gh call env %+v", c)
		}
	}
	if v := f.find("Repo")[0].Vars; v["owner"] != "acme" || v["name"] != "web" {
		t.Fatalf("repo vars %v", v)
	}
}

func TestCheckHTTPUsesTransport(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Repo", data(map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}))
	f.onGQL("Project", projectReply())
	a := newAdapter(t, f, "http", boardScope())
	acct, err := a.Check(context.Background())
	if err != nil || acct != "tester" {
		t.Fatalf("%q %v", acct, err)
	}
	for _, c := range f.all() {
		if c.ViaGH || c.Auth != "Bearer "+secret {
			t.Fatalf("http call %+v", c)
		}
	}
	if v := f.find("Project")[0].Vars; v["owner"] != "acme" || v["number"] != float64(3) || v["field"] != "Status" {
		t.Fatalf("project vars %v", v)
	}
}

func TestCheckMissingScope(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Repo", data(map[string]any{"repository": nil}))
	a := newAdapter(t, f, "http", repoScope())
	if _, err := a.Check(context.Background()); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("missing repo: %v", err)
	}
	a = newAdapter(t, f, "http", map[string]string{"org": "acme"})
	if _, err := a.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("no repo, no board: %v", err)
	}
}

func TestCLIFallsBackToResolver(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Repo", data(map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}))
	useFakeGH(t, f)
	t.Setenv("FAKE_GH_LOGGEDOUT", "1")

	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(), BaseURL: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Check(context.Background()); !errors.Is(err, cred.ErrNeedsSession) {
		t.Fatalf("no resolver: %v", err)
	}

	var got cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(_ context.Context, req cred.Request) (cred.Transport, error) {
		got = req
		return cred.Static(cred.SourceKeychain, cred.Token{Value: secret}), nil
	}))
	defer restore()
	a, _ = hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(), BaseURL: f.srv.URL})
	acct, err := a.Check(context.Background())
	if err != nil || acct != "tester" {
		t.Fatalf("%q %v", acct, err)
	}
	if got.Workspace != "acme" || got.Provider != "github" || got.Preferred != cred.SourceKeychain {
		t.Fatalf("request %+v", got)
	}
	if c := f.find("Repo"); len(c) != 1 || c[0].ViaGH || c[0].Auth != "Bearer "+secret {
		t.Fatalf("fallback call %+v", c)
	}
}

func TestStatuses(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	a := newAdapter(t, f, "http", repoScope())
	st, err := a.Statuses(context.Background())
	if err != nil || fmt.Sprint(st) != "[{Open todo} {Closed done}]" {
		t.Fatalf("%v %v", st, err)
	}
	if len(f.all()) != 0 {
		t.Fatal("no-board statuses need no request")
	}
	f.onGQL("Project", projectReply())
	a = newAdapter(t, f, "http", boardScope())
	st, err = a.Statuses(context.Background())
	want := "[{Backlog backlog} {Todo todo} {In Progress active} {In Review review} {Done done}]"
	if err != nil || fmt.Sprint(st) != want {
		t.Fatalf("%v %v", st, err)
	}
}

func TestCategoryFor(t *testing.T) {
	for name, want := range map[string]string{
		"Todo": "todo", "To Do": "todo", "Ready": "todo", "Not started": "todo", "Backlog": "backlog",
		"Icebox": "backlog", "In Progress": "active", "Doing": "active", "In Review": "review",
		"QA": "review", "Testing": "review", "Done": "done", "Shipped": "done", "Closed": "done",
		// whole words (or a word's stem), not substrings
		"Inactive": "todo", "Latest": "todo", "Contest": "todo", "Renewal": "todo",
		"New": "backlog", "Verified": "review", "Approved": "review", "Completed": "done",
		"In-Progress": "active", "InProgress": "active", "Started": "active", "NOT STARTED": "todo",
	} {
		if got := categoryFor(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

func TestListChangedRepo(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		page := func(next bool, cursor string, nodes ...any) reply {
			return data(map[string]any{"repository": map[string]any{"issues": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": next, "endCursor": cursor}, "nodes": nodes}}})
		}
		child := issue(5, "child", "2026-10-02T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 4))
		oldChild := issue(6, "old child", "2026-09-01T00:00:00Z", withParent("acme/web", 4))
		f.onGQL("Issues",
			page(true, "c1", issue(4, "parent", "2026-10-01T00:00:00Z", withLabels("bug", "agent-ready"),
				withComments(map[string]any{"id": "C1", "body": "first", "createdAt": "2026-10-01T01:00:00Z", "author": map[string]any{"login": "tester"}},
					map[string]any{"id": "C2", "body": "second", "createdAt": "2026-10-01T02:00:00Z", "author": nil}),
				withChildren(child, oldChild))),
			page(false, "c2", issue(8, "closed one", "2026-10-03T00:00:00Z", withState("CLOSED"))),
		)
		a := newAdapter(t, f, mode, repoScope())
		since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		items, wm, err := a.ListChanged(context.Background(), since)
		if err != nil {
			t.Fatal(err)
		}
		if !wm.Equal(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("watermark %v", wm)
		}
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		if fmt.Sprint(ids) != "[acme/web#4 acme/web#5 acme/web#8]" {
			t.Fatalf("ids %v", ids)
		}
		p := items[0]
		if p.Key != "web#4" || p.URL != "https://github.com/acme/web/issues/4" || p.Title != "parent" ||
			p.Body != "body of parent" || p.Status != "Open" || p.StatusCategory != "todo" || p.Assignee != "tester" ||
			p.Rev != "2026-10-01T00:00:00Z" || fmt.Sprint(p.Labels) != "[bug agent-ready]" {
			t.Fatalf("parent %+v", p)
		}
		if len(p.Comments) != 2 || p.Comments[0].ID != "C2" || p.Comments[1].Author != "tester" {
			t.Fatalf("comments newest first %+v", p.Comments)
		}
		if items[1].ParentID != "acme/web#4" || items[1].Assignee != "acme-dev" {
			t.Fatalf("child %+v", items[1])
		}
		if items[2].Status != "Closed" || items[2].StatusCategory != "done" {
			t.Fatalf("closed %+v", items[2])
		}
		cs := f.find("Issues")
		if len(cs) != 2 || cs[0].Vars["since"] != "2026-09-30T00:00:00Z" || cs[0].Vars["assignee"] != nil ||
			cs[1].Vars["cursor"] != "c1" || cs[0].Vars["owner"] != "acme" || cs[0].Vars["name"] != "web" {
			t.Fatalf("issue vars %+v", cs)
		}
	})
}

func TestListChangedAnyoneAndNoChange(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{}}}}))
	cfg := hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(), AssignedTo: "anyone",
		Auth: cred.SourceEnv, Transport: cred.Static(cred.SourceEnv, cred.Token{Value: secret}), BaseURL: f.srv.URL}
	a, _ := hubsync.New(cfg)
	since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil || len(items) != 0 || !wm.Equal(since) {
		t.Fatalf("%v %v %v", items, wm, err)
	}
	if len(f.find("Viewer")) != 0 {
		t.Fatal("anyone needs no viewer lookup")
	}
	if v := f.find("Issues")[0].Vars; v["assignee"] != nil {
		t.Fatalf("anyone sends no assignee: %v", v)
	}
}

func TestListChangedBoard(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		f.onGQL("Project", projectReply())
		item := func(id, updated string, status any, content any) map[string]any {
			return map[string]any{"id": id, "updatedAt": updated, "fieldValueByName": status, "content": content}
		}
		f.onGQL("ProjectItems",
			data(map[string]any{"node": map[string]any{"items": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "p1"},
				"nodes": []any{
					// issue unchanged but its card moved later: the item time counts
					item("PVTI_1", "2026-10-05T00:00:00Z", statusVal("In Progress"), issue(1, "moving", "2026-09-01T00:00:00Z",
						withChildren(issue(9, "child on the board", "2026-10-02T00:00:00Z", withParent("acme/web", 1))))),
					item("PVTI_2", "2026-10-01T00:00:00Z", nil, map[string]any{"__typename": "DraftIssue", "title": "draft"}),
					item("PVTI_3", "2026-10-02T00:00:00Z", statusVal("Todo"), issue(2, "not mine", "2026-10-02T00:00:00Z", withAssignee("acme-dev"))),
				},
			}}}),
			data(map[string]any{"node": map[string]any{"items": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false},
				"nodes": []any{
					item("PVTI_4", "2026-09-01T00:00:00Z", statusVal("Done"), issue(3, "old", "2026-09-01T00:00:00Z")),
					item("PVTI_5", "2026-10-03T00:00:00Z", statusVal("Todo"), issue(4, "other repo", "2026-10-03T00:00:00Z", withRepo("acme/api"))),
					item("PVTI_6", "2026-10-04T00:00:00Z", nil, issue(5, "no status", "2026-10-04T00:00:00Z", withState("CLOSED"))),
					// seen first as a child (no status); its own board entry wins
					item("PVTI_9", "2026-10-02T00:00:00Z", statusVal("In Review"), issue(9, "child on the board", "2026-10-02T00:00:00Z", withParent("acme/web", 1))),
				},
			}}}),
		)
		a := newAdapter(t, f, mode, boardScope())
		since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		items, wm, err := a.ListChanged(context.Background(), since)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Fatalf("items %+v", items)
		}
		if it := items[0]; it.ID != "acme/web#1" || it.Status != "In Progress" || it.StatusCategory != "active" ||
			it.Rev != "2026-10-05T00:00:00Z" || !it.UpdatedAt.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("moved item %+v", it)
		}
		if it := items[1]; it.ID != "acme/web#9" || it.Status != "In Review" || it.ParentID != "acme/web#1" {
			t.Fatalf("child item %+v", it)
		}
		if it := items[2]; it.ID != "acme/web#5" || it.Status != "" || it.StatusCategory != "done" {
			t.Fatalf("no status item %+v", it)
		}
		if !wm.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("watermark %v", wm)
		}
		cs := f.find("ProjectItems")
		if len(cs) != 2 || cs[0].Vars["id"] != "PVT_1" || cs[1].Vars["cursor"] != "p1" || cs[0].Vars["field"] != "Status" {
			t.Fatalf("vars %+v", cs)
		}
	})
}

func TestGet(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReply())
		f.onGQL("Issue", data(map[string]any{"repository": map[string]any{"issue": issue(7, "seven", "2026-10-01T00:00:00Z",
			withParent("acme/web", 4),
			withProjectItems(
				map[string]any{"id": "PVTI_other", "updatedAt": "2026-10-09T00:00:00Z", "project": map[string]any{"id": "PVT_9"}, "fieldValueByName": statusVal("Done")},
				map[string]any{"id": "PVTI_7", "updatedAt": "2026-10-02T00:00:00Z", "project": map[string]any{"id": "PVT_1"}, "fieldValueByName": statusVal("In Review")},
			))}}))
		a := newAdapter(t, f, mode, boardScope())
		for _, id := range []string{"acme/web#7", "#7", "7"} {
			it, err := a.Get(context.Background(), id)
			if err != nil {
				t.Fatal(id, err)
			}
			if it.ID != "acme/web#7" || it.ParentID != "acme/web#4" || it.Status != "In Review" ||
				it.StatusCategory != "review" || it.Rev != "2026-10-02T00:00:00Z" {
				t.Fatalf("%s: %+v", id, it)
			}
		}
		if v := f.find("Issue")[0].Vars; v["owner"] != "acme" || v["name"] != "web" || v["number"] != float64(7) {
			t.Fatalf("vars %v", v)
		}
		f.onGQL("Issue", data(map[string]any{"repository": map[string]any{"issue": nil}}))
		if _, err := a.Get(context.Background(), "acme/web#99"); !errors.Is(err, hubsync.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		for _, bad := range []string{"", "acme/web#x", "web#3", "a/b/c#1"} {
			if _, err := a.Get(context.Background(), bad); err == nil {
				t.Fatalf("bad id %q accepted", bad)
			}
		}
	})
}

func TestGetGraphQLNotFound(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Issue", ok(js(map[string]any{"data": map[string]any{"repository": nil},
		"errors": []any{map[string]any{"type": "NOT_FOUND", "message": "Could not resolve to a Repository"}}})))
	a := newAdapter(t, f, "http", repoScope())
	if _, err := a.Get(context.Background(), "acme/gone#1"); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestMoveBoard(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReply())
		f.onGQL("IssueItems", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_7",
			"projectItems": map[string]any{"nodes": []any{
				map[string]any{"id": "PVTI_x", "project": map[string]any{"id": "PVT_9"}},
				map[string]any{"id": "PVTI_7", "project": map[string]any{"id": "PVT_1"}},
			}}}}}))
		f.onGQL("SetStatus", data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_7"}}}))
		a := newAdapter(t, f, mode, boardScope())
		if err := a.Move(context.Background(), "acme/web#7", "in progress"); err != nil {
			t.Fatal(err)
		}
		// a category name picks the first status of that category
		if err := a.Move(context.Background(), "acme/web#7", "review"); err != nil {
			t.Fatal(err)
		}
		cs := f.find("SetStatus")
		if len(cs) != 2 || cs[0].Vars["project"] != "PVT_1" || cs[0].Vars["item"] != "PVTI_7" ||
			cs[0].Vars["field"] != "FLD_status" || cs[0].Vars["option"] != "opt-in-progress" || cs[1].Vars["option"] != "opt-in-review" {
			t.Fatalf("set status %+v", cs)
		}
		err := a.Move(context.Background(), "acme/web#7", "Shipped to the moon")
		if err == nil || !strings.Contains(err.Error(), "In Progress") {
			t.Fatalf("unknown status: %v", err)
		}

		// not on the board yet: it is added first
		f.onGQL("IssueItems", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_8",
			"projectItems": map[string]any{"nodes": []any{}}}}}))
		f.onGQL("AddToProject", data(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": "PVTI_8"}}}))
		if err := a.Move(context.Background(), "acme/web#8", "Done"); err != nil {
			t.Fatal(err)
		}
		add := f.find("AddToProject")
		if len(add) != 1 || add[0].Vars["project"] != "PVT_1" || add[0].Vars["content"] != "I_8" {
			t.Fatalf("add %+v", add)
		}
		if cs := f.find("SetStatus"); cs[len(cs)-1].Vars["item"] != "PVTI_8" || cs[len(cs)-1].Vars["option"] != "opt-done" {
			t.Fatalf("set after add %+v", cs)
		}
	})
}

func TestMoveRepoOpensAndCloses(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("PATCH /repos/acme/web/issues/7", ok(`{"number":7}`))
		a := newAdapter(t, f, mode, repoScope())
		for _, s := range []string{"Closed", "done", "open", "active"} {
			if err := a.Move(context.Background(), "acme/web#7", s); err != nil {
				t.Fatal(s, err)
			}
		}
		var states []any
		for _, c := range f.find("PATCH /repos/acme/web/issues/7") {
			states = append(states, c.Body["state"])
		}
		if fmt.Sprint(states) != "[closed closed open open]" {
			t.Fatalf("states %v", states)
		}
		if err := a.Move(context.Background(), "acme/web#7", "In Progress"); err == nil {
			t.Fatal("unknown status accepted without a board")
		}
	})
}

func TestComment(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues/7/comments", reply{Status: 201, Body: `{"id":1}`})
		a := newAdapter(t, f, mode, repoScope())
		md := "**Done**: see PR #12\n\n- [x] tests"
		if err := a.Comment(context.Background(), "acme/web#7", md); err != nil {
			t.Fatal(err)
		}
		cs := f.find("POST /repos/acme/web/issues/7/comments")
		if len(cs) != 1 || cs[0].Body["body"] != md {
			t.Fatalf("%+v", cs)
		}
	})
}

func TestCreateChildOnBoard(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReply())
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9012,"number":12,"node_id":"I_12","html_url":"https://github.com/acme/web/issues/12"}`})
		f.onREST("POST /repos/acme/web/issues/4/sub_issues", reply{Status: 201, Body: `{"number":4}`})
		f.onGQL("AddToProject", data(map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": "PVTI_12"}}}))
		f.onGQL("IssueItems", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_12",
			"projectItems": map[string]any{"nodes": []any{map[string]any{"id": "PVTI_12", "project": map[string]any{"id": "PVT_1"}}}}}}}))
		f.onGQL("SetStatus", data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_12"}}}))
		f.onGQL("Issue", data(map[string]any{"repository": map[string]any{"issue": issue(12, "child", "2026-10-02T00:00:00Z",
			withParent("acme/web", 4), withLabels("feature"),
			withProjectItems(map[string]any{"id": "PVTI_12", "updatedAt": "2026-10-02T00:00:00Z", "project": map[string]any{"id": "PVT_1"}, "fieldValueByName": statusVal("Todo")}))}}))
		a := newAdapter(t, f, mode, boardScope())
		it, err := a.Create(context.Background(), hubsync.NewItem{Title: "child", Body: "do it", Acceptance: "- [ ] works",
			ParentID: "acme/web#4", Status: "Todo", Labels: []string{"feature"}})
		if err != nil {
			t.Fatal(err)
		}
		if it.ID != "acme/web#12" || it.ParentID != "acme/web#4" || it.Status != "Todo" {
			t.Fatalf("%+v", it)
		}
		c := f.find("POST /repos/acme/web/issues")[0]
		if c.Body["title"] != "child" || !strings.Contains(c.Body["body"].(string), "do it") ||
			!strings.Contains(c.Body["body"].(string), "## Acceptance criteria\n\n- [ ] works") || fmt.Sprint(c.Body["labels"]) != "[feature]" {
			t.Fatalf("create body %+v", c.Body)
		}
		if s := f.find("POST /repos/acme/web/issues/4/sub_issues"); len(s) != 1 || s[0].Body["sub_issue_id"] != float64(9012) {
			t.Fatalf("sub issue %+v", s)
		}
		if add := f.find("AddToProject"); len(add) != 1 || add[0].Vars["content"] != "I_12" {
			t.Fatalf("add %+v", add)
		}
		if s := f.find("SetStatus"); len(s) != 1 || s[0].Vars["option"] != "opt-todo" {
			t.Fatalf("status %+v", s)
		}
	})
}

func TestCreateRepoNoParent(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9013,"number":13,"node_id":"I_13"}`})
	f.onGQL("Issue", data(map[string]any{"repository": map[string]any{"issue": issue(13, "plain", "2026-10-02T00:00:00Z")}}))
	a := newAdapter(t, f, "http", repoScope())
	it, err := a.Create(context.Background(), hubsync.NewItem{Title: "plain"})
	if err != nil || it.ID != "acme/web#13" || it.Status != "Open" {
		t.Fatalf("%+v %v", it, err)
	}
	if b := f.find("POST /repos/acme/web/issues")[0].Body; b["body"] != "" || b["labels"] != nil {
		t.Fatalf("body %+v", b)
	}
	if len(f.find("AddToProject")) != 0 {
		t.Fatal("no board, no project add")
	}
	a = newAdapter(t, f, "http", map[string]string{"org": "acme", "board": "3"})
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "x"}); err == nil {
		t.Fatal("create without a repo in scope or a parent")
	}
}

func TestUpdate(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("PATCH /repos/acme/web/issues/7", ok(`{"number":7}`))
		a := newAdapter(t, f, mode, repoScope())
		title := "new title"
		if err := a.Update(context.Background(), "acme/web#7", &title, nil); err != nil {
			t.Fatal(err)
		}
		if err := a.Update(context.Background(), "acme/web#7", nil, nil); err != nil {
			t.Fatal(err)
		}
		empty := ""
		if err := a.Update(context.Background(), "acme/web#7", nil, &empty); err != nil {
			t.Fatal(err)
		}
		cs := f.find("PATCH /repos/acme/web/issues/7")
		if len(cs) != 2 || fmt.Sprint(cs[0].Body) != "map[title:new title]" || fmt.Sprint(cs[1].Body) != "map[body:]" {
			t.Fatalf("%+v", cs)
		}
	})
}

func TestSetLabels(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues/7/labels", ok(`[]`))
		f.onREST("DELETE /repos/acme/web/issues/7/labels/needs info", ok(`[]`))
		f.onREST("DELETE /repos/acme/web/issues/7/labels/gone", reply{Status: 404, Body: `{"message":"Label does not exist"}`})
		a := newAdapter(t, f, mode, repoScope())
		if err := a.SetLabels(context.Background(), "acme/web#7", []string{"agent-ready", "bug"}, []string{"needs info", "gone"}); err != nil {
			t.Fatal(err)
		}
		if cs := f.find("POST /repos/acme/web/issues/7/labels"); len(cs) != 1 || fmt.Sprint(cs[0].Body["labels"]) != "[agent-ready bug]" {
			t.Fatalf("add %+v", cs)
		}
		if len(f.find("DELETE /repos/acme/web/issues/7/labels/needs info")) != 1 {
			t.Fatalf("remove %+v", f.all())
		}
		if err := a.SetLabels(context.Background(), "acme/web#7", nil, nil); err != nil {
			t.Fatal(err)
		}
		if n := len(f.all()); n != 3 {
			t.Fatalf("empty SetLabels sent requests: %d", n)
		}
	})
}

func TestRateLimits(t *testing.T) {
	fixed := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		rep  reply
		want time.Duration
	}{
		{"429 retry-after", reply{Status: 429, Header: map[string]string{"Retry-After": "7"}, Body: `{"message":"slow down"}`}, 7 * time.Second},
		{"secondary 403", reply{Status: 403, Body: `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`}, time.Minute},
		{"primary exhausted", reply{Status: 403, Header: map[string]string{"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset": fmt.Sprint(fixed.Add(90 * time.Second).Unix())}, Body: `{"message":"API rate limit exceeded"}`}, 90 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			each(t, func(t *testing.T, mode string) {
				restore := setNow(func() time.Time { return fixed })
				defer restore()
				f := newFixture(t)
				f.onREST("POST /repos/acme/web/issues/7/comments", c.rep)
				a := newAdapter(t, f, mode, repoScope())
				err := a.Comment(context.Background(), "acme/web#7", "hi")
				var rl *hubsync.RateLimitError
				if !errors.As(err, &rl) || rl.Provider != "github" || rl.RetryAfter != c.want {
					t.Fatalf("%T %v", err, err)
				}
			})
		})
	}
	t.Run("graphql RATE_LIMITED", func(t *testing.T) {
		each(t, func(t *testing.T, mode string) {
			f := newFixture(t)
			f.onGQL("Viewer", ok(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded for user"}]}`))
			a := newAdapter(t, f, mode, repoScope())
			_, err := a.Check(context.Background())
			var rl *hubsync.RateLimitError
			if !errors.As(err, &rl) {
				t.Fatalf("%T %v", err, err)
			}
		})
	})
	t.Run("graphql 403 secondary", func(t *testing.T) {
		each(t, func(t *testing.T, mode string) {
			f := newFixture(t)
			f.onGQL("Viewer", reply{Status: 403, Header: map[string]string{"Retry-After": "30"},
				Body: `{"message":"You have exceeded a secondary rate limit"}`})
			a := newAdapter(t, f, mode, repoScope())
			_, err := a.Check(context.Background())
			var rl *hubsync.RateLimitError
			if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
				t.Fatalf("%T %v", err, err)
			}
		})
	})
}

func TestErrorsNameTheFailureNotTheToken(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues/7/comments", reply{Status: 401, Body: `{"message":"Bad credentials"}`})
		f.onREST("PATCH /repos/acme/web/issues/9", reply{Status: 404, Body: `{"message":"Not Found"}`})
		f.onREST("PATCH /repos/acme/web/issues/10", reply{Status: 422, Body: `{"message":"Validation Failed","errors":[{"message":"title is too long"}]}`})
		a := newAdapter(t, f, mode, repoScope())
		err := a.Comment(context.Background(), "acme/web#7", "hi")
		if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Bad credentials") {
			t.Fatalf("%v", err)
		}
		title := "t"
		if err := a.Update(context.Background(), "acme/web#9", &title, nil); !errors.Is(err, hubsync.ErrNotFound) {
			t.Fatalf("404: %v", err)
		}
		if err := a.Update(context.Background(), "acme/web#10", &title, nil); err == nil || !strings.Contains(err.Error(), "title is too long") {
			t.Fatalf("422: %v", err)
		}
	})
}
