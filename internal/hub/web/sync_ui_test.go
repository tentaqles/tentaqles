package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// uiTracker is an in-memory tracker for the page tests; it never touches a network.
type uiTracker struct {
	mu      gosync.Mutex
	items   []hubsync.Item
	lists   int
	created []hubsync.NewItem
}

func (f *uiTracker) Name() string                              { return "github" }
func (f *uiTracker) Check(ctx context.Context) (string, error) { return "tester", nil }
func (f *uiTracker) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	return []hubsync.Status{{Name: "To Do", Category: hubsync.CategoryTodo}, {Name: "In Progress", Category: hubsync.CategoryActive},
		{Name: "In Review", Category: hubsync.CategoryReview}, {Name: "Done", Category: hubsync.CategoryDone}}, nil
}
func (f *uiTracker) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	out := f.items
	f.items = nil
	return out, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), nil
}
func (f *uiTracker) Get(ctx context.Context, id string) (hubsync.Item, error) {
	return hubsync.Item{ID: id, Status: "To Do"}, nil
}
func (f *uiTracker) Move(ctx context.Context, id, status string) error      { return nil }
func (f *uiTracker) Comment(ctx context.Context, id, markdown string) error { return nil }
func (f *uiTracker) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	return hubsync.Item{ID: "77", Key: "#77", URL: "https://tracker.example/acme/77", Title: in.Title, Status: "To Do"}, nil
}
func (f *uiTracker) Update(ctx context.Context, id string, title, body *string) error { return nil }
func (f *uiTracker) SetLabels(ctx context.Context, id string, add, remove []string) error {
	return nil
}

func (f *uiTracker) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// syncBackend is a StaticBackend that also knows each workspace's sync engine.
type syncBackend struct {
	StaticBackend
	engines map[string]SyncEngine
}

func (b syncBackend) SyncEngine(ws string) (SyncEngine, error) {
	if _, err := b.Service(ws); err != nil {
		return nil, err
	}
	if e := b.engines[ws]; e != nil {
		return e, nil
	}
	return nil, hubsync.ErrNoTracker
}

// syncEnv is acme with a github tracker and an engine on the fake.
func syncEnv(t *testing.T) (*env, *hubsync.Engine, *uiTracker, syncBackend) {
	t.Helper()
	e := newEnv(t, "acme")
	fake := &uiTracker{}
	eng, err := hubsync.NewEngine(hubsync.EngineConfig{Workspace: "acme", Store: e.svcs["acme"].Store(),
		Tracker: &manifest.Tracker{Provider: "github", Scope: map[string]string{"org": "acme"}},
		Adapter: func(context.Context) (hubsync.Adapter, error) { return fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return e, eng, fake, syncBackend{StaticBackend: e.backend(), engines: map[string]SyncEngine{"acme": eng}}
}

func TestConnectionsWithoutATracker(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	code, body, _ := get(t, ts, "/connections?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d", code)
	}
	main := between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{"No company tracker", "tq onboard", "tq allow", ">GitHub<", ">Azure DevOps<", ">Jira<", ">Linear<", ">Asana<",
		`href="/connections?ws=acme" class="on" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("connections lacks %q", want)
		}
	}
	if strings.Contains(main, `action="/connections/sync`) || strings.Contains(main, "release 2, part B") {
		t.Error("a workspace without a tracker has no Sync now button and no placeholder")
	}
	assertCSPSafe(t, "/connections", body)
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts2 := e.server(Options{Backend: b})
	if code, _, _ := get(t, ts2, "/connections?ws=zeta", nil); code != http.StatusForbidden {
		t.Fatalf("an unavailable workspace: %d", code)
	}
	// Sync now and the JSON overview without a tracker answer 404 with the reason.
	if code, _, _ := postForm(t, ts, "/connections/sync?ws=acme", url.Values{}, origin(ts)); code != http.StatusNotFound {
		t.Fatalf("sync now without a tracker: %d", code)
	}
	if code, m, _ := apiDo(t, ts.URL, "GET", "/api/sync?ws=acme", nil, nil); code != http.StatusNotFound || m["code"] != "no_tracker" {
		t.Fatalf("api sync without a tracker: %d %v", code, m)
	}
}

func TestConnectionsShowsTheTrackerState(t *testing.T) {
	e, eng, fake, b := syncEnv(t)
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	_, body, _ := get(t, ts, "/connections?ws=acme", nil)
	main := between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{">GitHub<", "acme", "Never synced", "not checked yet", `action="/connections/sync?ws=acme"`, ">Sync now</button>",
		"Built from the tracker", "cli → keychain → env → mcp"} {
		if !strings.Contains(main, want) {
			t.Errorf("before a sync, connections lacks %q", want)
		}
	}
	fake.items = []hubsync.Item{{ID: "12", Key: "#12", URL: "https://tracker.example/acme/12", Title: "Fix the login", Status: "In Progress",
		StatusCategory: hubsync.CategoryActive}}
	if _, err := eng.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, body, _ = get(t, ts, "/connections?ws=acme", nil)
	main = between(body, `<div id="main">`, `<div class="toast"`)
	ov, _ := eng.Overview()
	for _, want := range []string{"tester", "data-auth-source", "Last sync", clock(ov.States[0].LastOKAt), "Next run", clock(ov.NextAt),
		"<td>In Progress</td><td>In progress</td>", "<td>In Review</td><td>Review</td>"} {
		if !strings.Contains(main, want) {
			t.Errorf("after a sync, connections lacks %q", want)
		}
	}
	if strings.Contains(main, "Never synced") {
		t.Error("still never synced")
	}
	// The six policy toggles, each off/ask/auto, with the stored value checked.
	for _, a := range store.OutboxActions {
		for _, v := range store.PolicyValues {
			radio := fmt.Sprintf(`name="policy.%s" value="%s"`, a, v)
			if !strings.Contains(main, radio) {
				t.Errorf("no %s toggle", radio)
			}
		}
		want := fmt.Sprintf(`name="policy.%s" value="%s" checked`, a, store.DefaultSyncPolicy().Get(a))
		if !strings.Contains(main, want) {
			t.Errorf("%s: the stored value is not checked", a)
		}
	}
	if !strings.Contains(main, `data-sync-settings`) || !strings.Contains(main, `action="/connections/settings?ws=acme"`) {
		t.Error("no policy form")
	}
	// A failed poll shows its error.
	if err := e.svcs["acme"].Store().RecordSyncError("github", eng.Scope(), "github: rate limited"); err != nil {
		t.Fatal(err)
	}
	_, body, _ = get(t, ts, "/connections?ws=acme", nil)
	if !strings.Contains(body, "github: rate limited") || !strings.Contains(body, "Last error") {
		t.Error("the last error is not shown")
	}
	assertCSPSafe(t, "/connections", body)
	code, m, raw := apiDo(t, ts.URL, "GET", "/api/sync?ws=acme", nil, nil)
	ovm, _ := m["overview"].(map[string]any)
	conn, _ := m["connection"].(map[string]any)
	if code != 200 || ovm["provider"] != "github" || conn["account"] != "tester" {
		t.Fatalf("api sync: %d %s", code, raw)
	}
	if strings.Contains(raw, "token") {
		t.Fatalf("the overview mentions a token: %s", raw)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSyncNowChecksAndPolls(t *testing.T) {
	e, eng, fake, b := syncEnv(t)
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	if code, _, _ := postForm(t, ts, "/connections/sync?ws=acme", url.Values{}, nil); code != http.StatusForbidden {
		t.Fatalf("a cross-site sync now: %d", code)
	}
	code, _, h := postForm(t, ts, "/connections/sync?ws=acme", url.Values{}, origin(ts))
	if code != http.StatusSeeOther || h.Get("Location") != "/connections?ws=acme&flash=sync-started" {
		t.Fatalf("sync now: %d %s", code, h.Get("Location"))
	}
	waitFor(t, "the pass", func() bool {
		ov, _ := eng.Overview()
		return fake.listCount() == 1 && len(ov.States) == 1 && ov.States[0].LastOKAt != ""
	})
	waitFor(t, "the check", func() bool { c, ok := eng.Connection(); return ok && c.Account == "tester" })
	waitFor(t, "the flight to end", func() bool {
		_, body, _ := get(t, ts, "/connections?ws=acme", nil)
		return !strings.Contains(body, "Sync running")
	})
	if code, m, raw := apiDo(t, ts.URL, "POST", "/api/sync/now?ws=acme", nil, nil); code != http.StatusAccepted || m["started"] != true {
		t.Fatalf("api sync now: %d %s", code, raw)
	}
	waitFor(t, "the second pass", func() bool { return fake.listCount() == 2 })
	// Board sync turned off: Sync now is refused with the reason.
	off := false
	if err := e.svcs["acme"].Store().SetSyncEnabled(off); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the api flight to end", func() bool {
		_, m, _ := apiDo(t, ts.URL, "GET", "/api/sync?ws=acme", nil, nil)
		return m["running"] == false
	})
	if code, body, _ := postForm(t, ts, "/connections/sync?ws=acme", url.Values{}, origin(ts)); code != http.StatusBadRequest || !strings.Contains(body, "sync is off") {
		t.Fatalf("sync now while off: %d", code)
	}
}

func TestSyncPolicyFormSavesThroughTheSettings(t *testing.T) {
	e, _, _, b := syncEnv(t)
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	st := e.svcs["acme"].Store()
	form := url.Values{"policy.close_item": {"off"}, "policy.create_card": {"auto"}, "interval": {"120"}, "enabled": {"on"}}
	if code, _, _ := postForm(t, ts, "/connections/settings?ws=acme", form, mergeHdr(origin(ts), map[string]string{HeaderAgent: "1"})); code != http.StatusForbidden {
		t.Fatalf("an agent changed the policy: %d", code)
	}
	if st.SyncPolicyFor(store.OutboxCloseItem) != store.PolicyAsk {
		t.Fatal("the refused update was stored")
	}
	code, _, h := postForm(t, ts, "/connections/settings?ws=acme", form, origin(ts))
	if code != http.StatusSeeOther || h.Get("Location") != "/connections?ws=acme&flash=settings-saved" {
		t.Fatalf("%d %s", code, h.Get("Location"))
	}
	if st.SyncPolicyFor(store.OutboxCloseItem) != store.PolicyOff || st.SyncPolicyFor(store.OutboxCreateCard) != store.PolicyAuto ||
		st.SyncInterval() != 120 || !st.SyncEnabled(false) {
		t.Fatal("the policy form was not stored")
	}
	acts, _ := st.Activity(50)
	if !slices.ContainsFunc(acts, func(a activity.Entry) bool { return a.Verb == activity.VerbSettingsUpdate }) {
		t.Fatal("no settings.update activity: the form bypassed the settings API")
	}
	for _, bad := range []url.Values{{"policy.close_item": {"never"}}, {"interval": {"30"}}, {"enabled": {"maybe"}}, {"policy.close_items": {"off"}}} {
		if code, _, _ := postForm(t, ts, "/connections/settings?ws=acme", bad, origin(ts)); code != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, code)
		}
	}
	_, body, _ := get(t, ts, "/connections?ws=acme&flash=settings-saved", nil)
	if !strings.Contains(body, `name="policy.close_item" value="off" checked`) || !strings.Contains(body, `value="120"`) {
		t.Error("the page does not show the saved policy")
	}
}

func mergeHdr(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestLinkedCardShowsTheExternalBadgeAndLane(t *testing.T) {
	e, _, _, b := syncEnv(t)
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	st := e.svcs["acme"].Store()
	c, err := st.AddCard(store.NewCard{Title: "Mirror me", Lane: "todo"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LinkCard(c.ID, store.ExternalLink{Provider: "azure-devops", ID: "123", URL: "https://tracker.example/acme/_workitems/edit/123", Lane: "Active"}); err != nil {
		t.Fatal(err)
	}
	_, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	main := between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{`<a class="ext" href="https://tracker.example/acme/_workitems/edit/123" rel="noopener noreferrer">ADO #123</a>`,
		`company lane <span class="chip">Active</span>`} {
		if !strings.Contains(main, want) {
			t.Errorf("card page lacks %q", want)
		}
	}
	if strings.Contains(main, "/push?") {
		t.Error("a linked card offers Push to company board")
	}
	_, board, _ := get(t, ts, "/?ws=acme", nil)
	if !strings.Contains(board, `rel="noopener noreferrer">ADO #123</a>`) {
		t.Error("the board tile has no external badge")
	}
	// A link that is not http(s) is shown as text, never as a link.
	c2, _ := st.AddCard(store.NewCard{Title: "Odd link", Lane: "todo"}, "tester")
	if err := st.LinkCard(c2.ID, store.ExternalLink{Provider: "jira", ID: "ACME-7", URL: "javascript:alert(1)"}); err != nil {
		t.Fatal(err)
	}
	_, body, _ = get(t, ts, "/cards/"+c2.ID+"?ws=acme", nil)
	if !strings.Contains(body, `<span class="ext">Jira ACME-7</span>`) || strings.Contains(body, "javascript:") {
		t.Error("an unsafe external link was rendered")
	}
}

func TestPushToCompanyBoard(t *testing.T) {
	e, eng, fake, b := syncEnv(t)
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	st := e.svcs["acme"].Store()
	c, err := st.AddCard(store.NewCard{Title: "Local idea", Body: "write it up", Lane: "backlog"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if !strings.Contains(body, `action="/cards/`+c.ID+`/push?ws=acme"`) || !strings.Contains(body, "Push to company board</button>") {
		t.Fatal("a local card has no Push to company board button")
	}
	// Agents are refused, on the form route and on the API.
	if code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/push?ws=acme", url.Values{}, mergeHdr(origin(ts), map[string]string{HeaderAgent: "1"})); code != http.StatusForbidden {
		t.Fatalf("agent push (form): %d", code)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/push?ws=acme", nil, map[string]string{HeaderAgent: "1"}); code != http.StatusForbidden || m["code"] != "human_only" {
		t.Fatalf("agent push (api): %d %v", code, m)
	}
	if rows, _ := st.ListOutbox(store.OutboxFilter{CardID: c.ID}); len(rows) != 0 {
		t.Fatal("a refused push queued a row")
	}
	code, _, h := postForm(t, ts, "/cards/"+c.ID+"/push?ws=acme", url.Values{}, origin(ts))
	if code != http.StatusSeeOther || h.Get("Location") != "/cards/"+c.ID+"?ws=acme&flash=pushed" {
		t.Fatalf("push: %d %s", code, h.Get("Location"))
	}
	rows, _ := st.ListOutbox(store.OutboxFilter{CardID: c.ID})
	if len(rows) != 1 || rows[0].Action != store.OutboxCreateCard || rows[0].Status != store.OutboxPending {
		t.Fatalf("outbox %+v", rows)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/push?ws=acme", nil, nil); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(m["error"]), "already queued") {
		t.Fatalf("a second push: %d %v", code, m)
	}
	_, body, _ = get(t, ts, "/cards/"+c.ID+"?ws=acme&flash=pushed", nil)
	main := between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{"Queued for the company board", "Company board: create card pending", rows[0].ID} {
		if !strings.Contains(main, want) {
			t.Errorf("after the push the card lacks %q", want)
		}
	}
	if strings.Contains(main, "Push to company board</button>") {
		t.Error("a queued card still offers the push")
	}

	// The default create_card policy is ask: the engine asks send/skip, and the decision renders
	// like the other permission decisions, on the card and in the inbox.
	if _, err := eng.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ := st.OutboxRow(rows[0].ID)
	if row.DecisionID == "" {
		t.Fatalf("no decision for the ask row: %+v", row)
	}
	_, body, _ = get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	main = between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{`id="d-` + row.DecisionID + `"`, `name="choice" value="send">Send</button>`, `name="choice" value="skip">Skip</button>`,
		"company board", "the sync sends or skips the write", "Company board: create card waiting on you"} {
		if !strings.Contains(main, want) {
			t.Errorf("the outbox decision lacks %q", want)
		}
	}
	_, inbox, _ := get(t, ts, "/waiting?ws=acme", nil)
	if !strings.Contains(inbox, `id="d-`+row.DecisionID+`"`) || !strings.Contains(inbox, "the sync sends or skips the write") {
		t.Error("the outbox decision is not in the inbox")
	}
	code, _, _ = postForm(t, ts, "/cards/"+c.ID+"/decisions/"+row.DecisionID+"/answer?ws=acme", url.Values{"choice": {"send"}}, origin(ts))
	if code != http.StatusSeeOther {
		t.Fatalf("answer send: %d", code)
	}
	if _, err := eng.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.created) != 1 {
		t.Fatalf("created %d items", len(fake.created))
	}
	_, body, _ = get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	if !strings.Contains(body, "Company board: create card sent") || !strings.Contains(body, ">GitHub #77</a>") {
		t.Error("the sent row or the new link is not on the card")
	}

	// Without a tracker there is no button, and the route says why.
	e2 := newEnv(t, "globex")
	ts2 := e2.server(Options{LocalKey: "lk"})
	c3, _ := e2.svcs["globex"].Store().AddCard(store.NewCard{Title: "No tracker", Lane: "backlog"}, "tester")
	_, body, _ = get(t, ts2, "/cards/"+c3.ID+"?ws=globex", nil)
	if strings.Contains(body, "/push?") {
		t.Error("push offered without a tracker")
	}
	if code, m, _ := apiDo(t, ts2.URL, "POST", "/api/cards/"+c3.ID+"/push?ws=globex", nil, nil); code != http.StatusNotFound || m["code"] != "no_tracker" {
		t.Fatalf("push without a tracker: %d %v", code, m)
	}
}

func TestOutboxTimelineItems(t *testing.T) {
	rows := []store.OutboxRow{
		{ID: "o-1", Action: store.OutboxMoveOnStart, Status: store.OutboxSent, CreatedAt: "2026-10-27T10:00:00Z", SentAt: "2026-10-27T10:00:05Z"},
		{ID: "o-2", Action: store.OutboxCommentResults, Status: store.OutboxFailed, LastError: "github: 500", CreatedAt: "2026-10-27T10:01:00Z"},
		{ID: "o-3", Action: store.OutboxCloseItem, Status: store.OutboxSkipped, LastError: "policy off: sync.policy.close_item", CreatedAt: "2026-10-27T10:02:00Z"},
		{ID: "o-4", Action: store.OutboxCreateCard, Status: store.OutboxPending, DecisionID: "d-1", CreatedAt: "2026-10-27T10:03:00Z"},
		{ID: "o-5", Action: store.OutboxMoveOnReview, Status: store.OutboxPending, CreatedAt: "2026-10-27T10:04:00Z"},
	}
	got := withOutbox([]tlItem{{When: "2026-10-27T10:00:01Z", Text: "Run queued"}}, rows)
	want := []struct{ text, class, code, when string }{
		{"Company board: move on start sent", "ok", "o-1", "2026-10-27T10:00:00Z"},
		{"Run queued", "", "", "2026-10-27T10:00:01Z"},
		{"Company board: comment results failed", "x", "o-2", "2026-10-27T10:01:00Z"},
		{"Company board: close item skipped", "", "o-3", "2026-10-27T10:02:00Z"},
		{"Company board: create card waiting on you", "w", "o-4", "2026-10-27T10:03:00Z"},
		{"Company board: move on review pending", "r", "o-5", "2026-10-27T10:04:00Z"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d items: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Text != w.text || got[i].Class != w.class || got[i].Code != w.code || got[i].When != w.when {
			t.Errorf("%d: %+v, want %+v", i, got[i], w)
		}
	}
	if got[2].Detail != "github: 500" {
		t.Errorf("failed detail %q", got[2].Detail)
	}
}

func TestExternalLabel(t *testing.T) {
	for _, c := range []struct{ provider, id, want string }{
		{"azure-devops", "123", "ADO #123"}, {"github", "45", "GitHub #45"}, {"jira", "ACME-7", "Jira ACME-7"},
		{"linear", "ENG-3", "Linear ENG-3"}, {"asana", "1200001", "Asana #1200001"}, {"acme-tracker", "9", "acme-tracker #9"},
	} {
		if got := extLabel(c.provider, c.id); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.provider, c.id, got, c.want)
		}
	}
	for in, want := range map[string]string{"https://tracker.example/1": "https://tracker.example/1", "http://x.example": "http://x.example",
		"javascript:alert(1)": "", "": "", "ftp://x.example": "", "//evil.example": ""} {
		if got := extURL(in); got != want {
			t.Errorf("extURL(%q) = %q", in, got)
		}
	}
}

// The policy toggles save on change through the settings API; a refusal restores the stored value.
func TestHubJSSavesThePolicyToggles(t *testing.T) {
	js := readAsset(t, "static/hub.js")
	for _, want := range []string{`form[data-sync-settings]`, `input[type="radio"][name^="policy."]`,
		`api("POST", "/api/settings", { sync: { policy: policy } })`, "g.checked = g.defaultChecked", "mainBinders.push(bindSyncPolicy)"} {
		if !strings.Contains(js, want) {
			t.Errorf("hub.js lacks %q", want)
		}
	}
}
