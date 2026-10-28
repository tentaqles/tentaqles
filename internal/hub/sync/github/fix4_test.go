package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
)

// serveNodes answers the batched node reads (IssueNodes, ChildNodes, SubIssueProbe) from these
// issues and their sub-issues, by node id; later calls add to what is served.
func serveNodes(f *fixture, issues ...map[string]any) {
	f.mu.Lock()
	if f.nodes == nil {
		f.nodes = map[string]map[string]any{}
	}
	byID := f.nodes
	f.mu.Unlock()
	var index func(m map[string]any)
	index = func(m map[string]any) {
		if id, _ := m["id"].(string); id != "" {
			if _, seen := byID[id]; !seen {
				byID[id] = m
			}
		}
		for _, k := range subNodes(m) {
			index(k)
		}
	}
	for _, m := range issues {
		index(m)
	}
	ids := func(vars map[string]any) []any {
		raw, _ := vars["ids"].([]any)
		return raw
	}
	full := func(vars map[string]any) reply {
		var out []any
		for _, id := range ids(vars) {
			if m, ok := byID[id.(string)]; ok {
				out = append(out, m)
			} else {
				out = append(out, nil)
			}
		}
		return data(map[string]any{"nodes": out})
	}
	f.onGQLFunc("IssueNodes", full)
	f.onGQLFunc("ChildNodes", full)
	f.onGQLFunc("SubIssueProbe", func(vars map[string]any) reply {
		var out []any
		for _, id := range ids(vars) {
			m, ok := byID[id.(string)]
			if !ok {
				out = append(out, nil)
				continue
			}
			var kids []any
			for _, k := range subNodes(m) {
				kids = append(kids, map[string]any{"id": k["id"], "number": k["number"], "updatedAt": k["updatedAt"], "repository": k["repository"]})
			}
			out = append(out, map[string]any{"id": id, "subIssues": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false}, "nodes": kids}})
		}
		return data(map[string]any{"nodes": out})
	})
}

func subNodes(m map[string]any) []map[string]any {
	s, _ := m["subIssues"].(map[string]any)
	switch n := s["nodes"].(type) {
	case []map[string]any:
		return n
	case []any:
		var out []map[string]any
		for _, x := range n {
			if mm, ok := x.(map[string]any); ok {
				out = append(out, mm)
			}
		}
		return out
	}
	return nil
}

// onBoard serves the light board pages (cursors p1, p2, ...) and the node reads for their issues.
func onBoard(f *fixture, pages ...[]any) {
	var replies []reply
	var issues []map[string]any
	for i, nodes := range pages {
		replies = append(replies, boardItemsReply(i < len(pages)-1, fmt.Sprintf("p%d", i+1), nodes...))
		for _, n := range nodes {
			if c, ok := n.(map[string]any)["content"].(map[string]any); ok && c["__typename"] == "Issue" {
				issues = append(issues, c)
			}
		}
	}
	f.onGQL("ProjectItems", replies...)
	serveNodes(f, issues...)
}

func idsOf(items []hubsync.Item) string {
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return fmt.Sprint(ids)
}

func varIDs(cs []call) []string {
	var out []string
	for _, c := range cs {
		raw, _ := c.Vars["ids"].([]any)
		for _, id := range raw {
			out = append(out, id.(string))
		}
	}
	slices.Sort(out)
	return out
}

// The board walk pages a light selection, then reads full fields (and sub-issues) only for the
// items changed since the watermark, in batches; an unchanged parent's sub-issues are probed by
// id and time, and only the changed ones are read in full.
func TestBoardWalkReadsOnlyWhatChanged(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		f.onGQL("Project", projectReply())
		late := issue(15, "late child", "2026-10-04T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 4))
		onBoard(f, []any{
			boardItem("PVTI_1", "2026-10-05T00:00:00Z", statusVal("Todo"), issue(1, "moved", "2026-09-01T00:00:00Z",
				withChildren(issue(11, "c11", "2026-10-02T00:00:00Z", withAssignee("acme-dev"), withParent("acme/web", 1))))),
			boardItem("PVTI_2", "2026-09-01T00:00:00Z", statusVal("Todo"), issue(2, "quiet leaf", "2026-09-01T00:00:00Z")),
			boardItem("PVTI_3", "2026-09-01T00:00:00Z", statusVal("Todo"), issue(3, "quiet parent", "2026-09-01T00:00:00Z",
				withChildren(issue(12, "c12", "2026-10-03T00:00:00Z", withParent("acme/web", 3)),
					issue(13, "c13", "2026-09-02T00:00:00Z", withParent("acme/web", 3))))),
			boardItem("PVTI_4", "2026-09-01T00:00:00Z", statusVal("Todo"), issue(4, "big quiet parent", "2026-09-01T00:00:00Z",
				withChildren(issue(14, "c14", "2026-09-05T00:00:00Z", withParent("acme/web", 4))))),
		}, []any{
			boardItem("PVTI_5", "2026-10-06T00:00:00Z", statusVal("In Progress"), issue(5, "changed leaf", "2026-10-06T00:00:00Z")),
		})
		serveNodes(f, late)
		// #4 has more sub-issues than the probe's first page
		f.onGQLFunc("SubIssueProbe", func(vars map[string]any) reply {
			var out []any
			for _, id := range vars["ids"].([]any) {
				switch id {
				case "I_1":
					out = append(out, map[string]any{"id": "I_1", "subIssues": map[string]any{"pageInfo": map[string]any{"hasNextPage": false},
						"nodes": []any{map[string]any{"id": "I_11", "number": 11, "updatedAt": "2026-10-02T00:00:00Z", "repository": map[string]any{"nameWithOwner": "acme/web"}}}}})
				case "I_3":
					out = append(out, map[string]any{"id": "I_3", "subIssues": map[string]any{"pageInfo": map[string]any{"hasNextPage": false},
						"nodes": []any{
							map[string]any{"id": "I_12", "number": 12, "updatedAt": "2026-10-03T00:00:00Z", "repository": map[string]any{"nameWithOwner": "acme/web"}},
							map[string]any{"id": "I_13", "number": 13, "updatedAt": "2026-09-02T00:00:00Z", "repository": map[string]any{"nameWithOwner": "acme/web"}},
						}}})
				case "I_4":
					out = append(out, map[string]any{"id": "I_4", "subIssues": map[string]any{"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "k1"},
						"nodes": []any{map[string]any{"id": "I_14", "number": 14, "updatedAt": "2026-09-05T00:00:00Z", "repository": map[string]any{"nameWithOwner": "acme/web"}}}}})
				default:
					out = append(out, nil)
				}
			}
			return data(map[string]any{"nodes": out})
		})
		f.onGQL("SubIssueIDs", data(map[string]any{"node": map[string]any{"subIssues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false},
			"nodes":    []any{map[string]any{"id": "I_15", "number": 15, "updatedAt": "2026-10-04T00:00:00Z", "repository": map[string]any{"nameWithOwner": "acme/web"}}}}}}))

		a := newAdapter(t, f, mode, boardScope())
		ctx := context.Background()
		since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		items, wm, err := a.ListChanged(ctx, since)
		if err != nil {
			t.Fatal(err)
		}
		if got := idsOf(items); got != "[acme/web#1 acme/web#11 acme/web#12 acme/web#15 acme/web#5]" {
			t.Fatalf("ids %s", got)
		}
		if it := items[0]; it.Status != "Todo" || it.Rev != "2026-10-05T00:00:00Z" || it.Body != "body of moved" {
			t.Fatalf("parent %+v", it)
		}
		if it := items[4]; it.Status != "In Progress" || it.Title != "changed leaf" {
			t.Fatalf("leaf %+v", it)
		}
		if !wm.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("watermark %v", wm)
		}
		if got := varIDs(f.find("IssueNodes")); fmt.Sprint(got) != "[I_1]" {
			t.Fatalf("full reads with sub-issues: %v", got)
		}
		if got := varIDs(f.find("ChildNodes")); fmt.Sprint(got) != "[I_12 I_15 I_5]" {
			t.Fatalf("full reads: %v", got)
		}
		if got := varIDs(f.find("SubIssueProbe")); fmt.Sprint(got) != "[I_3 I_4]" {
			t.Fatalf("probes: %v", got)
		}
		if s := f.find("SubIssueIDs"); len(s) != 1 || s[0].Vars["id"] != "I_4" || s[0].Vars["cursor"] != "k1" {
			t.Fatalf("probe pages %+v", s)
		}

		// the board page is light: no nested connections beyond the assignees, and it reports cost
		q := f.find("ProjectItems")[0].Query
		for _, heavy := range []string{"comments", "subIssues(", "labels(", "projectItems(", "issueFields", "body"} {
			if strings.Contains(q, heavy) {
				t.Fatalf("board page query has %q:\n%s", heavy, q)
			}
		}
		for _, op := range []string{"ProjectItems", "IssueNodes", "ChildNodes", "SubIssueProbe", "SubIssueIDs"} {
			if q := f.find(op)[0].Query; !strings.Contains(q, "rateLimit") || !strings.Contains(q, "cost") || !strings.Contains(q, "remaining") {
				t.Fatalf("%s does not ask for its rate limit cost", op)
			}
		}

		// nothing changed since: no full reads at all
		nFull, nChild := len(f.find("IssueNodes")), len(f.find("ChildNodes"))
		items, _, err = a.ListChanged(ctx, wm)
		if err != nil || len(items) != 0 {
			t.Fatalf("quiet tick: %+v %v", items, err)
		}
		if len(f.find("IssueNodes")) != nFull || len(f.find("ChildNodes")) != nChild {
			t.Fatal("a quiet tick read issues in full")
		}
	})
}

func TestRepoListAsksForRateLimit(t *testing.T) {
	isolate(t)
	f := newFixture(t)
	f.onGQL("Viewer", viewerReply())
	f.onGQL("Issues", data(map[string]any{"repository": map[string]any{"issues": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false}, "nodes": []any{}}}}))
	a := newAdapter(t, f, "http", repoScope())
	if _, _, err := a.ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if q := f.find("Issues")[0].Query; !strings.Contains(q, "rateLimit") {
		t.Fatal("Issues does not ask for its rate limit cost")
	}
}

// A list query that leaves less than a tenth of the hourly GraphQL budget stops the sync until
// the reset, so the user's own gh keeps working; the watermark does not move.
func TestListStopsBeforeTheBudgetRunsOut(t *testing.T) {
	each(t, func(t *testing.T, mode string) {
		fixed := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
		restore := setNow(func() time.Time { return fixed })
		defer restore()
		f := newFixture(t)
		f.onGQL("Viewer", viewerReply())
		f.onGQL("Project", projectReply())
		page := func(remaining int) reply {
			return data(map[string]any{
				"rateLimit": map[string]any{"cost": 1, "remaining": remaining, "limit": 5000, "resetAt": fixed.Add(10 * time.Minute).Format(time.RFC3339)},
				"node": map[string]any{"items": map[string]any{"pageInfo": map[string]any{"hasNextPage": false},
					"nodes": []any{boardItem("PVTI_5", "2026-10-06T00:00:00Z", statusVal("Todo"), issue(5, "leaf", "2026-10-06T00:00:00Z"))}}}})
		}
		f.onGQL("ProjectItems", page(120))
		serveNodes(f, issue(5, "leaf", "2026-10-06T00:00:00Z"))
		a := newAdapter(t, f, mode, boardScope())
		since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		items, wm, err := a.ListChanged(context.Background(), since)
		var rl *hubsync.RateLimitError
		if !errors.As(err, &rl) || rl.RetryAfter != 10*time.Minute || items != nil || !wm.Equal(since) {
			t.Fatalf("%v %v %T %v", items, wm, err, err)
		}
		f.onGQL("ProjectItems", page(4000))
		if items, _, err := a.ListChanged(context.Background(), since); err != nil || len(items) != 1 {
			t.Fatalf("budget left: %v %v", items, err)
		}
	})
}
