package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// keychainResolver makes the cred chain hand out a keychain token and records the request.
func keychainResolver(t *testing.T) *cred.Request {
	t.Helper()
	got := &cred.Request{}
	restore := cred.SetDefault(cred.ResolverFunc(func(_ context.Context, req cred.Request) (cred.Transport, error) {
		*got = req
		return cred.Static(cred.SourceKeychain, cred.Token{Value: secret}), nil
	}))
	t.Cleanup(restore)
	return got
}

func TestGHEnvAlwaysDropsAmbientIdentity(t *testing.T) {
	base := []string{"PATH=/bin", "GH_TOKEN=ambient", "GITHUB_TOKEN=ambient", "GH_HOST=ghe.acme.test",
		"GH_ENTERPRISE_TOKEN=x", "GITHUB_ENTERPRISE_TOKEN=y", "GH_CONFIG_DIR=/ambient/gh", "gh_token=lower"}
	for _, identity := range [][]string{nil, {"AZURE_CONFIG_DIR=/ws/az"}, {"GH_CONFIG_DIR=/ws/gh"}} {
		got := ghEnv(base, identity)
		for _, kv := range got {
			k, v, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(k) {
			case "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
				t.Fatalf("identity %v: ambient %s kept", identity, k)
			case "GH_CONFIG_DIR":
				if v != "/ws/gh" {
					t.Fatalf("identity %v: GH_CONFIG_DIR=%s", identity, v)
				}
			}
		}
	}
	// the identity may set its own host
	got := ghEnv(base, []string{"GH_CONFIG_DIR=/ws/gh", "GH_HOST=ghe.acme.test"})
	if !strings.Contains(strings.Join(got, "\n"), "GH_HOST=ghe.acme.test") {
		t.Fatalf("identity host dropped: %v", got)
	}
}

// An identity env without GH_CONFIG_DIR must not run gh with the hub process's login: the cli
// backend stands aside and the cred chain takes over.
func TestCLIUnpinnedIdentityNeverUsesAmbientLogin(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Repo", data(map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}))
	useFakeGH(t, f)
	t.Setenv("GH_CONFIG_DIR", t.TempDir()) // another workspace's dir, in the hub process env
	t.Setenv("GH_TOKEN", "ambient-token")
	got := keychainResolver(t)
	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(), BaseURL: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if acct, err := a.Check(context.Background()); err != nil || acct != "tester" {
		t.Fatalf("%q %v", acct, err)
	}
	for _, c := range f.all() {
		if c.ViaGH || c.AmbientToken || c.Auth != "Bearer "+secret {
			t.Fatalf("call used gh or the ambient login: %+v", c)
		}
	}
	if got.Preferred != cred.SourceKeychain {
		t.Fatalf("resolver request %+v", got)
	}
}

func TestCLI401FallsBackToResolver(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onREST("POST /repos/acme/web/issues/7/comments", reply{Status: 201, Body: `{"id":1}`})
	f.onGQL("Viewer", reply{Status: 401, Body: `{"message":"Bad credentials"}`}, viewerReply())
	useFakeGH(t, f)
	keychainResolver(t)
	a, _ := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(), BaseURL: f.srv.URL,
		Env: []string{"GH_CONFIG_DIR=" + t.TempDir()}})
	f.onGQL("Repo", data(map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}))
	if acct, err := a.Check(context.Background()); err != nil || acct != "tester" {
		t.Fatalf("%q %v", acct, err)
	}
	cs := f.find("Viewer")
	if len(cs) != 2 || !cs[0].ViaGH || cs[1].ViaGH || cs[1].Auth != "Bearer "+secret {
		t.Fatalf("viewer calls %+v", cs)
	}
}

func TestCLI401WithNoFallbackSaysWhy(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", reply{Status: 401, Body: `{"message":"Bad credentials"}`})
	a := newAdapter(t, f, "cli", repoScope())
	_, err := a.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), secret) {
		t.Fatalf("%v", err)
	}
}

func boardItemsReply(next bool, cursor string, nodes ...any) reply {
	return data(map[string]any{"node": map[string]any{"items": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": next, "endCursor": cursor}, "nodes": nodes}}})
}

// boardItem is a board page node. An issue's content carries its sub-issue count, as the light
// board query reads it; the full issue is served by onBoard/serveNodes.
func boardItem(id, updated string, status any, content any) map[string]any {
	if m, ok := content.(map[string]any); ok && m["__typename"] == "Issue" {
		cp := map[string]any{}
		for k, v := range m {
			cp[k] = v
		}
		total := 0
		if s, ok := m["subIssues"].(map[string]any); ok {
			if n, ok := s["nodes"].([]map[string]any); ok {
				total = len(n)
			}
			if n, ok := s["nodes"].([]any); ok {
				total = len(n)
			}
		}
		cp["subIssuesSummary"] = map[string]any{"total": total}
		content = cp
	}
	return map[string]any{"id": id, "updatedAt": updated, "fieldValueByName": status, "content": content}
}

// An unassigned child is skipped as a board item of its own (not the viewer's) and comes in
// through its parent: it must still carry its board status and the board item's time.
func TestListChangedBoardUnassignedChildKeepsStatus(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		f.onGQL("Project", projectReply())
		child := issue(9, "child", "2026-10-02T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 1, "tester"),
			withProjectItems(
				map[string]any{"id": "PVTI_x", "updatedAt": "2026-10-09T00:00:00Z", "project": map[string]any{"id": "PVT_9"}, "fieldValueByName": statusVal("Done")},
				map[string]any{"id": "PVTI_9", "updatedAt": "2026-10-04T00:00:00Z", "project": map[string]any{"id": "PVT_1"}, "fieldValueByName": statusVal("In Progress")}))
		onBoard(f, []any{
			boardItem("PVTI_1", "2026-10-01T00:00:00Z", statusVal("Todo"), issue(1, "parent", "2026-10-01T00:00:00Z", withChildren(child))),
			boardItem("PVTI_9", "2026-10-04T00:00:00Z", statusVal("In Progress"), child),
		})
		a := newAdapter(t, f, mode, boardScope())
		items, _, err := a.ListChanged(context.Background(), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("items %+v", items)
		}
		ch := items[1]
		if ch.ID != "acme/web#9" || ch.Status != "In Progress" || ch.StatusCategory != "active" || ch.Rev != "2026-10-04T00:00:00Z" {
			t.Fatalf("child %+v", ch)
		}
		if v := f.find("ProjectItems")[0].Vars; v["field"] != "Status" {
			t.Fatalf("vars %v", v)
		}
	})
}

// Only the child changed: the parent is not in the since window, but the child (unassigned, its
// parent the viewer's) is, so it is listed.
func TestListChangedRepoChildOnlyChange(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes": []any{
				issue(5, "child", "2026-10-02T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 4, "tester")),
				issue(6, "someone else's", "2026-10-03T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 2, "acme-dev")),
				issue(7, "unrelated", "2026-10-04T00:00:00Z", withAssignee("acme-dev")),
			}}}}))
		a := newAdapter(t, f, mode, repoScope())
		since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		items, wm, err := a.ListChanged(context.Background(), since)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != "acme/web#5" || items[0].ParentID != "acme/web#4" {
			t.Fatalf("items %+v", items)
		}
		// skipped issues still move the watermark: the walk is in update order
		if !wm.Equal(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("watermark %v", wm)
		}
		if v := f.find("Issues")[0].Vars; v["assignee"] != nil {
			t.Fatalf("the assignee filter is client side: %v", v)
		}
	})
}

func TestListChangedBoardTruncatedIsAnError(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Project", projectReply())
	f.onGQL("ProjectItems", boardItemsReply(true, "more",
		boardItem("PVTI_1", "2026-10-05T00:00:00Z", statusVal("Todo"), issue(1, "one", "2026-10-05T00:00:00Z"))))
	a := newAdapter(t, f, "http", boardScope())
	since := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	items, wm, err := a.ListChanged(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "more than") || items != nil || !wm.Equal(since) {
		t.Fatalf("%v %v %v", items, wm, err)
	}
	if n := len(f.find("ProjectItems")); n != maxBoardPages {
		t.Fatalf("pages %d", n)
	}
}

func TestListChangedRepoTruncatedKeepsTopLevelWatermark(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "more"},
		"nodes": []any{issue(4, "parent", "2026-10-01T00:00:00Z",
			withChildren(issue(5, "late child", "2026-10-09T00:00:00Z", withParent("acme/web", 4, "tester"))))}}}}))
	a := newAdapter(t, f, "http", repoScope())
	_, wm, err := a.ListChanged(context.Background(), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// the walk stopped at the cap: the watermark must not jump past issues not read yet
	if !wm.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("watermark %v", wm)
	}
}

func TestListChangedPagesSubIssues(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		parent := issue(4, "parent", "2026-10-01T00:00:00Z", withChildren(issue(5, "c5", "2026-10-02T00:00:00Z", withParent("acme/web", 4, "tester"))))
		parent["subIssues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "s1"}
		f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{parent}}}}))
		f.onGQL("SubIssues", data(map[string]any{"node": map[string]any{"subIssues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes":    []any{issue(6, "c6", "2026-10-03T00:00:00Z", withParent("acme/web", 4, "tester"))}}}}))
		a := newAdapter(t, f, mode, repoScope())
		items, _, err := a.ListChanged(context.Background(), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		if fmt.Sprint(ids) != "[acme/web#4 acme/web#5 acme/web#6]" {
			t.Fatalf("ids %v", ids)
		}
		cs := f.find("SubIssues")
		if len(cs) != 1 || cs[0].Vars["id"] != "I_4" || cs[0].Vars["cursor"] != "s1" {
			t.Fatalf("sub-issue pages %+v", cs)
		}
	})
}

// A failure after the POST must hand back the created issue, so a retry does not open another.
func TestCreatePartialFailureReturnsTheCreatedIssue(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9012,"number":12,"node_id":"I_12","html_url":"https://github.com/acme/web/issues/12"}`})
		f.onREST("POST /repos/acme/web/issues/4/sub_issues", reply{Status: 429, Header: map[string]string{"Retry-After": "5"}, Body: `{"message":"slow down"}`})
		a := newAdapter(t, f, mode, repoScope())
		it, err := a.Create(context.Background(), hubsync.NewItem{Title: "child", ParentID: "acme/web#4"})
		var rl *hubsync.RateLimitError
		if !errors.As(err, &rl) {
			t.Fatalf("want the rate limit, got %T %v", err, err)
		}
		if it.ID != "acme/web#12" || it.Key != "web#12" || it.URL != "https://github.com/acme/web/issues/12" || it.Title != "child" {
			t.Fatalf("partial item %+v", it)
		}
		if n := len(f.find("POST /repos/acme/web/issues")); n != 1 {
			t.Fatalf("creates %d", n)
		}
	})
}

// When only the read-back fails the issue is complete: Create succeeds with what it knows.
func TestCreateReadBackFailureStillSucceeds(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9013,"number":13,"node_id":"I_13","html_url":"https://github.com/acme/web/issues/13"}`})
	f.onGQL("Issue", reply{Status: 502, Body: `{"message":"bad gateway"}`})
	a := newAdapter(t, f, "http", repoScope())
	it, err := a.Create(context.Background(), hubsync.NewItem{Title: "plain", Labels: []string{"bug"}})
	if err != nil || it.ID != "acme/web#13" || it.Status != "Open" || it.StatusCategory != "todo" || fmt.Sprint(it.Labels) != "[bug]" {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestMoveBoardClosesAndReopens(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReply())
		onBoard := func(state string) reply {
			return data(map[string]any{"repository": map[string]any{"issue": map[string]any{"id": "I_7", "state": state,
				"projectItems": map[string]any{"nodes": []any{map[string]any{"id": "PVTI_7", "project": map[string]any{"id": "PVT_1"}}}}}}})
		}
		f.onGQL("SetStatus", data(map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": "PVTI_7"}}}))
		f.onREST("PATCH /repos/acme/web/issues/7", ok(`{"number":7}`))
		a := newAdapter(t, f, mode, boardScope())

		f.onGQL("IssueItems", onBoard("OPEN"))
		if err := a.Move(context.Background(), "acme/web#7", "Done"); err != nil {
			t.Fatal(err)
		}
		f.onGQL("IssueItems", onBoard("CLOSED"))
		if err := a.Move(context.Background(), "acme/web#7", "In Progress"); err != nil {
			t.Fatal(err)
		}
		f.onGQL("IssueItems", onBoard("OPEN"))
		if err := a.Move(context.Background(), "acme/web#7", "In Review"); err != nil {
			t.Fatal(err)
		}
		f.onGQL("IssueItems", onBoard("CLOSED"))
		if err := a.Move(context.Background(), "acme/web#7", "Done"); err != nil {
			t.Fatal(err)
		}
		var states []any
		for _, c := range f.find("PATCH /repos/acme/web/issues/7") {
			states = append(states, c.Body["state"])
		}
		if fmt.Sprint(states) != "[closed open]" {
			t.Fatalf("state changes %v", states)
		}
		if n := len(f.find("SetStatus")); n != 4 {
			t.Fatalf("set status %d", n)
		}
	})
}
