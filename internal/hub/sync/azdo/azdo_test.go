package azdo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// fakeADO is an in-memory Azure DevOps organisation "acme" with project "Platform": just enough of
// the REST API (connectionData, projects, workitemtypes, wiql, workitemsbatch, workitems,
// comments) for the adapter.
type fakeADO struct {
	t  *testing.T
	mu sync.Mutex

	items    map[int]map[string]any // id -> {"id","rev","fields","relations"}
	comments map[int][]map[string]any
	flat     []int    // ids the flat (assigned) WIQL query returns
	links    [][2]int // source -> target pairs the link WIQL queries return (filtered like ADO would)
	asOf     string   // the flat WIQL reply's asOf; empty sends none
	noAcct   bool     // connectionData carries no Account property (display name only)

	wantAuth string
	requests []string // "METHOD path"
	queries  []string // WIQL texts
	wiqlURLs []string
	patches  map[int][]map[string]any
	created  []map[string]any // create ops, keyed by type in "type"
	posted   map[int][]string // comment texts
	batch    []map[string]any // workitemsbatch bodies
	nextID   int

	hook func(w http.ResponseWriter, r *http.Request) bool // returns true when it handled the request
}

func newFake(t *testing.T) (*fakeADO, *httptest.Server) {
	f := &fakeADO{t: t, items: map[int]map[string]any{}, comments: map[int][]map[string]any{},
		patches: map[int][]map[string]any{}, posted: map[int][]string{}, wantAuth: "Bearer tok-acme", nextID: 100,
		asOf: "2030-01-01T00:00:00Z"}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeADO) addItem(id, rev int, changed string, fields map[string]any, relations ...map[string]any) {
	fields["System.ChangedDate"] = changed
	f.items[id] = map[string]any{"id": id, "rev": rev, "fields": fields, "relations": relations,
		"url": "https://dev.azure.test/acme/_apis/wit/workItems/" + strconv.Itoa(id)}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

var (
	reSourceIn    = regexp.MustCompile(`\[Source\]\.\[System\.Id\] IN \(([0-9, ]+)\)`)
	reTargetSince = regexp.MustCompile(`\[Target\]\.\[System\.ChangedDate\] >= '([^']+)'`)
	reItem        = regexp.MustCompile(`^/acme/Platform/_apis/wit/workitems/(\d+)$`)
	reComments    = regexp.MustCompile(`^/acme/Platform/_apis/wit/workitems/(\d+)/comments$`)
	reCreate      = regexp.MustCompile(`^/acme/Platform/_apis/wit/workitems/\$(.+)$`)
)

func (f *fakeADO) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if f.hook != nil && f.hook(w, r) {
		return
	}
	if got := r.Header.Get("Authorization"); got != f.wantAuth {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	if r.URL.Path != "/acme/_apis/connectionData" && q.Get("api-version") == "" {
		f.t.Errorf("%s %s without api-version", r.Method, r.URL)
	}
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	switch {
	case path == "/acme/_apis/connectionData":
		props := map[string]any{"Account": map[string]any{"$type": "System.String", "$value": "tester@acme.test"}}
		if f.noAcct {
			props = map[string]any{}
		}
		writeJSON(w, map[string]any{"authenticatedUser": map[string]any{
			"id": "u1", "providerDisplayName": "Tester", "properties": props}})
	case path == "/acme/_apis/projects/Platform":
		writeJSON(w, map[string]any{"id": "p1", "name": "Platform"})
	case path == "/acme/Platform/_apis/wit/workitemtypes":
		writeJSON(w, map[string]any{"count": 3, "value": []any{
			map[string]any{"name": "User Story", "states": []any{
				map[string]any{"name": "New", "category": "Proposed"},
				map[string]any{"name": "Active", "category": "InProgress"},
				map[string]any{"name": "Resolved", "category": "Resolved"},
				map[string]any{"name": "Closed", "category": "Completed"},
				map[string]any{"name": "Removed", "category": "Removed"},
			}},
			map[string]any{"name": "Task", "states": []any{
				map[string]any{"name": "New", "category": "Proposed"},
				map[string]any{"name": "In Review", "category": "InProgress"},
				map[string]any{"name": "Done", "category": "Completed"},
			}},
			map[string]any{"name": "Old Thing", "isDisabled": true, "states": []any{
				map[string]any{"name": "Zombie", "category": "Proposed"},
			}},
		}})
	case strings.HasSuffix(path, "/_apis/wit/wiql") && r.Method == "POST":
		var in struct{ Query string }
		json.Unmarshal(body, &in)
		f.queries = append(f.queries, in.Query)
		f.wiqlURLs = append(f.wiqlURLs, r.URL.String())
		// Like ADO, $top silently truncates the reply rows.
		top := -1
		if v := q.Get("$top"); v != "" {
			top, _ = strconv.Atoi(v)
		}
		if strings.Contains(in.Query, "WorkItemLinks") {
			// Filter like ADO: a Source id list, and a Target ChangedDate floor.
			var srcIn map[int]bool
			if m := reSourceIn.FindStringSubmatch(in.Query); m != nil {
				srcIn = map[int]bool{}
				for _, s := range strings.Split(m[1], ",") {
					n, _ := strconv.Atoi(strings.TrimSpace(s))
					srcIn[n] = true
				}
			}
			var floor time.Time
			if m := reTargetSince.FindStringSubmatch(in.Query); m != nil {
				floor, _ = time.Parse(time.RFC3339, m[1])
			}
			rels := []any{}
			for _, l := range f.links {
				if srcIn != nil && !srcIn[l[0]] {
					continue
				}
				if tgt, ok := f.items[l[1]]; ok && !floor.IsZero() {
					ch, _ := time.Parse(time.RFC3339Nano, tgt["fields"].(map[string]any)["System.ChangedDate"].(string))
					if ch.Before(floor) {
						continue
					}
				}
				rels = append(rels, map[string]any{"rel": nil, "source": nil, "target": map[string]any{"id": l[0]}})
				rels = append(rels, map[string]any{"rel": "System.LinkTypes.Hierarchy-Forward",
					"source": map[string]any{"id": l[0]}, "target": map[string]any{"id": l[1]}})
			}
			if top >= 0 && len(rels) > top {
				rels = rels[:top]
			}
			writeJSON(w, map[string]any{"queryType": "oneHop", "workItemRelations": rels})
			return
		}
		wis := []any{}
		for _, id := range f.flat {
			wis = append(wis, map[string]any{"id": id})
		}
		if top >= 0 && len(wis) > top {
			wis = wis[:top]
		}
		res := map[string]any{"queryType": "flat", "workItems": wis}
		if f.asOf != "" {
			res["asOf"] = f.asOf
		}
		writeJSON(w, res)
	case path == "/acme/Platform/_apis/wit/workitemsbatch" && r.Method == "POST":
		var in map[string]any
		json.Unmarshal(body, &in)
		f.batch = append(f.batch, in)
		out := []any{}
		for _, v := range in["ids"].([]any) {
			if it, ok := f.items[int(v.(float64))]; ok {
				out = append(out, it)
			} else {
				out = append(out, nil)
			}
		}
		writeJSON(w, map[string]any{"count": len(out), "value": out})
	case reComments.MatchString(path):
		id, _ := strconv.Atoi(reComments.FindStringSubmatch(path)[1])
		if _, ok := f.items[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			var in struct{ Text string }
			json.Unmarshal(body, &in)
			f.posted[id] = append(f.posted[id], in.Text)
			writeJSON(w, map[string]any{"id": 9, "text": in.Text})
			return
		}
		if q.Get("order") != "desc" || q.Get("$top") != "20" {
			f.t.Errorf("comments query %q", r.URL.RawQuery)
		}
		writeJSON(w, map[string]any{"count": len(f.comments[id]), "comments": f.comments[id]})
	case reCreate.MatchString(path) && r.Method == "POST":
		if ct := r.Header.Get("Content-Type"); ct != "application/json-patch+json" {
			f.t.Errorf("create content type %q", ct)
		}
		typ, _ := url.PathUnescape(reCreate.FindStringSubmatch(path)[1])
		var ops []map[string]any
		json.Unmarshal(body, &ops)
		f.created = append(f.created, map[string]any{"type": typ, "ops": ops})
		f.nextID++
		fields := map[string]any{"System.WorkItemType": typ, "System.State": "New"}
		var rels []map[string]any
		for _, op := range ops {
			p := op["path"].(string)
			if strings.HasPrefix(p, "/fields/") {
				fields[strings.TrimPrefix(p, "/fields/")] = op["value"]
			}
			if p == "/relations/-" {
				rels = append(rels, op["value"].(map[string]any))
			}
		}
		f.addItem(f.nextID, 1, "2026-10-27T12:00:00.5Z", fields, rels...)
		writeJSON(w, f.items[f.nextID])
	case reItem.MatchString(path):
		id, _ := strconv.Atoi(reItem.FindStringSubmatch(path)[1])
		it, ok := f.items[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"message": "TF401232: Work item " + strconv.Itoa(id) + " does not exist"})
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, it)
		case "PATCH":
			if ct := r.Header.Get("Content-Type"); ct != "application/json-patch+json" {
				f.t.Errorf("patch content type %q", ct)
			}
			var ops []map[string]any
			json.Unmarshal(body, &ops)
			f.patches[id] = append(f.patches[id], ops...)
			fields := it["fields"].(map[string]any)
			for _, op := range ops {
				p := op["path"].(string)
				if op["op"] == "test" && p == "/rev" && int(op["value"].(float64)) != it["rev"].(int) {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
				if strings.HasPrefix(p, "/fields/") {
					fields[strings.TrimPrefix(p, "/fields/")] = op["value"]
				}
			}
			it["rev"] = it["rev"].(int) + 1
			writeJSON(w, it)
		}
	default:
		f.t.Errorf("unhandled %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func newAdapter(t *testing.T, srv *httptest.Server, mod ...func(*hubsync.Config)) *Adapter {
	t.Helper()
	isolate(t)
	cfg := hubsync.Config{Workspace: "acme", Provider: Provider, AssignedTo: "me", BaseURL: srv.URL,
		Scope:     map[string]string{"org": "acme", "project": "Platform"},
		Transport: cred.Static(cred.SourceCLI, cred.Token{Value: "tok-acme"})}
	for _, m := range mod {
		m(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func seed(f *fakeADO) {
	f.addItem(1, 4, "2026-10-27T10:00:00.123Z", map[string]any{
		"System.Title": "Login page", "System.WorkItemType": "User Story", "System.State": "Active",
		"System.Description":                       "<div>Build the <b>login</b> page</div>",
		"System.AssignedTo":                        map[string]any{"displayName": "Tester", "uniqueName": "tester@acme.test"},
		"System.Tags":                              "frontend; agent-ready",
		"System.CommentCount":                      3,
		"Microsoft.VSTS.Common.AcceptanceCriteria": "<ul><li>form posts</li><li>errors show</li></ul>",
	}, map[string]any{"rel": "AttachedFile", "url": "https://dev.azure.test/acme/_apis/wit/attachments/a1",
		"attributes": map[string]any{"name": "mock.png"}})
	f.comments[1] = []map[string]any{
		{"id": 3, "text": "<p>newest</p>", "createdBy": map[string]any{"displayName": "Tester", "uniqueName": "tester@acme.test"}, "createdDate": "2026-10-27T09:00:00Z"},
		{"id": 2, "text": "gone", "isDeleted": true, "createdDate": "2026-10-26T09:00:00Z"},
		{"id": 1, "text": "oldest", "createdBy": map[string]any{"displayName": "Acme Bot"}, "createdDate": "2026-10-25T09:00:00Z"},
	}
	f.addItem(2, 1, "2026-10-27T11:00:00Z", map[string]any{
		"System.Title": "Old bug", "System.WorkItemType": "Bug", "System.State": "Closed",
		"System.AssignedTo": map[string]any{"displayName": "Tester", "uniqueName": "tester@acme.test"},
	})
	f.addItem(3, 2, "2026-10-27T09:30:00Z", map[string]any{
		"System.Title": "Child task", "System.WorkItemType": "Task", "System.State": "In Review",
		"System.Parent": 1,
	}, map[string]any{"rel": "System.LinkTypes.Hierarchy-Reverse", "url": "https://dev.azure.test/acme/_apis/wit/workItems/1"})
	f.flat = []int{1, 2}
	f.links = [][2]int{{1, 3}}
}

func TestRegisteredAsAzureDevOps(t *testing.T) {
	if _, ok := hubsync.Lookup("azure-devops"); !ok {
		t.Fatal("azure-devops adapter is not registered")
	}
	_, srv := newFake(t)
	a, err := hubsync.New(hubsync.Config{Provider: "azure-devops", BaseURL: srv.URL, Scope: map[string]string{"org": "acme", "project": "Platform"}})
	if err != nil || a.Name() != "azure-devops" {
		t.Fatal(a, err)
	}
}

func TestNewNeedsOrgAndProject(t *testing.T) {
	for _, scope := range []map[string]string{nil, {"org": "acme"}, {"project": "Platform"}} {
		if _, err := New(hubsync.Config{Provider: Provider, Scope: scope}); err == nil {
			t.Fatalf("scope %v: want an error", scope)
		}
	}
}

func TestCheckReturnsTheAccountAndVerifiesTheProject(t *testing.T) {
	f, srv := newFake(t)
	acct, err := newAdapter(t, srv).Check(context.Background())
	if err != nil || acct != "tester@acme.test" {
		t.Fatal(acct, err)
	}
	if !strings.Contains(strings.Join(f.requests, ","), "GET /acme/_apis/projects/Platform") {
		t.Fatalf("project not verified: %v", f.requests)
	}
}

func TestStatusesMapCategoriesToLaneKinds(t *testing.T) {
	_, srv := newFake(t)
	st, err := newAdapter(t, srv).Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range st {
		if _, dup := got[s.Name]; dup {
			t.Fatalf("duplicate status %q", s.Name)
		}
		got[s.Name] = s.Category
	}
	want := map[string]string{"New": "todo", "Active": "active", "Resolved": "review", "Closed": "done",
		"Removed": "done", "In Review": "active", "Done": "done"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestListChangedAssignedPlusChildren(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	since := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	items, mark, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 3 {
		t.Fatalf("queries %q", f.queries)
	}
	flat, link, kids := f.queries[0], f.queries[1], f.queries[2]
	// The children of the items that came back, whatever their own ChangedDate.
	for _, s := range []string{"FROM WorkItemLinks", "[Source].[System.Id] IN (1,2)", "System.LinkTypes.Hierarchy-Forward"} {
		if !strings.Contains(kids, s) {
			t.Fatalf("children query lacks %q: %s", s, kids)
		}
	}
	if strings.Contains(kids, "[Target].[System.ChangedDate]") {
		t.Fatalf("children query filters the targets by date: %s", kids)
	}
	for _, s := range []string{"FROM WorkItems", "[System.AssignedTo] = @Me", "[System.ChangedDate] >= '2026-10-27T08:00:00Z'", "@project"} {
		if !strings.Contains(flat, s) {
			t.Fatalf("flat query lacks %q: %s", s, flat)
		}
	}
	for _, s := range []string{"FROM WorkItemLinks", "[Source].[System.AssignedTo] = @Me", "System.LinkTypes.Hierarchy-Forward", "[Target].[System.ChangedDate] >= '2026-10-27T08:00:00Z'"} {
		if !strings.Contains(link, s) {
			t.Fatalf("link query lacks %q: %s", s, link)
		}
	}
	if !strings.Contains(f.wiqlURLs[0], "timePrecision=true") {
		t.Fatalf("wiql url %s", f.wiqlURLs[0])
	}
	if f.batch[0]["$expand"] != "Relations" {
		t.Fatalf("batch %v", f.batch[0])
	}
	if !mark.Equal(time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("watermark %v", mark)
	}
	if len(items) != 3 {
		t.Fatalf("items %+v", items)
	}
	// Oldest change first.
	if items[0].ID != "3" || items[1].ID != "1" || items[2].ID != "2" {
		t.Fatalf("order %s %s %s", items[0].ID, items[1].ID, items[2].ID)
	}
	one := items[1]
	if one.Key != "#1" || one.Title != "Login page" || one.Status != "Active" || one.StatusCategory != "active" ||
		one.Body != "Build the login page" || one.Assignee != "tester@acme.test" || one.Rev != "4" {
		t.Fatalf("item 1 %+v", one)
	}
	if one.URL != srv.URL+"/acme/Platform/_workitems/edit/1" {
		t.Fatalf("url %s", one.URL)
	}
	if one.Acceptance != "- form posts\n- errors show" {
		t.Fatalf("acceptance %q", one.Acceptance)
	}
	if fmt.Sprint(one.Labels) != "[frontend agent-ready]" {
		t.Fatalf("labels %v", one.Labels)
	}
	if len(one.Attachments) != 1 || one.Attachments[0].Name != "mock.png" {
		t.Fatalf("attachments %+v", one.Attachments)
	}
	if len(one.Comments) != 2 || one.Comments[0].Body != "newest" || one.Comments[0].Author != "Tester" || one.Comments[1].ID != "1" {
		t.Fatalf("comments %+v", one.Comments)
	}
	if !one.UpdatedAt.Equal(time.Date(2026, 10, 27, 10, 0, 0, 123e6, time.UTC)) {
		t.Fatalf("updated %v", one.UpdatedAt)
	}
	child := items[0]
	if child.ParentID != "1" || child.StatusCategory != "active" {
		t.Fatalf("child %+v", child)
	}
	if items[2].StatusCategory != "done" { // a Bug: the type is not in the fixture, the name decides
		t.Fatalf("bug %+v", items[2])
	}
	// No comments fetched for items without any.
	for _, r := range f.requests {
		if strings.Contains(r, "/workitems/2/comments") || strings.Contains(r, "/workitems/3/comments") {
			t.Fatalf("needless comment fetch %s", r)
		}
	}
}

func TestListChangedReturnsOldChildrenOfAChangedParent(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	// Item 1 was just assigned to tester; its task 4 has not changed for weeks.
	f.addItem(4, 1, "2026-10-01T09:00:00Z", map[string]any{
		"System.Title": "Old task", "System.WorkItemType": "Task", "System.State": "New", "System.Parent": 1,
	})
	f.links = append(f.links, [2]int{1, 4})
	items, mark, err := newAdapter(t, srv).ListChanged(context.Background(), time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, it := range items {
		ids[it.ID] = true
	}
	if !ids["4"] || !ids["3"] || len(items) != 4 {
		t.Fatalf("items %v", ids)
	}
	if !mark.Equal(time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("watermark %v", mark)
	}
}

func TestListChangedWatermarkStopsAtTheQueryTime(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	// The flat query ran at 10:30; item 2 changed at 11:00, after it, and came back from the batch.
	f.asOf = "2026-10-27T10:30:00.25Z"
	since := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	a := newAdapter(t, srv)
	_, mark, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(time.Date(2026, 10, 27, 10, 30, 0, 250e6, time.UTC)) {
		t.Fatalf("watermark %v", mark)
	}
	// No asOf in the reply: the local time the query was sent, less a skew, caps it.
	f.asOf = ""
	a.now = func() time.Time { return time.Date(2026, 10, 27, 10, 45, 0, 0, time.UTC) }
	_, mark, err = a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if !mark.Equal(time.Date(2026, 10, 27, 10, 45, 0, 0, time.UTC).Add(-clockSkew)) {
		t.Fatalf("watermark without asOf %v", mark)
	}
	// The cap never moves the mark back before since.
	f.asOf = "2026-10-27T07:00:00Z"
	_, mark, err = a.ListChanged(context.Background(), since)
	if err != nil || !mark.Equal(since) {
		t.Fatalf("watermark %v %v", mark, err)
	}
}

func TestListChangedEmptyKeepsTheWatermark(t *testing.T) {
	_, srv := newFake(t)
	since := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	items, mark, err := newAdapter(t, srv).ListChanged(context.Background(), since)
	if err != nil || len(items) != 0 || !mark.Equal(since) {
		t.Fatal(items, mark, err)
	}
}

func TestListChangedAnyoneAndTeamScope(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv, func(c *hubsync.Config) {
		c.AssignedTo = "anyone"
		c.Scope["board"] = "Platform Team"
	})
	if _, _, err := a.ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if len(f.queries) != 1 || strings.Contains(f.queries[0], "@Me") || strings.Contains(f.queries[0], "ChangedDate] >=") {
		t.Fatalf("queries %q", f.queries)
	}
	if !strings.HasPrefix(f.wiqlURLs[0], "/acme/Platform/Platform%20Team/_apis/wit/wiql") {
		t.Fatalf("team route %s", f.wiqlURLs[0])
	}
}

func TestGetAndNotFound(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	it, err := a.Get(context.Background(), "3")
	if err != nil || it.Title != "Child task" || it.ParentID != "1" {
		t.Fatal(it, err)
	}
	_, err = a.Get(context.Background(), "77")
	if !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	for _, bad := range []string{"", "1/../2", "abc", "-1"} {
		if _, err := a.Get(context.Background(), bad); err == nil {
			t.Fatalf("id %q accepted", bad)
		}
	}
}

func TestMoveCommentUpdate(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	ctx := context.Background()
	if err := a.Move(ctx, "1", "Resolved"); err != nil {
		t.Fatal(err)
	}
	if p := f.patches[1]; len(p) != 1 || p[0]["op"] != "add" || p[0]["path"] != "/fields/System.State" || p[0]["value"] != "Resolved" {
		t.Fatalf("move %v", p)
	}
	if err := a.Comment(ctx, "1", "**Done.** PR [1](https://example.test/pr/1)"); err != nil {
		t.Fatal(err)
	}
	if got := f.posted[1]; len(got) != 1 || got[0] != `<p><strong>Done.</strong> PR <a href="https://example.test/pr/1">1</a></p>` {
		t.Fatalf("comment %q", got)
	}
	title := "Login page v2"
	if err := a.Update(ctx, "1", &title, nil); err != nil {
		t.Fatal(err)
	}
	if p := f.patches[1][1]; p["path"] != "/fields/System.Title" || p["value"] != title || len(f.patches[1]) != 2 {
		t.Fatalf("update %v", f.patches[1])
	}
	body := "- one"
	if err := a.Update(ctx, "1", nil, &body); err != nil {
		t.Fatal(err)
	}
	if p := f.patches[1][2]; p["path"] != "/fields/System.Description" || p["value"] != "<ul><li>one</li></ul>" {
		t.Fatalf("update body %v", f.patches[1])
	}
	n := len(f.requests)
	if err := a.Update(ctx, "1", nil, nil); err != nil || len(f.requests) != n {
		t.Fatalf("an empty update must not call the tracker: %v", err)
	}
	for _, blank := range []string{"", "  "} {
		if err := a.Update(ctx, "1", &blank, nil); err == nil || !strings.Contains(err.Error(), "title") || len(f.requests) != n {
			t.Fatalf("a blank title must fail before any call: %v", err)
		}
	}
	if err := a.Move(ctx, "77", "Closed"); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("move missing %v", err)
	}
}

func TestCreateChildAssignedToMe(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	it, err := a.Create(context.Background(), hubsync.NewItem{Title: "Write tests", Body: "Cover **login**",
		Acceptance: "- green", ParentID: "1", Labels: []string{"agent-ready", "hub"}})
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "101" || it.ParentID != "1" || it.Title != "Write tests" || it.StatusCategory != "todo" {
		t.Fatalf("created %+v", it)
	}
	c := f.created[0]
	if c["type"] != "Task" {
		t.Fatalf("type %v", c["type"])
	}
	ops := map[string]any{}
	for _, op := range c["ops"].([]map[string]any) {
		ops[op["path"].(string)] = op["value"]
	}
	if ops["/fields/System.Title"] != "Write tests" || ops["/fields/System.Description"] != "<p>Cover <strong>login</strong></p>" ||
		ops["/fields/Microsoft.VSTS.Common.AcceptanceCriteria"] != "<ul><li>green</li></ul>" ||
		ops["/fields/System.Tags"] != "agent-ready; hub" || ops["/fields/System.AssignedTo"] != "tester@acme.test" {
		t.Fatalf("ops %v", ops)
	}
	rel := ops["/relations/-"].(map[string]any)
	if rel["rel"] != "System.LinkTypes.Hierarchy-Reverse" || rel["url"] != srv.URL+"/acme/_apis/wit/workItems/1" {
		t.Fatalf("relation %v", rel)
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{}); err == nil {
		t.Fatal("a create without a title must fail")
	}
}

func TestCreateTypeFromInputOrScope(t *testing.T) {
	f, srv := newFake(t)
	a := newAdapter(t, srv, func(c *hubsync.Config) { c.AssignedTo = "anyone"; c.Scope["type"] = "User Story" })
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "y", Type: "Product Backlog Item"}); err != nil {
		t.Fatal(err)
	}
	if f.created[0]["type"] != "User Story" || f.created[1]["type"] != "Product Backlog Item" {
		t.Fatalf("types %v %v", f.created[0]["type"], f.created[1]["type"])
	}
	for _, op := range f.created[0]["ops"].([]map[string]any) {
		if op["path"] == "/fields/System.AssignedTo" {
			t.Fatal("assigned_to anyone must not assign")
		}
	}
}

func TestSetLabelsMergesWithARevTest(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	a := newAdapter(t, srv)
	if err := a.SetLabels(context.Background(), "1", []string{"hub", "Frontend"}, []string{"agent-ready"}); err != nil {
		t.Fatal(err)
	}
	p := f.patches[1]
	if len(p) != 2 || p[0]["op"] != "test" || p[0]["path"] != "/rev" || p[0]["value"] != float64(4) ||
		p[1]["path"] != "/fields/System.Tags" || p[1]["value"] != "frontend; hub" {
		t.Fatalf("patch %v", p)
	}
	n := len(f.patches[1])
	if err := a.SetLabels(context.Background(), "1", []string{"hub"}, nil); err != nil || len(f.patches[1]) != n {
		t.Fatalf("a no-op label change must not patch: %v %v", err, f.patches[1])
	}
}

func TestRateLimit429HonoursRetryAfter(t *testing.T) {
	f, srv := newFake(t)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	_, err := newAdapter(t, srv).Get(context.Background(), "1")
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second || rl.Provider != Provider {
		t.Fatalf("err %v", err)
	}
}

func TestRateLimitExhaustedBudgetBlocksTheNextCall(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	reset := time.Now().Add(30 * time.Second).Unix()
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/comments") { // the last request Get makes
			return false
		}
		w.Header().Set("X-RateLimit-Resource", "Core")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		return false
	}
	a := newAdapter(t, srv)
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	n := len(f.requests)
	_, err := a.Get(context.Background(), "1")
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 || rl.RetryAfter > 31*time.Second {
		t.Fatalf("err %v", err)
	}
	if len(f.requests) != n {
		t.Fatal("a blocked call must not reach the tracker")
	}
}

func TestRetryAfterOnSuccessDelaysTheNextCall(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/comments") {
			return false
		}
		w.Header().Set("Retry-After", "5")
		w.Header().Set("X-RateLimit-Delay", "1.5")
		return false
	}
	a := newAdapter(t, srv)
	var slept []time.Duration
	clock := time.Now()
	a.now = func() time.Time { return clock }
	a.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		clock = clock.Add(d)
		return nil
	}
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	// A short delay is waited out, not turned into an error.
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatalf("err %v", err)
	}
	if len(slept) != 1 || slept[0] <= 4*time.Second || slept[0] > 5*time.Second {
		t.Fatalf("slept %v", slept)
	}
}

func TestLongRetryAfterFailsFast(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			w.Header().Set("Retry-After", "40")
		}
		return false
	}
	a := newAdapter(t, srv)
	a.sleep = func(ctx context.Context, d time.Duration) error { t.Errorf("slept %v", d); return nil }
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	var rl *hubsync.RateLimitError
	if _, err := a.Get(context.Background(), "1"); !errors.As(err, &rl) || rl.RetryAfter < 30*time.Second {
		t.Fatalf("err %v", err)
	}
}

func TestShortWaitPastTheDeadlineFailsFast(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			w.Header().Set("Retry-After", "5")
		}
		return false
	}
	a := newAdapter(t, srv)
	a.sleep = func(ctx context.Context, d time.Duration) error { t.Errorf("slept %v", d); return nil }
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var rl *hubsync.RateLimitError
	if _, err := a.Get(ctx, "1"); !errors.As(err, &rl) {
		t.Fatalf("err %v", err)
	}
}

func TestSleepCtxWaitsOrStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestAHungRequestTimesOut(t *testing.T) {
	f, srv := newFake(t)
	release := make(chan struct{})
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-r.Context().Done():
		case <-release:
		case <-time.After(5 * time.Second):
		}
		return true
	}
	a := newAdapter(t, srv)
	a.timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := a.Get(context.Background(), "1")
	close(release)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	b, err := New(hubsync.Config{Provider: Provider, Scope: map[string]string{"org": "acme", "project": "Platform"}})
	if err != nil || b.timeout != requestTimeout || requestTimeout <= 0 || b.sleep == nil {
		t.Fatalf("defaults %v %v", b.timeout, err)
	}
}

func TestUnauthorizedNamesTheSourceNeverTheToken(t *testing.T) {
	f, srv := newFake(t)
	f.wantAuth = "Bearer other"
	_, err := newAdapter(t, srv).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cli") || strings.Contains(err.Error(), "tok-acme") {
		t.Fatalf("err %v", err)
	}
	// ADO answers a bad PAT with 203 and a sign-in page.
	f.hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNonAuthoritativeInfo)
		io.WriteString(w, "<html>sign in</html>")
		return true
	}
	_, err = newAdapter(t, srv).Get(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("203: %v", err)
	}
}

func TestPATFromKeychainGoesAsBasic(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	f.wantAuth = "Basic " + base64.StdEncoding.EncodeToString([]byte(":pat-acme"))
	a := newAdapter(t, srv, func(c *hubsync.Config) {
		c.Transport = cred.Static(cred.SourceKeychain, cred.Token{Value: "pat-acme"})
	})
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	// A JWT (an Entra token kept in the keychain or env) stays a bearer token.
	jwt := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ0ZXN0ZXIifQ.sig"
	f.wantAuth = "Bearer " + jwt
	a = newAdapter(t, srv, func(c *hubsync.Config) {
		c.Transport = cred.Static(cred.SourceEnv, cred.Token{Value: jwt})
	})
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
}

func TestResolvesTheCredentialLazily(t *testing.T) {
	f, srv := newFake(t)
	seed(f)
	var got cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(ctx context.Context, r cred.Request) (cred.Transport, error) {
		got = r
		return cred.Static(cred.SourceCLI, cred.Token{Value: "tok-acme"}), nil
	}))
	t.Cleanup(restore)
	a := newAdapter(t, srv, func(c *hubsync.Config) { c.Transport = nil; c.EnvVar = "ACME_ADO_TOKEN" })
	if _, err := a.Get(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if got.Provider != Provider || got.Workspace != "acme" || got.EnvVar != "ACME_ADO_TOKEN" {
		t.Fatalf("request %+v", got)
	}
	// No credential at all: the resolver's error comes back as it is (needs-session).
	restore2 := cred.SetDefault(nil)
	t.Cleanup(restore2)
	b := newAdapter(t, srv, func(c *hubsync.Config) { c.Transport = nil })
	if _, err := b.Get(context.Background(), "1"); !errors.Is(err, cred.ErrNeedsSession) {
		t.Fatalf("err %v", err)
	}
}
