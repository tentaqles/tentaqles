package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

var ctx = context.Background()

func basic(s string) string { return "Basic " + base64.StdEncoding.EncodeToString([]byte(s)) }

func TestRegistered(t *testing.T) {
	if !slices.Contains(hubsync.Registered(), "jira") {
		t.Fatalf("jira not registered: %v", hubsync.Registered())
	}
	if _, ok := cred.CLIHookFor("jira"); !ok {
		t.Fatal("the jira cli hook (acli) is not registered")
	}
	a, err := hubsync.New(hubsync.Config{Workspace: "acme", Provider: "jira", Scope: map[string]string{"org": "acme"}})
	if err != nil || a.Name() != "jira" {
		t.Fatal(a, err)
	}
}

func TestSiteURL(t *testing.T) {
	for _, tc := range []struct{ org, want string }{
		{"acme", "https://acme.atlassian.net"},
		{"acme.atlassian.net", "https://acme.atlassian.net"},
		{"https://acme.atlassian.net/", "https://acme.atlassian.net"},
		{"ACME.Atlassian.net", "https://acme.atlassian.net"},
	} {
		a, err := newAdapter(hubsync.Config{Provider: "jira", Scope: map[string]string{"org": tc.org}})
		if err != nil || a.base != tc.want {
			t.Fatalf("%q: %q %v", tc.org, a.base, err)
		}
	}
	a, err := newAdapter(hubsync.Config{Provider: "jira", Scope: map[string]string{"site": "acme"}})
	if err != nil || a.base != "https://acme.atlassian.net" {
		t.Fatalf("site key: %v", err)
	}
	for _, bad := range []string{"", "acme.example.com", "http://acme.atlassian.net", "acme.atlassian.net.example.com", "acme/x"} {
		if _, err := newAdapter(hubsync.Config{Provider: "jira", Scope: map[string]string{"org": bad}}); err == nil {
			t.Fatalf("%q accepted: a token must only go to an Atlassian Cloud site", bad)
		}
	}
}

func TestCheckSendsBasicAuth(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/myself", 200, `{"accountId":"557058:abc","emailAddress":"tester@example.com","displayName":"Tester"}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	acct, err := a.Check(ctx)
	if err != nil || acct != "tester@example.com" {
		t.Fatal(acct, err)
	}
	c, _ := f.last("GET", "/rest/api/3/myself")
	if c.Auth != basic(fixtureCred) {
		t.Fatalf("auth %q", c.Auth)
	}

	// an API token alone plus tracker.scope.email
	a = newTestAdapter(t, f, map[string]string{"org": "acme", "email": "tester@example.com"}, cred.Token{Value: fixtureCredPart})
	if _, err := a.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.last("GET", "/rest/api/3/myself"); c.Auth != basic(fixtureCred) {
		t.Fatalf("auth %q", c.Auth)
	}
	// an already-encoded Basic value passes through
	enc := base64.StdEncoding.EncodeToString([]byte(fixtureCred))
	a = newTestAdapter(t, f, nil, cred.Token{Scheme: "Basic", Value: enc})
	if _, err := a.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if c, _ := f.last("GET", "/rest/api/3/myself"); c.Auth != "Basic "+enc {
		t.Fatalf("auth %q", c.Auth)
	}

	// a bare token with no email is refused before anything is sent, and the error names no token
	n := len(f.seen())
	a = newTestAdapter(t, f, nil, cred.Token{Value: fixtureCredPart})
	_, err = a.Check(ctx)
	if err == nil || strings.Contains(err.Error(), "s3cr3t") || !strings.Contains(err.Error(), "email") {
		t.Fatalf("%v", err)
	}
	if len(f.seen()) != n {
		t.Fatal("a request went out without a usable credential")
	}

	// display name when the email is hidden
	f.json("GET", "/rest/api/3/myself", 200, `{"accountId":"557058:abc","displayName":"Tester"}`)
	a = newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	if acct, _ := a.Check(ctx); acct != "Tester" {
		t.Fatal(acct)
	}
}

func TestUnauthorizedNamesTheSourceNeverTheToken(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/myself", 401, `{"errorMessages":["Client must be authenticated to access this resource."]}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	_, err := a.Check(ctx)
	if err == nil || strings.Contains(err.Error(), "s3cr3t") || !strings.Contains(err.Error(), "keychain") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("%v", err)
	}
}

func TestStatusesMapCategories(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/project/ACME/statuses", 200, `[
	 {"name":"Task","statuses":[
	   {"name":"To Do","statusCategory":{"key":"new"}},
	   {"name":"In Progress","statusCategory":{"key":"indeterminate"}},
	   {"name":"In Review","statusCategory":{"key":"indeterminate"}},
	   {"name":"Done","statusCategory":{"key":"done"}}]},
	 {"name":"Subtask","statuses":[
	   {"name":"To Do","statusCategory":{"key":"new"}},
	   {"name":"Done","statusCategory":{"key":"done"}}]}]`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	got, err := a.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []hubsync.Status{{Name: "To Do", Category: "todo"}, {Name: "In Progress", Category: "active"}, {Name: "In Review", Category: "active"}, {Name: "Done", Category: "done"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}

	f.json("GET", "/rest/api/3/status", 200, `[{"name":"Backlog","statusCategory":{"key":"undefined"}},{"name":"Closed","statusCategory":{"key":"done"}}]`)
	a = newTestAdapter(t, f, map[string]string{"org": "acme"}, cred.Token{Value: fixtureCred})
	got, err = a.Statuses(ctx)
	if err != nil || !slices.Equal(got, []hubsync.Status{{Name: "Backlog", Category: "backlog"}, {Name: "Closed", Category: "done"}}) {
		t.Fatalf("%v %v", got, err)
	}
	for k, want := range map[string]string{"new": "todo", "indeterminate": "active", "done": "done", "undefined": "backlog", "": "backlog"} {
		if c := Category(k); c != want || !hubsync.ValidCategory(c) {
			t.Fatalf("%q -> %q", k, c)
		}
	}
}

func adfPara(s string) string {
	return fmt.Sprintf(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":%q}]}]}`, s)
}

func issueJSON(id, key, summary, status, cat, updated string, extra string) string {
	return fmt.Sprintf(`{"id":%q,"key":%q,"fields":{"summary":%q,
	  "status":{"name":%q,"statusCategory":{"key":%q}},
	  "assignee":{"displayName":"Tester","accountId":"557058:abc"},
	  "labels":["backend"],"updated":%q,
	  "description":%s%s}}`, id, key, summary, status, cat, updated, adfPara("Body of "+key), extra)
}

func TestListChangedPaginatesAndMaps(t *testing.T) {
	f := newFakeJira(t)
	var bodies []map[string]any
	f.on("POST", "/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if body["nextPageToken"] == nil {
			io.WriteString(w, `{"issues":[`+issueJSON("10001", "ACME-1", "First", "In Progress", "indeterminate", "2026-10-27T10:00:00.000+0000",
				`,"parent":{"id":"10000","key":"ACME-0"},"attachment":[{"filename":"spec.pdf","content":"https://acme.atlassian.net/rest/api/3/attachment/content/1"}]`)+
				`],"nextPageToken":"page-2","isLast":false}`)
			return
		}
		if body["nextPageToken"] != "page-2" {
			t.Errorf("token %v", body["nextPageToken"])
		}
		io.WriteString(w, `{"issues":[`+issueJSON("10002", "ACME-2", "Second", "Done", "done", "2026-10-27T11:30:00.000+0000", "")+`],"isLast":true}`)
	})
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	since := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC) // an hour before a.now
	items, mark, err := a.ListChanged(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || len(items) != 2 {
		t.Fatalf("%d pages, %d items", len(bodies), len(items))
	}
	jql, _ := bodies[0]["jql"].(string)
	for _, want := range []string{`project = "ACME"`, "assignee = currentUser()", `updated >= "-61m"`, "ORDER BY updated ASC"} {
		if !strings.Contains(jql, want) {
			t.Fatalf("jql %q misses %q", jql, want)
		}
	}
	if fields, _ := bodies[0]["fields"].([]any); !slices.Contains(fields, any("summary")) || !slices.Contains(fields, any("status")) {
		t.Fatalf("fields %v", bodies[0]["fields"])
	}
	one := items[0]
	if one.ID != "ACME-1" || one.Key != "ACME-1" || one.URL != f.srv.URL+"/browse/ACME-1" || one.Title != "First" ||
		one.Status != "In Progress" || one.StatusCategory != "active" || one.ParentID != "ACME-0" ||
		one.Body != "Body of ACME-1" || one.Assignee != "Tester" || !slices.Equal(one.Labels, []string{"backend"}) ||
		one.Rev != "2026-10-27T10:00:00.000+0000" || !one.UpdatedAt.Equal(time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", one)
	}
	if len(one.Attachments) != 1 || one.Attachments[0].Name != "spec.pdf" || !strings.Contains(one.Attachments[0].URL, "/attachment/content/1") {
		t.Fatalf("%+v", one.Attachments)
	}
	if items[1].StatusCategory != "done" {
		t.Fatalf("%+v", items[1])
	}
	if !mark.Equal(time.Date(2026, 10, 27, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("watermark %v", mark)
	}

	// nothing changed: the watermark stays
	f.json("POST", "/rest/api/3/search/jql", 200, `{"issues":[],"isLast":true}`)
	items, mark, err = a.ListChanged(ctx, since)
	if err != nil || len(items) != 0 || !mark.Equal(since) {
		t.Fatal(items, mark, err)
	}
}

func TestListChangedFirstSyncAndAnyone(t *testing.T) {
	f := newFakeJira(t)
	f.json("POST", "/rest/api/3/search/jql", 200, `{"issues":[],"isLast":true}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	if _, _, err := a.ListChanged(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	c, _ := f.last("POST", "/rest/api/3/search/jql")
	jql := c.Body["jql"].(string)
	if strings.Contains(jql, "updated >=") || !strings.Contains(jql, "statusCategory != Done") {
		t.Fatalf("first sync jql %q", jql)
	}
	a.cfg.AssignedTo = "anyone"
	a.ListChanged(ctx, time.Time{})
	c, _ = f.last("POST", "/rest/api/3/search/jql")
	if strings.Contains(c.Body["jql"].(string), "currentUser") {
		t.Fatalf("anyone jql %q", c.Body["jql"])
	}
}

func TestGetUsesRenderedFieldsAndComments(t *testing.T) {
	f := newFakeJira(t)
	var comments []string
	for i := 1; i <= 25; i++ {
		comments = append(comments, fmt.Sprintf(`{"id":"%d","author":{"displayName":"Tester"},"body":%s,"created":"2026-10-%02dT09:00:00.000+0000"}`, i, adfPara(fmt.Sprintf("note %d", i)), i))
	}
	f.json("GET", "/rest/api/3/issue/ACME-7", 200, `{"id":"10007","key":"ACME-7","fields":{
	  "summary":"Seven","status":{"name":"To Do","statusCategory":{"key":"new"}},
	  "updated":"2026-10-27T10:00:00.000+0000","labels":[],
	  "description":{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]}]},
	  "customfield_10050":`+adfPara("Given a card, it moves")+`,
	  "comment":{"comments":[`+strings.Join(comments, ",")+`]}},
	  "renderedFields":{"description":"<p>Rendered &amp; <b>shown</b></p>"}}`)
	a := newTestAdapter(t, f, map[string]string{"org": "acme", "project": "ACME", "acceptance_field": "customfield_10050"}, cred.Token{Value: fixtureCred})
	it, err := a.Get(ctx, "ACME-7")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := f.last("GET", "/rest/api/3/issue/ACME-7")
	if !strings.Contains(c.Query, "expand=renderedFields") || !strings.Contains(c.Query, "customfield_10050") {
		t.Fatalf("query %q", c.Query)
	}
	if it.Body != "Rendered & shown" {
		t.Fatalf("body %q: the rendered field is the fallback when ADF has no text", it.Body)
	}
	if it.Acceptance != "Given a card, it moves" || it.StatusCategory != "todo" {
		t.Fatalf("%+v", it)
	}
	if len(it.Comments) != hubsync.MaxComments || it.Comments[0].Body != "note 25" || it.Comments[0].ID != "25" ||
		it.Comments[0].Author != "Tester" || it.Comments[19].Body != "note 6" {
		t.Fatalf("comments newest first, capped: %d %+v", len(it.Comments), it.Comments[0])
	}

	_, err = a.Get(ctx, "ACME-404")
	if !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}

func TestMoveResolvesTheTransition(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/issue/ACME-1/transitions", 200, `{"transitions":[
	  {"id":"11","name":"Start work","to":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}},
	  {"id":"31","name":"Finish","to":{"name":"Done","statusCategory":{"key":"done"}}}]}`)
	f.json("POST", "/rest/api/3/issue/ACME-1/transitions", 204, ``)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	if err := a.Move(ctx, "ACME-1", "in progress"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.last("POST", "/rest/api/3/issue/ACME-1/transitions")
	if tr, _ := c.Body["transition"].(map[string]any); tr["id"] != "11" {
		t.Fatalf("%v", c.Body)
	}
	err := a.Move(ctx, "ACME-1", "QA")
	if err == nil {
		t.Fatal("moved to a status with no transition")
	}
	for _, want := range []string{"QA", "ACME-1", "In Progress", "Done"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q misses %q", err, want)
		}
	}
}

func TestCommentSendsADF(t *testing.T) {
	f := newFakeJira(t)
	f.json("POST", "/rest/api/3/issue/ACME-1/comment", 201, `{"id":"900"}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	md := "Run finished.\n\n- tests green\n- see [PR 12](https://github.com/acme/app/pull/12)\n\n```go\nfmt.Println(1)\n```"
	if err := a.Comment(ctx, "ACME-1", md); err != nil {
		t.Fatal(err)
	}
	c, _ := f.last("POST", "/rest/api/3/issue/ACME-1/comment")
	doc, _ := c.Body["body"].(map[string]any)
	if doc["type"] != "doc" || doc["version"] != float64(1) {
		t.Fatalf("%v", c.Body)
	}
	raw, _ := json.Marshal(doc)
	for _, want := range []string{`"bulletList"`, `"codeBlock"`, `"language":"go"`, `"href":"https://github.com/acme/app/pull/12"`, `"Run finished."`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s misses %s", raw, want)
		}
	}
	if err := a.Comment(ctx, "ACME-1", "  \n"); err == nil {
		t.Fatal("an empty comment was sent")
	}
}

func TestCreateChildAndTopLevel(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/issue/createmeta/ACME/issuetypes", 200, `{"issueTypes":[{"id":"1","name":"Task","subtask":false},{"id":"5","name":"Sub-task","subtask":true}]}`)
	f.json("POST", "/rest/api/3/issue", 201, `{"id":"10010","key":"ACME-10"}`)
	f.json("GET", "/rest/api/3/issue/ACME-10", 200, issueJSON("10010", "ACME-10", "Child", "To Do", "new", "2026-10-27T12:00:00.000+0000", `,"parent":{"key":"ACME-1"}`))
	f.json("GET", "/rest/api/3/issue/ACME-10/transitions", 200, `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress"}}]}`)
	f.json("POST", "/rest/api/3/issue/ACME-10/transitions", 204, ``)
	a := newTestAdapter(t, f, map[string]string{"org": "acme"}, cred.Token{Value: fixtureCred})
	it, err := a.Create(ctx, hubsync.NewItem{Title: "Child", Body: "Do **it**", Acceptance: "- passes", ParentID: "ACME-1", Labels: []string{"agent"}, Status: "In Progress"})
	if err != nil {
		t.Fatal(err)
	}
	if it.Key != "ACME-10" || it.ParentID != "ACME-1" {
		t.Fatalf("%+v", it)
	}
	c, _ := f.last("POST", "/rest/api/3/issue")
	fields := c.Body["fields"].(map[string]any)
	if fields["summary"] != "Child" || fields["project"].(map[string]any)["key"] != "ACME" ||
		fields["parent"].(map[string]any)["key"] != "ACME-1" || fields["issuetype"].(map[string]any)["id"] != "5" {
		t.Fatalf("%v", fields)
	}
	if fmt.Sprint(fields["labels"]) != "[agent]" {
		t.Fatalf("%v", fields["labels"])
	}
	raw, _ := json.Marshal(fields["description"])
	if !strings.Contains(string(raw), `"strong"`) || !strings.Contains(string(raw), "Acceptance") || !strings.Contains(string(raw), "passes") {
		t.Fatalf("description %s", raw)
	}
	if c, ok := f.last("POST", "/rest/api/3/issue/ACME-10/transitions"); !ok || c.Body["transition"].(map[string]any)["id"] != "11" {
		t.Fatal("status not applied after create")
	}

	// top level: the scope project, the configured type, acceptance into its own field
	a = newTestAdapter(t, f, map[string]string{"org": "acme", "project": "ACME", "issue_type": "Story", "acceptance_field": "customfield_10050"}, cred.Token{Value: fixtureCred})
	if _, err := a.Create(ctx, hubsync.NewItem{Title: "Top", Acceptance: "works"}); err != nil {
		t.Fatal(err)
	}
	c, _ = f.last("POST", "/rest/api/3/issue")
	fields = c.Body["fields"].(map[string]any)
	if fields["issuetype"].(map[string]any)["name"] != "Story" || fields["parent"] != nil || fields["customfield_10050"] == nil || fields["description"] != nil {
		t.Fatalf("%v", fields)
	}
	// top level with no project anywhere
	a = newTestAdapter(t, f, map[string]string{"org": "acme"}, cred.Token{Value: fixtureCred})
	if _, err := a.Create(ctx, hubsync.NewItem{Title: "Lost"}); err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("%v", err)
	}
}

func TestUpdateAndLabels(t *testing.T) {
	f := newFakeJira(t)
	f.json("PUT", "/rest/api/3/issue/ACME-1", 204, ``)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	title, body := "New title", "New *body*"
	if err := a.Update(ctx, "ACME-1", &title, &body); err != nil {
		t.Fatal(err)
	}
	c, _ := f.last("PUT", "/rest/api/3/issue/ACME-1")
	fields := c.Body["fields"].(map[string]any)
	if fields["summary"] != "New title" || fields["description"].(map[string]any)["type"] != "doc" {
		t.Fatalf("%v", c.Body)
	}
	if err := a.Update(ctx, "ACME-1", nil, &body); err != nil {
		t.Fatal(err)
	}
	c, _ = f.last("PUT", "/rest/api/3/issue/ACME-1")
	if _, has := c.Body["fields"].(map[string]any)["summary"]; has {
		t.Fatalf("summary sent when nil: %v", c.Body)
	}
	n := len(f.seen())
	if err := a.Update(ctx, "ACME-1", nil, nil); err != nil || len(f.seen()) != n {
		t.Fatal("a no-op update went out")
	}

	if err := a.SetLabels(ctx, "ACME-1", []string{"agent-ready"}, []string{"draft"}); err != nil {
		t.Fatal(err)
	}
	c, _ = f.last("PUT", "/rest/api/3/issue/ACME-1")
	raw, _ := json.Marshal(c.Body)
	if string(raw) != `{"update":{"labels":[{"add":"agent-ready"},{"remove":"draft"}]}}` {
		t.Fatalf("%s", raw)
	}
	n = len(f.seen())
	if err := a.SetLabels(ctx, "ACME-1", nil, nil); err != nil || len(f.seen()) != n {
		t.Fatal("a no-op label change went out")
	}
	if err := a.SetLabels(ctx, "ACME-1", []string{"has space"}, nil); err == nil {
		t.Fatal("jira labels cannot hold spaces")
	}
}

func TestRateLimitRetryAfter(t *testing.T) {
	f := newFakeJira(t)
	f.on("POST", "/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("X-RateLimit-Limit", "100")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	_, _, err := a.ListChanged(ctx, time.Time{})
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second || rl.Provider != "jira" {
		t.Fatalf("%v", err)
	}

	// an HTTP date, and X-RateLimit-Reset when there is no Retry-After
	f.on("GET", "/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Retry-After", a.now().Add(30*time.Second).Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err = a.Check(ctx)
	if !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Fatalf("%v", err)
	}
	f.on("GET", "/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("X-RateLimit-Reset", a.now().Add(2*time.Minute).Format(time.RFC3339))
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err = a.Check(ctx)
	if !errors.As(err, &rl) || rl.RetryAfter != 2*time.Minute {
		t.Fatalf("%v", err)
	}
}

func TestErrorBodyIsBounded(t *testing.T) {
	f := newFakeJira(t)
	f.json("PUT", "/rest/api/3/issue/ACME-1", 400, `{"errorMessages":[],"errors":{"summary":"`+strings.Repeat("x", 5000)+`"}}`)
	a := newTestAdapter(t, f, nil, cred.Token{Value: fixtureCred})
	title := "t"
	err := a.Update(ctx, "ACME-1", &title, nil)
	if err == nil || len(err.Error()) > 700 || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("%d: %.200v", len(fmt.Sprint(err)), err)
	}
}

func TestLazyResolveThroughTheCredResolver(t *testing.T) {
	f := newFakeJira(t)
	f.json("GET", "/rest/api/3/myself", 200, `{"displayName":"Tester"}`)
	var got cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(_ context.Context, req cred.Request) (cred.Transport, error) {
		got = req
		return cred.Static(cred.SourceEnv, cred.Token{Value: fixtureCred}), nil
	}))
	t.Cleanup(restore)
	a, err := newAdapter(hubsync.Config{Workspace: "acme", Provider: "jira", Scope: map[string]string{"org": "acme"}, Auth: cred.SourceEnv, EnvVar: "ACME_JIRA_TOKEN", BaseURL: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if got.Provider != "jira" || got.Workspace != "acme" || got.EnvVar != "ACME_JIRA_TOKEN" || got.Preferred != cred.SourceEnv {
		t.Fatalf("%+v", got)
	}

	cred.SetDefault(nil)
	a, _ = newAdapter(hubsync.Config{Workspace: "acme", Provider: "jira", Scope: map[string]string{"org": "acme"}, BaseURL: f.srv.URL})
	if _, err := a.Check(ctx); !errors.Is(err, cred.ErrNeedsSession) {
		t.Fatalf("%v", err)
	}
}
