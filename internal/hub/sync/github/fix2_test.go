package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// A status Create cannot set is refused before the issue exists, so a retry never opens a
// second issue.
func TestCreateRejectsUnsettableStatusBeforePost(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Project", projectReply())
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9012,"number":12,"node_id":"I_12"}`})

		a := newAdapter(t, f, mode, boardScope())
		it, err := a.Create(context.Background(), hubsync.NewItem{Title: "x", Status: "Shipped to the moon"})
		var pc *hubsync.PartialCreateError
		if err == nil || it.ID != "" || errors.As(err, &pc) || !strings.Contains(err.Error(), "In Progress") {
			t.Fatalf("board: %+v %v", it, err)
		}

		a = newAdapter(t, f, mode, repoScope())
		it, err = a.Create(context.Background(), hubsync.NewItem{Title: "x", Status: "In Progress"})
		if err == nil || it.ID != "" || errors.As(err, &pc) {
			t.Fatalf("repo: %+v %v", it, err)
		}
		if n := len(f.find("POST /repos/acme/web/issues")); n != 0 {
			t.Fatalf("issues created: %d", n)
		}
	})
}

// A failure after the POST is a PartialCreateError carrying the created item; the cause stays
// reachable with errors.As.
func TestCreatePartialFailureIsTyped(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/web/issues", reply{Status: 201, Body: `{"id":9012,"number":12,"node_id":"I_12","html_url":"https://github.com/acme/web/issues/12"}`})
		f.onREST("POST /repos/acme/web/issues/4/sub_issues", reply{Status: 429, Header: map[string]string{"Retry-After": "5"}, Body: `{"message":"slow down"}`})
		a := newAdapter(t, f, mode, repoScope())
		_, err := a.Create(context.Background(), hubsync.NewItem{Title: "child", ParentID: "acme/web#4"})
		var pc *hubsync.PartialCreateError
		if !errors.As(err, &pc) || pc.Item.ID != "acme/web#12" || pc.Item.URL != "https://github.com/acme/web/issues/12" {
			t.Fatalf("%T %v", err, err)
		}
		var rl *hubsync.RateLimitError
		if !errors.As(err, &rl) || rl.RetryAfter != 5*time.Second {
			t.Fatalf("cause lost: %v", err)
		}
		if it, ok := hubsync.CreatedItem(err); !ok || it.ID != "acme/web#12" {
			t.Fatalf("CreatedItem %+v %v", it, ok)
		}
	})
}

func TestWritesStayInScope(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onREST("POST /repos/acme/api/issues/3/comments", reply{Status: 201, Body: `{"id":1}`})
		a := newAdapter(t, f, mode, repoScope())
		ctx := context.Background()
		title := "t"
		for name, fn := range map[string]func(id string) error{
			"comment":   func(id string) error { return a.Comment(ctx, id, "hi") },
			"move":      func(id string) error { return a.Move(ctx, id, "closed") },
			"update":    func(id string) error { return a.Update(ctx, id, &title, nil) },
			"setlabels": func(id string) error { return a.SetLabels(ctx, id, []string{"x"}, nil) },
			"create": func(id string) error {
				_, err := a.Create(ctx, hubsync.NewItem{Title: "x", ParentID: id})
				return err
			},
		} {
			if err := fn("other-org/web#1"); !errors.Is(err, ErrOutOfScope) {
				t.Fatalf("%s other owner: %v", name, err)
			}
		}
		if n := len(f.all()); n != 0 {
			t.Fatalf("an out-of-scope owner reached the API: %+v", f.all())
		}

		// another repo of the owner, without a board: only a sub-issue of a scope issue
		f.onGQL("IssueParent", data(map[string]any{"repository": map[string]any{"issue": map[string]any{"parent": nil}}}))
		if err := a.Comment(ctx, "acme/api#3", "hi"); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("other repo, no parent: %v", err)
		}
		f.onGQL("IssueParent", data(map[string]any{"repository": map[string]any{"issue": map[string]any{
			"parent": map[string]any{"repository": map[string]any{"nameWithOwner": "acme/web"}}}}}))
		if err := a.Comment(ctx, "acme/api#3", "hi"); err != nil {
			t.Fatalf("cross-repo sub-issue: %v", err)
		}
		if v := f.find("IssueParent")[0].Vars; v["owner"] != "acme" || v["name"] != "api" || v["number"] != float64(3) {
			t.Fatalf("vars %v", v)
		}
		if n := len(f.find("POST /repos/acme/api/issues/3/comments")); n != 1 {
			t.Fatalf("comments %d", n)
		}

		// with a board, another repository of the owner is in scope only for an issue on the
		// board, or a sub-issue of one there or of a scope.project issue
		f2 := newFixture(t)
		f2.onGQL("Project", projectReply())
		f2.onREST("POST /repos/acme/api/issues/3/comments", reply{Status: 201, Body: `{"id":1}`})
		f2.onREST("POST /repos/acme/web/issues/3/comments", reply{Status: 201, Body: `{"id":1}`})
		b := newAdapter(t, f2, mode, boardScope())
		if err := b.Comment(ctx, "acme/web#3", "hi"); err != nil {
			t.Fatalf("board, scope repo: %v", err)
		}
		if len(f2.find("IssueParent")) != 0 {
			t.Fatal("the scope repository needs no lookup")
		}
		if err := b.Comment(ctx, "other-org/api#3", "hi"); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("board, other owner: %v", err)
		}
		items := func(projects ...string) map[string]any {
			var n []any
			for _, p := range projects {
				n = append(n, map[string]any{"project": map[string]any{"id": p}})
			}
			return map[string]any{"nodes": n}
		}
		scopeReply := func(issue map[string]any) reply {
			return data(map[string]any{"repository": map[string]any{"issue": issue}})
		}
		for name, c := range map[string]struct {
			issue map[string]any
			ok    bool
		}{
			"not on the board": {map[string]any{"projectItems": items("PVT_9"), "parent": nil}, false},
			"on the board":     {map[string]any{"projectItems": items("PVT_9", "PVT_1"), "parent": nil}, true},
			"parent on the board": {map[string]any{"projectItems": items(), "parent": map[string]any{
				"repository": map[string]any{"nameWithOwner": "acme/ops"}, "projectItems": items("PVT_1")}}, true},
			"parent in the scope repo": {map[string]any{"projectItems": items(), "parent": map[string]any{
				"repository": map[string]any{"nameWithOwner": "Acme/Web"}, "projectItems": items()}}, true},
			"parent elsewhere": {map[string]any{"projectItems": items(), "parent": map[string]any{
				"repository": map[string]any{"nameWithOwner": "acme/ops"}, "projectItems": items("PVT_9")}}, false},
		} {
			f2.onGQL("IssueParent", scopeReply(c.issue))
			err := b.Comment(ctx, "ACME/api#3", "hi")
			if c.ok && err != nil || !c.ok && !errors.Is(err, ErrOutOfScope) {
				t.Fatalf("board, other repo, %s: %v", name, err)
			}
		}

		// a board with no scope.project: every write needs the issue (or its parent) on the board
		f3 := newFixture(t)
		f3.onGQL("Project", projectReply())
		f3.onGQL("IssueParent", scopeReply(map[string]any{"projectItems": items(), "parent": nil}))
		ob := newAdapter(t, f3, mode, map[string]string{"org": "acme", "board": "3"})
		if err := ob.Update(ctx, "acme/web#3", &title, nil); !errors.Is(err, ErrOutOfScope) {
			t.Fatalf("board only, not on it: %v", err)
		}
		if len(f3.find("PATCH /repos/acme/web/issues/3")) != 0 {
			t.Fatal("an out-of-scope write reached the API")
		}
	})
}

// gh not on PATH: the cli backend stands aside and the cred chain is asked.
func TestGHNotInstalledFallsBack(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := ghCommand
	ghCommand = "tq-test-no-such-gh"
	t.Cleanup(func() { ghCommand = prev })
	a, _ := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "github", Scope: repoScope(),
		Env: []string{"GH_CONFIG_DIR=" + t.TempDir()}})
	_, err := a.Check(context.Background())
	if !errors.Is(err, errCLIUnavailable) || !errors.Is(err, cred.ErrNeedsSession) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("%v", err)
	}
}

// An item at the watermark that the last call already returned, unchanged, is not returned
// again; one that is new at that same second still is.
func TestListChangedBoundaryNotRepeated(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		page := func(nodes ...any) reply {
			return data(map[string]any{"repository": map[string]any{"issues": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false}, "nodes": nodes}}})
		}
		f.onGQL("Issues", page(issue(4, "four", "2026-10-02T00:00:00Z")))
		a := newAdapter(t, f, mode, repoScope())
		ctx := context.Background()
		items, wm, err := a.ListChanged(ctx, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
		if err != nil || len(items) != 1 {
			t.Fatalf("%v %v", items, err)
		}
		items, wm2, err := a.ListChanged(ctx, wm)
		if err != nil || len(items) != 0 || !wm2.Equal(wm) {
			t.Fatalf("repeat: %+v %v %v", items, wm2, err)
		}
		f.onGQL("Issues", page(issue(4, "four", "2026-10-02T00:00:00Z"), issue(5, "five", "2026-10-02T00:00:00Z")))
		items, _, err = a.ListChanged(ctx, wm)
		if err != nil || len(items) != 1 || items[0].ID != "acme/web#5" {
			t.Fatalf("new at the boundary: %+v %v", items, err)
		}
		items, _, err = a.ListChanged(ctx, wm)
		if err != nil || len(items) != 0 {
			t.Fatalf("both seen: %+v %v", items, err)
		}
	})
}

// A parent with more sub-issues than the cap does not stop the sync: what was read is kept.
func TestSubIssueOverflowKeepsWhatWasRead(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	parent := issue(4, "epic", "2026-10-01T00:00:00Z", withChildren(issue(5, "c5", "2026-10-02T00:00:00Z", withParent("acme/web", 4, "tester"))))
	parent["subIssues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "s1"}
	f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{parent}}}}))
	f.onGQL("SubIssues", data(map[string]any{"node": map[string]any{"subIssues": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "again"},
		"nodes":    []any{issue(6, "c6", "2026-10-03T00:00:00Z", withParent("acme/web", 4, "tester"))}}}}))
	a := newAdapter(t, f, "http", repoScope())
	items, wm, err := a.ListChanged(context.Background(), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if fmt.Sprint(ids) != "[acme/web#4 acme/web#5 acme/web#6]" || !wm.Equal(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ids %v wm %v", ids, wm)
	}
	if n := len(f.find("SubIssues")); n != maxSubPages {
		t.Fatalf("sub-issue pages %d", n)
	}
}

func TestGHEnvDropsDebug(t *testing.T) {
	got := strings.Join(ghEnv([]string{"PATH=/bin", "GH_DEBUG=api", "DEBUG=1", "gh_debug=1"},
		[]string{"GH_CONFIG_DIR=/ws/gh", "GH_DEBUG=api"}), "\n")
	if strings.Contains(strings.ToUpper(got), "DEBUG") {
		t.Fatalf("debug kept: %v", got)
	}
}
