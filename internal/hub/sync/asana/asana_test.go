package asana

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

func testConfig(f *fakeAsana) hubsync.Config {
	return hubsync.Config{Workspace: "acme", Provider: "asana", Scope: map[string]string{"org": wsGID, "board": projGID},
		AssignedTo: "me", BaseURL: f.srv.URL + "/api/1.0", Transport: cred.Static(cred.SourceKeychain, cred.Token{Value: testToken})}
}

func newTest(t *testing.T, f *fakeAsana, mod func(*hubsync.Config)) *Adapter {
	t.Helper()
	cfg := testConfig(f)
	if mod != nil {
		mod(&cfg)
	}
	a, err := NewAdapter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return testNow }
	return a
}

// testNow is the adapter clock in tests: a first sync asks for tasks completed since then.
var testNow = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

func TestRegistered(t *testing.T) {
	f := newFake(t)
	a, err := hubsync.New(testConfig(f))
	if err != nil || a.Name() != "asana" {
		t.Fatal(a, err)
	}
	var _ hubsync.Adapter = (*Adapter)(nil)
}

func TestCheck(t *testing.T) {
	f := newFake(t)
	acct, err := newTest(t, f, nil).Check(context.Background())
	if err != nil || acct != "tester@acme.test" {
		t.Fatal(acct, err)
	}
	_, err = newTest(t, f, func(c *hubsync.Config) { c.Scope["org"] = "999" }).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "999") {
		t.Fatalf("unknown workspace: %v", err)
	}
	_, err = newTest(t, f, func(c *hubsync.Config) {
		c.Transport = cred.Static(cred.SourceEnv, cred.Token{Value: "wrong-value"})
	}).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "wrong-value") {
		t.Fatalf("bad token: %v", err)
	}
}

func TestStatusesAndLaneOverride(t *testing.T) {
	f := newFake(t)
	got, err := newTest(t, f, nil).Statuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []hubsync.Status{{Name: "Backlog", Category: "backlog"}, {Name: "To do", Category: "todo"}, {Name: "Doing", Category: "active"}, {Name: "In Review", Category: "review"}, {Name: "QA", Category: "review"}, {Name: "Done", Category: "done"}}
	if !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
	// tracker.lanes, through ConfigFromManifest: QA goes to the hub's "in progress" lane, Doing to review.
	tr := &manifest.Tracker{Provider: "asana", Scope: map[string]string{"org": wsGID, "board": projGID},
		Lanes: map[string]string{"qa": "in progress", "Doing": "review"}}
	cfg := hubsync.ConfigFromManifest("acme", tr)
	cfg.BaseURL, cfg.Transport = testConfig(f).BaseURL, testConfig(f).Transport
	a, err := NewAdapter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err = a.Statuses(context.Background())
	if err != nil || got[2].Category != "review" || got[4].Category != "active" {
		t.Fatalf("override: %v %v", got, err)
	}
	if _, err := newTest(t, f, func(c *hubsync.Config) { delete(c.Scope, "board") }).Statuses(context.Background()); err == nil {
		t.Fatal("statuses without a project")
	}
}

func TestCategoryHeuristics(t *testing.T) {
	for name, want := range map[string]string{
		"Backlog": "backlog", "Icebox": "backlog", "Untitled section": "backlog", "Ideas": "backlog",
		"To Do": "todo", "todo": "todo", "Ready": "todo", "Up next": "todo", "Not started": "todo",
		"In Progress": "active", "Doing": "active", "WIP": "active", "In development": "active",
		"Ready for review": "review", "Code Review": "review", "QA": "review", "Testing": "review",
		"Done": "done", "Completed": "done", "Shipped": "done", "Closed": "done",
		"Something else": "backlog",
	} {
		if got := categoryOf(name, nil); got != want {
			t.Errorf("%q: got %s want %s", name, got, want)
		}
	}
	lanes := map[string]string{"Blocked": "in progress", "Parked": "backlog", "Weird": "a lane named nothing"}
	if categoryOf("blocked", lanes) != "active" || categoryOf("Parked", lanes) != "backlog" || categoryOf("Weird", lanes) != "backlog" {
		t.Fatal("lane override")
	}
}

func TestListChangedAssignedToMe(t *testing.T) {
	f := newFake(t)
	since := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	items, wm, err := newTest(t, f, nil).ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	// 1002 is older than since; 1003 is in another project; 1007 was completed before since;
	// 1008 is my subtask of a project task (not a project member itself); 1005 is a subtask of a
	// mirrored task.
	if !slices.Equal(ids, []string{"1001", "1004", "1008", "1005"}) {
		t.Fatalf("ids %v", ids)
	}
	if want := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC); !wm.Equal(want) {
		t.Fatalf("watermark %v", wm)
	}
	q := f.queries[0]
	for _, s := range []string{"assignee=me", "workspace=" + wsGID, "modified_since=2026-10-19T00%3A00%3A00Z", "completed_since=2026-10-19T00%3A00%3A00Z", "opt_fields=", "notes", "custom_fields"} {
		if !strings.Contains(q, s) {
			t.Fatalf("query %q lacks %q", q, s)
		}
	}
	it := items[0]
	if it.Title != "Login page" || it.Body != "Build the login page." || it.Status != "Doing" || it.StatusCategory != "active" ||
		it.Assignee != "tester@acme.test" || it.Rev != "2026-10-20T10:00:00.000Z" || it.URL == "" || it.Key != "1001" ||
		!slices.Equal(it.Labels, []string{"bug"}) || !strings.Contains(it.Acceptance, "they see the board") {
		t.Fatalf("%+v", it)
	}
	if len(it.Comments) != 2 || it.Comments[0].Body != "second" || it.Comments[1].Body != "first" || it.Comments[0].Author != "tester" {
		t.Fatalf("comments %+v", it.Comments)
	}
	if len(it.Attachments) != 1 || it.Attachments[0].Name != "mock.png" || strings.Contains(it.Attachments[0].URL, "Signature") {
		t.Fatalf("attachments %+v", it.Attachments)
	}
	if items[1].StatusCategory != "done" || items[1].Status != "In Review" {
		t.Fatalf("completed task: %+v", items[1])
	}
	if items[3].ParentID != "1001" || items[3].Status != "" || items[3].StatusCategory != "todo" {
		t.Fatalf("subtask: %+v", items[3])
	}

	// nothing changed: the watermark stays
	f.reset()
	later := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	items, wm, err = newTest(t, f, nil).ListChanged(context.Background(), later)
	if err != nil || len(items) != 0 || !wm.Equal(later) {
		t.Fatal(items, wm, err)
	}
}

func TestListChangedAnyone(t *testing.T) {
	f := newFake(t)
	items, _, err := newTest(t, f, func(c *hubsync.Config) { c.AssignedTo = "anyone" }).ListChanged(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	// 1007 was completed before the first sync; 1005 and 1008 are subtasks of 1001.
	if !slices.Equal(ids, []string{"1001", "1002", "1004", "1006", "1005", "1008"}) {
		t.Fatalf("ids %v", ids)
	}
	if !strings.Contains(f.queries[0], "project="+projGID) || strings.Contains(f.queries[0], "modified_since") ||
		!strings.Contains(f.queries[0], "completed_since=2026-10-19T00%3A00%3A00Z") {
		t.Fatalf("query %s", f.queries[0])
	}
	if items[3].Body != "Line one" {
		t.Fatalf("html notes fallback %q", items[3].Body)
	}
	if _, _, err := newTest(t, f, func(c *hubsync.Config) { delete(c.Scope, "org") }).ListChanged(context.Background(), time.Time{}); err == nil {
		t.Fatal("assigned to me needs the workspace")
	}
}

func TestGet(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	it, err := a.Get(context.Background(), "1001")
	if err != nil || it.Title != "Login page" || len(it.Comments) != 2 || len(it.Attachments) != 1 {
		t.Fatal(it, err)
	}
	if _, err := a.Get(context.Background(), "4040"); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := a.Get(context.Background(), "../users/me"); err == nil {
		t.Fatal("a non-gid id was accepted")
	}
}

func TestMove(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	if err := a.Move(context.Background(), "1001", "in review"); err != nil {
		t.Fatal(err)
	}
	if f.tasks["1001"].Section != "303" || !slices.Contains(f.requests(), "POST /api/1.0/sections/303/addTask") {
		t.Fatalf("%v %v", f.tasks["1001"].Section, f.requests())
	}
	err := a.Move(context.Background(), "1001", "Nowhere")
	if err == nil || !strings.Contains(err.Error(), "Nowhere") {
		t.Fatalf("unknown status: %v", err)
	}
}

func TestComment(t *testing.T) {
	f := newFake(t)
	md := "Run **passed** <ok> & `go test` green.\n\n- one\n- see [PR](https://github.com/acme/app/pull/1)\n\nDone"
	if err := newTest(t, f, nil).Comment(context.Background(), "1001", md); err != nil {
		t.Fatal(err)
	}
	got := f.stories[len(f.stories)-1].HTML
	want := `<body>Run <strong>passed</strong> &lt;ok&gt; &amp; <code>go test</code> green.` + "\n\n" +
		`<ul><li>one</li><li>see <a href="https://github.com/acme/app/pull/1">PR</a></li></ul>` + "\n\n" + `Done</body>`
	if got != want {
		t.Fatalf("html_text\n got %s\nwant %s", got, want)
	}
}

func TestMarkdownToHTML(t *testing.T) {
	for md, want := range map[string]string{
		"":                             "<body></body>",
		"plain":                        "<body>plain</body>",
		"*em* and _em_":                "<body><em>em</em> and <em>em</em></body>",
		"[x](javascript:alert)":        "<body>[x](javascript:alert)</body>",
		"# Title\nline":                "<body><strong>Title</strong>\nline</body>",
		"a \"q\" ' b":                  "<body>a &#34;q&#34; &#39; b</body>",
		"* a\n* b":                     "<body><ul><li>a</li><li>b</li></ul></body>",
		"1. a\n2. b":                   "<body><ol><li>a</li><li>b</li></ol></body>",
		"```\nx <y>\n```":              "<body><pre>x &lt;y&gt;</pre></body>",
		"**unclosed bold":              "<body>**unclosed bold</body>",
		"[a](https://x.test/?q=1&b=2)": `<body><a href="https://x.test/?q=1&amp;b=2">a</a></body>`,
		"[`x`](https://a.b/c)":         `<body><a href="https://a.b/c"><code>x</code></a></body>`,
		"`[a](https://a.b/c)`":         `<body><code>[a](https://a.b/c)</code></body>`,
		"[a](https://a.b/`c`)":         `<body>[a](https://a.b/<code>c</code>)</body>`,
	} {
		if got := markdownToHTML(md); got != want {
			t.Errorf("%q\n got %s\nwant %s", md, got, want)
		}
	}
}

func TestCreate(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	it, err := a.Create(context.Background(), hubsync.NewItem{Title: "Signup", Body: "Build signup.", Acceptance: "- works",
		Status: "To do", Labels: []string{"bug", "frontend"}})
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Signup" || it.Status != "To do" || !slices.Equal(it.Labels, []string{"bug", "frontend"}) || it.Assignee != "tester@acme.test" {
		t.Fatalf("%+v", it)
	}
	if !strings.Contains(it.Body, "Build signup.") || !strings.Contains(it.Body, "Acceptance criteria") || !strings.Contains(it.Body, "- works") {
		t.Fatalf("body %q", it.Body)
	}
	first := f.bodies[0]
	if first["workspace"] != wsGID || first["assignee"] != "me" {
		t.Fatalf("create body %v", first)
	}
	// child: created as a subtask of the parent
	child, err := a.Create(context.Background(), hubsync.NewItem{Title: "Step", ParentID: "1001"})
	if err != nil || child.ParentID != "1001" {
		t.Fatal(child, err)
	}
	if !slices.Contains(f.requests(), "POST /api/1.0/tasks/1001/subtasks") {
		t.Fatal(f.requests())
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{Title: "x", Status: "Nowhere"}); err == nil {
		t.Fatal("unknown status")
	}
	if _, err := a.Create(context.Background(), hubsync.NewItem{}); err == nil {
		t.Fatal("empty title")
	}
}

func TestUpdate(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	title := "Login page v2"
	if err := a.Update(context.Background(), "1001", &title, nil); err != nil {
		t.Fatal(err)
	}
	if f.tasks["1001"].Name != title || f.tasks["1001"].Notes != "Build the login page." {
		t.Fatalf("%+v", f.tasks["1001"])
	}
	if _, has := f.bodies[0]["notes"]; has {
		t.Fatal("notes sent though nil")
	}
	f.reset()
	if err := a.Update(context.Background(), "1001", nil, nil); err != nil || len(f.requests()) != 0 {
		t.Fatal("no-op update made a request", f.requests(), err)
	}
	body := "new body"
	if err := a.Update(context.Background(), "4040", nil, &body); !errors.Is(err, hubsync.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSetLabels(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	if err := a.SetLabels(context.Background(), "1001", []string{"agent-ready", "Frontend"}, []string{"bug", "never-there"}); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range f.tasks["1001"].Tags {
		names = append(names, f.tags[g])
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"Frontend", "agent-ready"}) {
		t.Fatalf("tags %v", names)
	}
	if err := a.SetLabels(context.Background(), "1001", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimit(t *testing.T) {
	f := newFake(t)
	f.limited = 1
	_, err := newTest(t, f, nil).Check(context.Background())
	var rl *hubsync.RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second || rl.Provider != "asana" {
		t.Fatalf("429: %v", err)
	}
	// the next tick works
	if _, err := newTest(t, f, nil).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	// a 429 on the second page fails the whole ListChanged: no partial list, no watermark
	f.mu.Lock()
	f.limitAt = len(f.reqs) + 2
	f.mu.Unlock()
	items, wm, err := newTest(t, f, nil).ListChanged(context.Background(), time.Time{})
	if !errors.As(err, &rl) || items != nil || !wm.IsZero() {
		t.Fatalf("mid-list 429: %v %v %v", items, wm, err)
	}
	// Retry-After as an HTTP date, and missing
	if d := retryAfter("Mon, 02 Jan 2006 15:04:05 GMT", time.Date(2006, 1, 2, 15, 4, 0, 0, time.UTC)); d != 5*time.Second {
		t.Fatal(d)
	}
	if d := retryAfter("", time.Now()); d != 0 {
		t.Fatal(d)
	}
}

func TestCredentials(t *testing.T) {
	f := newFake(t)
	// no Transport in the config: the adapter asks the cred resolver, per request
	var asked []cred.Request
	restore := cred.SetDefault(cred.ResolverFunc(func(ctx context.Context, req cred.Request) (cred.Transport, error) {
		asked = append(asked, req)
		return cred.Static(cred.SourceKeychain, cred.Token{Value: testToken}), nil
	}))
	defer restore()
	a := newTest(t, f, func(c *hubsync.Config) { c.Transport = nil; c.EnvVar = "ACME_ASANA_TOKEN"; c.Auth = cred.SourceEnv })
	if _, err := a.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Provider != "asana" || asked[0].Workspace != "acme" || asked[0].EnvVar != "ACME_ASANA_TOKEN" || asked[0].Preferred != cred.SourceEnv {
		t.Fatalf("%+v", asked)
	}
	// only MCP left: needs-session comes through, and nothing is sent
	cred.SetDefault(nil)
	f.reset()
	_, err := newTest(t, f, func(c *hubsync.Config) { c.Transport = nil }).Check(context.Background())
	if !errors.Is(err, cred.ErrNeedsSession) || len(f.requests()) != 0 {
		t.Fatalf("needs-session: %v %v", err, f.requests())
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, nil)
	_, err := a.Get(context.Background(), "4040")
	if err == nil || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "asana") {
		t.Fatal(err)
	}
}

func TestListChangedFirstSyncSkipsOldCompletedTasks(t *testing.T) {
	f := newFake(t)
	items, _, err := newTest(t, f, nil).ListChanged(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == "1007" {
			t.Fatal("a task completed before the first sync was listed")
		}
	}
	if !strings.Contains(f.queries[0], "completed_since=2026-10-19T00%3A00%3A00Z") || strings.Contains(f.queries[0], "modified_since") {
		t.Fatalf("first-sync query %s", f.queries[0])
	}
}

func TestListTruncatedKeepsTheWatermark(t *testing.T) {
	f := newFake(t)
	a := newTest(t, f, func(c *hubsync.Config) { c.AssignedTo = "anyone" })
	a.maxPages = 1 // page size 2: 2 of the 4 open project tasks fit
	items, wm, err := a.ListChanged(context.Background(), time.Time{})
	if err == nil || !errors.Is(err, errTruncated) || items != nil || !wm.IsZero() {
		t.Fatalf("truncated list: %v %v %v", items, wm, err)
	}
}

func TestListChangedWatchedParents(t *testing.T) {
	f := newFake(t)
	// 1001 is unchanged since then; its children 1005 (no assignee) and 1008 changed later.
	since := time.Date(2026, 10, 20, 12, 30, 0, 0, time.UTC)
	a := newTest(t, f, func(c *hubsync.Config) { c.AssignedTo = "anyone" })
	items, _, err := a.ListChanged(context.Background(), since)
	if err != nil || len(items) != 0 {
		t.Fatalf("no watch: %v %v", items, err)
	}
	if err := a.SetMirroredParents([]string{"1001", "../x"}); err == nil {
		t.Fatal("a non-gid parent was accepted")
	}
	if err := a.SetMirroredParents([]string{"1001"}); err != nil {
		t.Fatal(err)
	}
	items, wm, err := a.ListChanged(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, []string{"1005", "1008"}) || !wm.Equal(time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("watched: %v %v", ids, wm)
	}
}

func TestListChangedMySubtaskOfAnUnchangedProjectTask(t *testing.T) {
	f := newFake(t)
	// only 1008 (mine, not a project member) changed; its parent 1001 is in the project
	since := time.Date(2026, 10, 20, 13, 30, 0, 0, time.UTC)
	items, _, err := newTest(t, f, nil).ListChanged(context.Background(), since)
	if err != nil || len(items) != 1 || items[0].ID != "1008" || items[0].ParentID != "1001" {
		t.Fatalf("%v %v", items, err)
	}
}

func TestCreateKeepsTheTaskWhenLabelsFail(t *testing.T) {
	f := newFake(t)
	f.failOn = "POST /api/1.0/tasks/5001/addTag"
	it, err := newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "Signup", Labels: []string{"bug"}})
	if err != nil || it.ID != "5001" || len(it.Labels) != 0 {
		t.Fatalf("a created task must come back without an error: %+v %v", it, err)
	}
	n := 0
	for _, r := range f.requests() {
		if r == "POST /api/1.0/tasks" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d creates", n)
	}
	// the follow-up read failing still returns the created task
	f.failOn = "GET /api/1.0/tasks/5002"
	it, err = newTest(t, f, nil).Create(context.Background(), hubsync.NewItem{Title: "Other"})
	if err != nil || it.ID != "5002" || it.Title != "Other" {
		t.Fatalf("%+v %v", it, err)
	}
}

func TestSectionGIDFromTheServerIsChecked(t *testing.T) {
	f := newFake(t)
	f.sections[3][0] = "303/../../users/me"
	err := newTest(t, f, nil).Move(context.Background(), "1001", "In Review")
	if err == nil || !strings.Contains(err.Error(), "gid") {
		t.Fatalf("bad section gid: %v", err)
	}
	for _, r := range f.requests() {
		if strings.HasPrefix(r, "POST") {
			t.Fatalf("sent %s", r)
		}
	}
}
