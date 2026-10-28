package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	gosync "sync"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

const (
	teamID   = "11111111-1111-4111-8111-111111111111"
	issueID  = "22222222-2222-4222-8222-222222222222"
	parentID = "33333333-3333-4333-8333-333333333333"
	childID  = "44444444-4444-4444-8444-444444444444"
	testKey  = "lin_api_testvalue"
)

// call is one GraphQL request the fake saw.
type call struct {
	Op   string
	Auth string
	Vars map[string]any
}

// reply is a canned response: status (0 = 200), headers, body.
type reply struct {
	Status int
	Header map[string]string
	Body   string
}

// fake is a recorded-fixture Linear GraphQL endpoint keyed by operation name.
type fake struct {
	t       *testing.T
	mu      gosync.Mutex
	calls   []call
	replies map[string][]reply // op -> replies in order; the last one repeats
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{t: t, replies: map[string][]reply{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fake) on(op string, rs ...reply) { f.replies[op] = append(f.replies[op], rs...) }

func (f *fake) ok(op, data string) { f.on(op, reply{Body: `{"data":` + data + `}`}) }

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
		http.Error(w, "bad route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "content type "+ct, http.StatusBadRequest)
		return
	}
	var req struct {
		Query         string         `json:"query"`
		OperationName string         `json:"operationName"`
		Variables     map[string]any `json:"variables"`
	}
	b, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(b, &req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.Query, req.OperationName) {
		http.Error(w, "operation name not in query", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{Op: req.OperationName, Auth: r.Header.Get("Authorization"), Vars: req.Variables})
	rs := f.replies[req.OperationName]
	var rep reply
	switch {
	case len(rs) == 0:
		f.mu.Unlock()
		f.t.Errorf("unexpected operation %s", req.OperationName)
		http.Error(w, "no fixture for "+req.OperationName, http.StatusInternalServerError)
		return
	case len(rs) == 1:
		rep = rs[0]
	default:
		rep = rs[0]
		f.replies[req.OperationName] = rs[1:]
	}
	f.mu.Unlock()
	for k, v := range rep.Header {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	if rep.Status != 0 {
		w.WriteHeader(rep.Status)
	}
	io.WriteString(w, rep.Body)
}

func (f *fake) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Op)
	}
	return out
}

func (f *fake) last(op string) call {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Op == op {
			return f.calls[i]
		}
	}
	f.t.Fatalf("no %s call (calls: %v)", op, f.calls)
	return call{}
}

func newAdapter(t *testing.T, srv *httptest.Server, scope map[string]string, assigned string) *Adapter {
	t.Helper()
	a, err := hubsync.New(hubsync.Config{
		Workspace: "acme", Provider: "linear", Scope: scope, AssignedTo: assigned,
		Transport: cred.Static(cred.SourceKeychain, cred.Token{Value: testKey}),
		BaseURL:   srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a.(*Adapter)
}

const teamFixture = `{"teams":{"nodes":[{"id":"` + teamID + `","key":"ACME","name":"Acme Platform"}]}}`

const statesFixture = `{"team":{"id":"` + teamID + `","states":{"nodes":[
 {"id":"s-done","name":"Done","type":"completed","position":4},
 {"id":"s-triage","name":"Triage","type":"triage","position":0},
 {"id":"s-backlog","name":"Backlog","type":"backlog","position":1},
 {"id":"s-todo","name":"Todo","type":"unstarted","position":2},
 {"id":"s-prog","name":"In Progress","type":"started","position":3},
 {"id":"s-review","name":"In Review","type":"started","position":3.5},
 {"id":"s-cancel","name":"Canceled","type":"canceled","position":5}]}}}`

func issueJSON(id, ident, title, updated string, extra string) string {
	s := `{"id":"` + id + `","identifier":"` + ident + `","url":"https://linear.app/acme/issue/` + ident + `","title":"` + title + `",
 "description":"Body of ` + ident + `","updatedAt":"` + updated + `",
 "state":{"id":"s-prog","name":"In Progress","type":"started"},
 "assignee":{"id":"u1","name":"tester","displayName":"tester","email":"tester@acme.test"},
 "parent":null,
 "team":{"id":"` + teamID + `","key":"ACME"},
 "labels":{"nodes":[{"id":"l-bug","name":"bug"}]},
 "comments":{"nodes":[
   {"id":"c1","body":"first","createdAt":"2026-10-20T10:00:00.000Z","user":{"name":"tester","displayName":"tester"}},
   {"id":"c2","body":"second","createdAt":"2026-10-21T10:00:00.000Z","user":{"name":"tester","displayName":"tester"}}]},
 "attachments":{"nodes":[{"title":"spec.pdf","url":"https://acme.test/spec.pdf"}]}`
	if extra != "" {
		s += "," + extra
	}
	return s + "}"
}

func TestRegistered(t *testing.T) {
	if !slices.Contains(hubsync.Registered(), "linear") {
		t.Fatalf("linear not registered: %v", hubsync.Registered())
	}
	if _, ok := cred.CLIHookFor("linear"); ok {
		t.Fatal("linear has no CLI; no cli hook must be registered")
	}
}

func TestCheckSendsRawKey(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearViewer", `{"viewer":{"id":"u1","name":"tester","displayName":"tester","email":"tester@acme.test"}}`)
	f.ok("LinearTeam", teamFixture)
	a := newAdapter(t, srv, map[string]string{"org": "acme"}, "me")
	acct, err := a.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if acct != "tester@acme.test" {
		t.Fatalf("account %q", acct)
	}
	if got := f.last("LinearViewer").Auth; got != testKey {
		t.Fatalf("Authorization %q: a personal API key goes raw, no scheme", got)
	}
	if v := f.last("LinearTeam").Vars["team"]; v != "acme" {
		t.Fatalf("team lookup %v", v)
	}
	if a.Name() != "linear" {
		t.Fatal(a.Name())
	}
}

func TestAuthHeaderSchemes(t *testing.T) {
	for _, tc := range []struct {
		tok  cred.Token
		want string
	}{
		{cred.Token{Value: "lin_api_x"}, "lin_api_x"},
		{cred.Token{Scheme: "Bearer", Value: "lin_api_x"}, "lin_api_x"},
		{cred.Token{Value: "plainkey"}, "plainkey"},
		{cred.Token{Scheme: "Bearer", Value: "lin_oauth_x"}, "Bearer lin_oauth_x"},
	} {
		if got := authHeader(tc.tok); got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.tok.Value, got, tc.want)
		}
	}
}

func TestCheckUnknownTeam(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearViewer", `{"viewer":{"id":"u1","name":"tester","email":""}}`)
	f.ok("LinearTeam", `{"teams":{"nodes":[]}}`)
	a := newAdapter(t, srv, map[string]string{"team": "nope"}, "me")
	if _, err := a.Check(context.Background()); err == nil || !strings.Contains(err.Error(), `team "nope"`) {
		t.Fatalf("err %v", err)
	}
}

func TestCheckUnauthorizedNeverEchoesToken(t *testing.T) {
	f, srv := newFake(t)
	f.on("LinearViewer", reply{Status: 401, Body: `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"code":"AUTHENTICATION_ERROR"}}]}`})
	a := newAdapter(t, srv, nil, "me")
	_, err := a.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "keychain") || !strings.Contains(err.Error(), "Authentication required") {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("token in error")
	}
}

func TestResolvesCredentialWhenNoTransport(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearViewer", `{"viewer":{"id":"u1","name":"tester","email":"tester@acme.test"}}`)
	var got cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(_ context.Context, req cred.Request) (cred.Transport, error) {
		got = req
		return cred.Static(cred.SourceEnv, cred.Token{Value: "lin_api_fromenv"}), nil
	}))
	defer restore()
	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "linear", Auth: cred.SourceEnv, EnvVar: "ACME_LINEAR_TOKEN", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got.Workspace != "acme" || got.Provider != "linear" || got.EnvVar != "ACME_LINEAR_TOKEN" || got.Preferred != cred.SourceEnv {
		t.Fatalf("request %+v", got)
	}
	if f.last("LinearViewer").Auth != "lin_api_fromenv" {
		t.Fatal("resolved token not sent")
	}
}

func TestNeedsSessionWhenNothingResolves(t *testing.T) {
	_, srv := newFake(t)
	restore := cred.SetDefault(nil)
	defer restore()
	a, _ := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "linear", BaseURL: srv.URL})
	if _, err := a.Check(context.Background()); !errors.Is(err, cred.ErrNeedsSession) {
		t.Fatalf("err %v", err)
	}
}

func TestStatusesMapTypes(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeam", teamFixture)
	f.ok("LinearStates", statesFixture)
	a := newAdapter(t, srv, map[string]string{"org": "ACME"}, "me")
	st, err := a.Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(st)
	want := "[{Triage backlog} {Backlog backlog} {Todo todo} {In Progress active} {In Review review} {Done done} {Canceled done}]"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// the team and states are cached
	if _, err := a.Statuses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(f.ops(), ","), "LinearStates"); n != 1 {
		t.Fatalf("states fetched %d times", n)
	}
}

func TestStatusesNeedTeam(t *testing.T) {
	_, srv := newFake(t)
	a := newAdapter(t, srv, nil, "me")
	if _, err := a.Statuses(context.Background()); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err %v", err)
	}
}

func TestTeamByUUIDSkipsLookup(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearStates", statesFixture)
	a := newAdapter(t, srv, map[string]string{"team": teamID}, "me")
	if _, err := a.Statuses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.ops(), "LinearTeam") {
		t.Fatal("a team id needs no lookup")
	}
}

func TestCategory(t *testing.T) {
	for _, tc := range [][3]string{
		{"triage", "Triage", "backlog"}, {"backlog", "Icebox", "backlog"}, {"unstarted", "Todo", "todo"},
		{"started", "Doing", "active"}, {"started", "Code review", "review"}, {"started", "QA Review", "review"},
		{"completed", "Done", "done"}, {"canceled", "Won't do", "done"}, {"duplicate", "Dup", "done"}, {"weird", "X", "backlog"},
	} {
		if got := Category(tc[0], tc[1]); got != tc[2] {
			t.Fatalf("%s/%s: %s", tc[0], tc[1], got)
		}
	}
}

func TestListChangedAssignedToMe(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeam", teamFixture)
	f.on("LinearAssigned",
		reply{Body: `{"data":{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":true,"endCursor":"cur1"},"nodes":[` +
			issueJSON(issueID, "ACME-1", "First", "2026-10-26T10:00:00.000Z", "") + `]}}}}`},
		reply{Body: `{"data":{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":false,"endCursor":"cur2"},"nodes":[` +
			issueJSON(parentID, "ACME-2", "Second", "2026-10-26T12:30:00.000Z", "") + `]}}}}`})
	f.ok("LinearChildren", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[`+
		issueJSON(childID, "ACME-3", "Child", "2026-10-26T11:00:00.000Z", `"parent":{"id":"`+parentID+`","identifier":"ACME-2"}`)+`,`+
		issueJSON(parentID, "ACME-2", "Second", "2026-10-26T12:30:00.000Z", "")+`]}}`)
	a := newAdapter(t, srv, map[string]string{"org": "acme"}, "me")
	since := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items %d: %+v", len(items), items)
	}
	if want := time.Date(2026, 10, 26, 12, 30, 0, 0, time.UTC); !wm.Equal(want) {
		t.Fatalf("watermark %s", wm)
	}
	// second page asked with the cursor; filter carries updatedAt gt since and the team
	c := f.last("LinearAssigned")
	if c.Vars["after"] != "cur1" {
		t.Fatalf("after %v", c.Vars["after"])
	}
	filter, _ := json.Marshal(c.Vars["filter"])
	if !strings.Contains(string(filter), `"updatedAt":{"gt":"2026-10-25T00:00:00Z"}`) || !strings.Contains(string(filter), teamID) {
		t.Fatalf("filter %s", filter)
	}
	child := f.last("LinearChildren")
	cf, _ := json.Marshal(child.Vars["filter"])
	if !strings.Contains(string(cf), `"parent":{"assignee":{"isMe":{"eq":true}}}`) {
		t.Fatalf("children filter %s", cf)
	}
	var ch hubsync.Item
	for _, it := range items {
		if it.ID == childID {
			ch = it
		}
	}
	if ch.ParentID != parentID || ch.Key != "ACME-3" {
		t.Fatalf("child %+v", ch)
	}
	it := items[0]
	if it.ID != issueID || it.Key != "ACME-1" || it.URL != "https://linear.app/acme/issue/ACME-1" || it.Title != "First" ||
		it.Body != "Body of ACME-1" || it.Status != "In Progress" || it.StatusCategory != "active" ||
		it.Assignee != "tester@acme.test" || it.Rev != "2026-10-26T10:00:00.000Z" || fmt.Sprint(it.Labels) != "[bug]" {
		t.Fatalf("item %+v", it)
	}
	if len(it.Comments) != 2 || it.Comments[0].Body != "second" || it.Comments[0].Author != "tester" {
		t.Fatalf("comments newest first: %+v", it.Comments)
	}
	if len(it.Attachments) != 1 || it.Attachments[0].Name != "spec.pdf" {
		t.Fatalf("attachments %+v", it.Attachments)
	}
	if !it.UpdatedAt.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Fatal(it.UpdatedAt)
	}
}

func TestListChangedNoChangesKeepsWatermark(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearAssigned", `{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`)
	f.ok("LinearChildren", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`)
	a := newAdapter(t, srv, nil, "")
	since := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil || len(items) != 0 || !wm.Equal(since) {
		t.Fatal(items, wm, err)
	}
	filter, _ := json.Marshal(f.last("LinearAssigned").Vars["filter"])
	if strings.Contains(string(filter), "team") {
		t.Fatalf("no team scope, no team filter: %s", filter)
	}
}

func TestListChangedZeroSinceHasNoDateFilter(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearAssigned", `{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`)
	f.ok("LinearChildren", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`)
	a := newAdapter(t, srv, nil, "me")
	if _, _, err := a.ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	filter, _ := json.Marshal(f.last("LinearAssigned").Vars["filter"])
	if strings.Contains(string(filter), "updatedAt") {
		t.Fatalf("filter %s", filter)
	}
}

func TestListChangedZeroSinceSkipsClosedHistory(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearAssigned", `{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`)
	f.ok("LinearChildren", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`)
	a := newAdapter(t, srv, nil, "me")
	if _, _, err := a.ListChanged(context.Background(), time.Time{}); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"LinearAssigned", "LinearChildren"} {
		filter, _ := json.Marshal(f.last(op).Vars["filter"])
		if !strings.Contains(string(filter), `"state":{"type":{"nin":["completed","canceled","duplicate"]}}`) {
			t.Fatalf("%s: the first sync must not page closed history: %s", op, filter)
		}
	}
	// a later tick reads closed issues too (an issue that moved to Done must sync)
	if _, _, err := a.ListChanged(context.Background(), time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if filter, _ := json.Marshal(f.last("LinearAssigned").Vars["filter"]); strings.Contains(string(filter), "nin") {
		t.Fatalf("incremental filter %s", filter)
	}
}

func TestListChangedPageCapStillAdvancesWatermark(t *testing.T) {
	// every page says there is another: the cap must stop it, and the watermark must move
	var n int
	var mu gosync.Mutex
	srvCap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OperationName string `json:"operationName"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		mu.Lock()
		n++
		i := n
		mu.Unlock()
		if req.OperationName != "LinearIssues" {
			t.Errorf("op %s", req.OperationName)
		}
		upd := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		io.WriteString(w, `{"data":{"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"c`+fmt.Sprint(i)+`"},"nodes":[`+
			issueJSON(fmt.Sprintf("%08d-0000-4000-8000-000000000000", i), fmt.Sprintf("ACME-%d", i), "x", upd, "")+`]}}}`)
	}))
	defer srvCap.Close()
	a := newAdapter(t, srvCap, map[string]string{"team": teamID}, "anyone")
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if n != maxPages || len(items) != maxPages {
		t.Fatalf("pages %d items %d", n, len(items))
	}
	if !wm.After(since) {
		t.Fatalf("watermark stuck at %s: the next tick would reread the same pages", wm)
	}
	if want := time.Date(2026, 10, 25, 23, 59, 0, 0, time.UTC); !wm.Equal(want) {
		t.Fatalf("watermark %s, want the newest updatedAt seen %s", wm, want)
	}
	if w := a.LastWarning(); !strings.Contains(w, "truncated") {
		t.Fatalf("truncation not reported: %q", w)
	}
	// a full read clears the warning
	f2, srv2 := newFake(t)
	f2.ok("LinearIssues", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`)
	a2 := newAdapter(t, srv2, map[string]string{"team": teamID}, "anyone")
	a2.warning = "old"
	if _, _, err := a2.ListChanged(context.Background(), since); err != nil || a2.LastWarning() != "" {
		t.Fatalf("warning %q err %v", a2.LastWarning(), err)
	}
}

func TestListChangedChildrenOfNewlyMirroredParents(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearAssigned", `{"viewer":{"assignedIssues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`)
	f.ok("LinearChildren", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`)
	a := newAdapter(t, srv, nil, "me")
	if _, _, err := a.ListChanged(context.Background(), time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	cf, _ := json.Marshal(f.last("LinearChildren").Vars["filter"])
	s := string(cf)
	// children and grandchildren of the viewer's issues
	for _, want := range []string{
		`{"parent":{"assignee":{"isMe":{"eq":true}}}}`,
		`{"parent":{"parent":{"assignee":{"isMe":{"eq":true}}}}}`,
		// a child that did not change itself still comes in when its parent (or grandparent) did,
		// as it does when the parent is newly assigned
		`{"updatedAt":{"gt":"2026-10-25T00:00:00Z"}}`,
		`{"parent":{"updatedAt":{"gt":"2026-10-25T00:00:00Z"}}}`,
		`{"parent":{"parent":{"updatedAt":{"gt":"2026-10-25T00:00:00Z"}}}}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("children filter lacks %s:\n%s", want, s)
		}
	}
}

func TestCommentsNewestFirstQuery(t *testing.T) {
	if !strings.Contains(issueFields, "comments(first: 20, orderBy: createdAt)") {
		t.Fatalf("comments must ask for the newest 20: %s", issueFields)
	}
}

func TestMutationNotFoundIsNotItemNotFound(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueTeam", issueTeamFixture())
	f.on("LinearIssueUpdate", reply{Body: `{"data":null,"errors":[{"message":"Entity not found: WorkflowState","path":["issueUpdate"],"extensions":{"code":"INVALID_INPUT"}}]}`})
	a := newAdapter(t, srv, nil, "me")
	err := a.Move(context.Background(), "ACME-1", "Todo")
	if err == nil || errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("a stale state id must not say the issue is gone: %v", err)
	}
	if !strings.Contains(err.Error(), "WorkflowState") {
		t.Fatalf("err %v", err)
	}
	// the read paths still map it
	f.on("LinearIssueTeam", reply{Body: `{"data":null,"errors":[{"message":"Entity not found: Issue"}]}`})
	f.replies["LinearIssueTeam"] = f.replies["LinearIssueTeam"][1:]
	if err := a.Move(context.Background(), "ACME-404", "Todo"); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("read path: %v", err)
	}
	f.on("LinearIssueID", reply{Body: `{"data":null,"errors":[{"message":"Entity not found: Issue"}]}`})
	if err := a.Comment(context.Background(), "ACME-404", "x"); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("issueID path: %v", err)
	}
}

func TestUnknownTeamIsNotFound(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeam", `{"teams":{"nodes":[]}}`)
	a := newAdapter(t, srv, map[string]string{"team": "nope"}, "me")
	if _, err := a.Statuses(context.Background()); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	f, srv := newFake(t)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	f.on("LinearViewer", reply{Status: 429, Header: map[string]string{"Retry-After": clock.Add(45 * time.Second).Format(http.TimeFormat)}, Body: `{}`})
	a := newAdapter(t, srv, nil, "me")
	a.now = func() time.Time { return clock }
	_, err := a.Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 45*time.Second {
		t.Fatalf("err %v", err)
	}
}

func TestRateLimitWaitIsClamped(t *testing.T) {
	f, srv := newFake(t)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	f.on("LinearViewer",
		reply{Header: map[string]string{
			"X-RateLimit-Requests-Remaining": "0",
			"X-RateLimit-Requests-Reset":     fmt.Sprint(clock.Add(30 * 24 * time.Hour).UnixMilli()),
		}, Body: `{"data":{"viewer":{"id":"u1","name":"tester"}}}`})
	a := newAdapter(t, srv, nil, "me")
	a.now = func() time.Time { return clock }
	if _, err := a.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := a.Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != maxRateWait {
		t.Fatalf("a far reset must be clamped to %s: %v", maxRateWait, err)
	}
	f2, srv2 := newFake(t)
	f2.on("LinearViewer", reply{Status: 429, Header: map[string]string{"Retry-After": "999999"}, Body: `{}`})
	a2 := newAdapter(t, srv2, nil, "me")
	a2.now = func() time.Time { return clock }
	if _, err := a2.Check(context.Background()); !errors.As(err, &rl) || rl.RetryAfter != maxRateWait {
		t.Fatalf("Retry-After must be clamped: %v", err)
	}
}

func TestListChangedAnyone(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeam", teamFixture)
	f.ok("LinearIssues", `{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[`+
		issueJSON(issueID, "ACME-1", "First", "2026-10-26T10:00:00.000Z", "")+`]}}`)
	a := newAdapter(t, srv, map[string]string{"org": "acme"}, "anyone")
	items, _, err := a.ListChanged(context.Background(), time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC))
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if slices.Contains(f.ops(), "LinearAssigned") {
		t.Fatal("anyone must not use viewer.assignedIssues")
	}
	if _, _, err := newAdapter(t, srv, nil, "anyone").ListChanged(context.Background(), time.Time{}); err == nil {
		t.Fatal("anyone without a team would mirror the whole workspace")
	}
}

func TestGet(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssue", `{"issue":`+issueJSON(issueID, "ACME-1", "First", "2026-10-26T10:00:00.000Z", "")+`}`)
	a := newAdapter(t, srv, nil, "me")
	it, err := a.Get(context.Background(), "ACME-1")
	if err != nil || it.ID != issueID || it.Key != "ACME-1" {
		t.Fatal(it, err)
	}
	if f.last("LinearIssue").Vars["id"] != "ACME-1" {
		t.Fatal("identifier passed through")
	}
}

func TestGetNotFound(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssue", `{"issue":null}`)
	f.on("LinearIssue", reply{Body: `{"data":null,"errors":[{"message":"Entity not found: Issue","extensions":{"code":"INVALID_INPUT","userPresentableMessage":"Could not find referenced Issue."}}]}`})
	a := newAdapter(t, srv, nil, "me")
	for i := 0; i < 2; i++ {
		if _, err := a.Get(context.Background(), "ACME-404"); !errors.Is(err, hubsync.ErrNotFound) {
			t.Fatalf("%d: err %v", i, err)
		}
	}
}

func issueTeamFixture() string {
	return `{"issue":{"id":"` + issueID + `","labels":{"nodes":[{"id":"l-bug","name":"bug"}]},"team":{"id":"` + teamID + `",
 "states":{"nodes":[{"id":"s-todo","name":"Todo","type":"unstarted"},{"id":"s-review","name":"In Review","type":"started"}]},
 "labels":{"nodes":[{"id":"l-bug","name":"bug"},{"id":"l-ready","name":"agent-ready"}]}}},
 "issueLabels":{"nodes":[{"id":"l-ws","name":"Customer"}]}}`
}

func TestMoveResolvesStateByName(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueTeam", issueTeamFixture())
	f.ok("LinearIssueUpdate", `{"issueUpdate":{"success":true}}`)
	a := newAdapter(t, srv, nil, "me")
	if err := a.Move(context.Background(), "ACME-1", "in review"); err != nil {
		t.Fatal(err)
	}
	c := f.last("LinearIssueUpdate")
	in, _ := json.Marshal(c.Vars["input"])
	if c.Vars["id"] != issueID || string(in) != `{"stateId":"s-review"}` {
		t.Fatalf("update %v %s", c.Vars["id"], in)
	}
	err := a.Move(context.Background(), "ACME-1", "Shipped")
	if err == nil || !strings.Contains(err.Error(), `"Shipped"`) || !strings.Contains(err.Error(), "In Review") {
		t.Fatalf("unknown state: %v", err)
	}
}

func TestMoveFailureReported(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueTeam", issueTeamFixture())
	f.ok("LinearIssueUpdate", `{"issueUpdate":{"success":false}}`)
	a := newAdapter(t, srv, nil, "me")
	if err := a.Move(context.Background(), issueID, "Todo"); err == nil {
		t.Fatal("success:false accepted")
	}
}

func TestComment(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueID", `{"issue":{"id":"`+issueID+`"}}`)
	f.ok("LinearCommentCreate", `{"commentCreate":{"success":true,"comment":{"id":"c9"}}}`)
	a := newAdapter(t, srv, nil, "me")
	if err := a.Comment(context.Background(), "ACME-1", "**Done**: PR #1"); err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(f.last("LinearCommentCreate").Vars["input"])
	if string(in) != `{"body":"**Done**: PR #1","issueId":"`+issueID+`"}` {
		t.Fatalf("input %s", in)
	}
	// a UUID needs no lookup
	if err := a.Comment(context.Background(), issueID, "again"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(f.ops(), ","), "LinearIssueID"); n != 1 {
		t.Fatalf("id lookups %d", n)
	}
}

func TestCreateChild(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeam", teamFixture)
	f.ok("LinearIssueID", `{"issue":{"id":"`+parentID+`"}}`)
	f.ok("LinearTeamMeta", `{"team":{"id":"`+teamID+`","states":{"nodes":[{"id":"s-todo","name":"Todo","type":"unstarted"}]},
 "labels":{"nodes":[{"id":"l-ready","name":"agent-ready"}]}},"issueLabels":{"nodes":[{"id":"l-ws","name":"Customer"}]}}`)
	f.ok("LinearIssueCreate", `{"issueCreate":{"success":true,"issue":`+
		issueJSON(childID, "ACME-9", "Child", "2026-10-27T09:00:00.000Z", `"parent":{"id":"`+parentID+`","identifier":"ACME-2"}`)+`}}`)
	a := newAdapter(t, srv, map[string]string{"org": "acme"}, "me")
	it, err := a.Create(context.Background(), hubsync.NewItem{
		Title: "Child", Body: "Do it.", Acceptance: "- [ ] works", ParentID: "ACME-2", Status: "todo", Labels: []string{"customer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != childID || it.ParentID != parentID || it.Key != "ACME-9" {
		t.Fatalf("item %+v", it)
	}
	var in map[string]any
	b, _ := json.Marshal(f.last("LinearIssueCreate").Vars["input"])
	json.Unmarshal(b, &in)
	if in["teamId"] != teamID || in["parentId"] != parentID || in["title"] != "Child" || in["stateId"] != "s-todo" {
		t.Fatalf("input %s", b)
	}
	if d, _ := in["description"].(string); !strings.HasPrefix(d, "Do it.") || !strings.Contains(d, "## Acceptance criteria\n\n- [ ] works") {
		t.Fatalf("description %q", d)
	}
	if fmt.Sprint(in["labelIds"]) != "[l-ws]" {
		t.Fatalf("labels %v", in["labelIds"])
	}
}

func TestCreateNeedsTeam(t *testing.T) {
	_, srv := newFake(t)
	a := newAdapter(t, srv, nil, "me")
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "x"}); err == nil {
		t.Fatal("create without a team")
	}
}

func TestCreateUnknownLabel(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearTeamMeta", `{"team":{"id":"`+teamID+`","states":{"nodes":[]},"labels":{"nodes":[]}},"issueLabels":{"nodes":[]}}`)
	a := newAdapter(t, srv, map[string]string{"team": teamID}, "me")
	_, err := a.Create(context.Background(), hubsync.NewItem{Title: "x", Labels: []string{"ghost"}})
	if err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("err %v", err)
	}
	if slices.Contains(f.ops(), "LinearIssueCreate") {
		t.Fatal("created despite the bad label")
	}
}

func TestUpdate(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueUpdate", `{"issueUpdate":{"success":true}}`)
	a := newAdapter(t, srv, nil, "me")
	title := "New title"
	if err := a.Update(context.Background(), issueID, &title, nil); err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(f.last("LinearIssueUpdate").Vars["input"])
	if string(in) != `{"title":"New title"}` {
		t.Fatalf("input %s", in)
	}
	body := ""
	if err := a.Update(context.Background(), issueID, nil, &body); err != nil {
		t.Fatal(err)
	}
	in, _ = json.Marshal(f.last("LinearIssueUpdate").Vars["input"])
	if string(in) != `{"description":""}` {
		t.Fatalf("input %s", in)
	}
	n := len(f.ops())
	if err := a.Update(context.Background(), issueID, nil, nil); err != nil || len(f.ops()) != n {
		t.Fatal("nothing to update must not call the API", err)
	}
}

func TestSetLabels(t *testing.T) {
	f, srv := newFake(t)
	f.ok("LinearIssueTeam", issueTeamFixture())
	f.ok("LinearIssueAddLabel", `{"issueAddLabel":{"success":true}}`)
	f.ok("LinearIssueRemoveLabel", `{"issueRemoveLabel":{"success":true}}`)
	a := newAdapter(t, srv, nil, "me")
	// bug is already there (skip), agent-ready is a team label, Customer a workspace one; remove
	// bug, and absent "stale" is a no-op.
	if err := a.SetLabels(context.Background(), "ACME-1", []string{"bug", "Agent-Ready", "customer"}, []string{"BUG", "stale"}); err != nil {
		t.Fatal(err)
	}
	var adds, removes []string
	f.mu.Lock()
	for _, c := range f.calls {
		switch c.Op {
		case "LinearIssueAddLabel":
			adds = append(adds, fmt.Sprint(c.Vars["id"], ":", c.Vars["labelId"]))
		case "LinearIssueRemoveLabel":
			removes = append(removes, fmt.Sprint(c.Vars["id"], ":", c.Vars["labelId"]))
		}
	}
	f.mu.Unlock()
	if fmt.Sprint(adds) != "["+issueID+":l-ready "+issueID+":l-ws]" || fmt.Sprint(removes) != "["+issueID+":l-bug]" {
		t.Fatalf("adds %v removes %v", adds, removes)
	}
	if err := a.SetLabels(context.Background(), "ACME-1", []string{"ghost"}, nil); err == nil || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("unknown label: %v", err)
	}
}

func TestRateLimit429RetryAfter(t *testing.T) {
	f, srv := newFake(t)
	f.on("LinearViewer", reply{Status: 429, Header: map[string]string{"Retry-After": "30"}, Body: `{"errors":[{"message":"Rate limit exceeded","extensions":{"code":"RATELIMITED"}}]}`})
	a := newAdapter(t, srv, nil, "me")
	_, err := a.Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.Provider != "linear" || rl.RetryAfter != 30*time.Second {
		t.Fatalf("err %v", err)
	}
}

func TestRateLimitedErrorCodeUsesResetHeader(t *testing.T) {
	f, srv := newFake(t)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	reset := clock.Add(90 * time.Second).UnixMilli()
	f.on("LinearViewer", reply{Status: 400, Header: map[string]string{
		"X-RateLimit-Requests-Remaining": "0",
		"X-RateLimit-Requests-Reset":     fmt.Sprint(reset),
	}, Body: `{"errors":[{"message":"Rate limit exceeded","extensions":{"code":"RATELIMITED"}}]}`})
	a := newAdapter(t, srv, nil, "me")
	a.now = func() time.Time { return clock }
	_, err := a.Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 90*time.Second {
		t.Fatalf("err %v", err)
	}
}

func TestExhaustedBudgetBlocksNextRequest(t *testing.T) {
	f, srv := newFake(t)
	clock := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	f.on("LinearViewer", reply{Header: map[string]string{
		"X-RateLimit-Complexity-Remaining": "0",
		"X-RateLimit-Complexity-Reset":     fmt.Sprint(clock.Add(time.Minute).UnixMilli()),
		"X-RateLimit-Requests-Remaining":   "1200",
	}, Body: `{"data":{"viewer":{"id":"u1","name":"tester","email":"tester@acme.test"}}}`})
	a := newAdapter(t, srv, nil, "me")
	a.now = func() time.Time { return clock }
	if _, err := a.Check(context.Background()); err != nil {
		t.Fatalf("the response that exhausts the budget still succeeds: %v", err)
	}
	_, err := a.Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != time.Minute {
		t.Fatalf("err %v", err)
	}
	if n := len(f.ops()); n != 1 {
		t.Fatalf("blocked request reached the server (%d calls)", n)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := a.Check(context.Background()); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}

func TestGraphQLErrorSurfaces(t *testing.T) {
	f, srv := newFake(t)
	f.on("LinearViewer", reply{Body: `{"data":null,"errors":[{"message":"Field 'x' doesn't exist"}]}`})
	a := newAdapter(t, srv, nil, "me")
	if _, err := a.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Fatalf("err %v", err)
	}
}

func TestNoRedirectsFollowed(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token sent to the redirect target")
		}
		io.WriteString(w, `{"data":{"viewer":{"id":"x"}}}`)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/graphql", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	a := newAdapter(t, srv, nil, "me")
	if _, err := a.Check(context.Background()); err == nil {
		t.Fatal("a redirect must fail, not be followed")
	}
}
