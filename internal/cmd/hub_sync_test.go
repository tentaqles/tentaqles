package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// cmdTracker is a small in-memory tracker for the command tests.
type cmdTracker struct {
	mu       gosync.Mutex
	items    map[string]hubsync.Item
	created  []hubsync.NewItem
	comments []string
}

func (f *cmdTracker) Name() string                              { return "github" }
func (f *cmdTracker) Check(ctx context.Context) (string, error) { return "tester", nil }
func (f *cmdTracker) Statuses(ctx context.Context) ([]hubsync.Status, error) {
	return []hubsync.Status{{Name: "Todo", Category: hubsync.CategoryTodo}, {Name: "In Progress", Category: hubsync.CategoryActive},
		{Name: "Done", Category: hubsync.CategoryDone}}, nil
}
func (f *cmdTracker) ListChanged(ctx context.Context, since time.Time) ([]hubsync.Item, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []hubsync.Item
	for _, it := range f.items {
		out = append(out, it)
	}
	return out, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), nil
}
func (f *cmdTracker) Get(ctx context.Context, id string) (hubsync.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items[id], nil
}
func (f *cmdTracker) Move(ctx context.Context, id, status string) error { return nil }
func (f *cmdTracker) Comment(ctx context.Context, id, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, markdown)
	return nil
}
func (f *cmdTracker) Update(ctx context.Context, id string, t, b *string) error { return nil }
func (f *cmdTracker) SetLabels(ctx context.Context, id string, a, r []string) error {
	return nil
}
func (f *cmdTracker) Create(ctx context.Context, in hubsync.NewItem) (hubsync.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	it := hubsync.Item{ID: "501", URL: "https://tracker.example/acme/501", Title: in.Title, Status: "Todo", Rev: "1"}
	f.items[it.ID] = it
	return it, nil
}

const trackerManifest = "tracker:\n  provider: github\n  scope: { org: acme }\n"

func syncWS(t *testing.T) *cmdTracker {
	t.Helper()
	// hubWS with a tracker block in the manifest.
	root := setupTrustedWorkspaceWithManifest(t, "acme", trackerManifest)
	home := filepath.Dir(os.Getenv("TQ_HOME"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		unsetenv(t, k)
	}
	t.Setenv("TQ_HUB_GATE_CMD", `["tq-hub-test-no-gate"]`)
	t.Chdir(root)
	f := &cmdTracker{items: map[string]hubsync.Item{
		"7": {ID: "7", URL: "https://tracker.example/acme/7", Title: "Export invoices", Status: "In Progress", StatusCategory: hubsync.CategoryActive, Rev: "1"},
	}}
	prev := syncAdapterHook
	syncAdapterHook = func(context.Context, *resolve.Workspace) (hubsync.Adapter, error) { return f, nil }
	t.Cleanup(func() { syncAdapterHook = prev })
	return f
}

func TestHubSyncNowStatusOutbox(t *testing.T) {
	syncWS(t)
	_, out, errOut, err := runTQ(t, "hub", "sync", "now", "--json")
	if err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	var rep hubsync.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Created != 1 || rep.Provider != "github" {
		t.Fatalf("%v %q", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "sync", "now")
	if err != nil || !strings.Contains(out, "synced github (org=acme)") || !strings.Contains(out, "updated 1") {
		t.Fatalf("%v %q", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "list", "--json")
	if err != nil || !strings.Contains(out, "Export invoices") || !strings.Contains(out, `"lane": "in progress"`) {
		t.Fatalf("%v %s", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "sync", "status", "--json")
	var ov hubsync.Overview
	if err != nil || json.Unmarshal([]byte(out), &ov) != nil || ov.Provider != "github" || len(ov.States) != 1 || ov.States[0].LastOKAt == "" {
		t.Fatalf("%v %s", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "sync", "status")
	if err != nil || !strings.Contains(out, "policy: move_on_start=auto") || !strings.Contains(out, "In Progress -> in progress") {
		t.Fatalf("%v %s", err, out)
	}
	_, out, _, err = runTQ(t, "hub", "sync", "outbox", "--json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("%v %q", err, out)
	}
	if _, _, _, err := runTQ(t, "hub", "sync", "outbox", "--status", "lost"); err == nil {
		t.Fatal("a bad --status was accepted")
	}
}

func TestHubPushAsksThenSends(t *testing.T) {
	f := syncWS(t)
	c := addCard(t, "Write the runbook", "--body", "steps")
	_, out, errOut, err := runTQ(t, "hub", "push", c.ID)
	if err != nil || !strings.Contains(out, "waiting for the send/skip decision") {
		t.Fatalf("%v %q %s", err, out, errOut)
	}
	_, out, _, err = runTQ(t, "hub", "sync", "outbox", "--json")
	var rows []store.OutboxRow
	if err != nil || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 || rows[0].Action != store.OutboxCreateCard || rows[0].DecisionID == "" {
		t.Fatalf("%v %s", err, out)
	}
	if _, _, errOut, err := runTQ(t, "hub", "answer", c.ID, rows[0].DecisionID, "--choice", "send"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	if _, _, errOut, err := runTQ(t, "hub", "sync", "now"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	if len(f.created) != 1 || f.created[0].Title != "Write the runbook" {
		t.Fatalf("%+v", f.created)
	}
	_, out, _, _ = runTQ(t, "hub", "show", c.ID, "--json")
	if !strings.Contains(out, `"external_id": "501"`) {
		t.Fatalf("not linked: %s", out)
	}
	if _, _, _, err := runTQ(t, "hub", "push", c.ID); err == nil {
		t.Fatal("pushed a linked card")
	}
}

// The real wiring: a workspace without a data block redacts contact PII on the way to the tracker,
// even on a line the tq:allow-pii marker kept unmasked in the store.
func TestSyncExitRedactsMarkedContactPII(t *testing.T) {
	f := syncWS(t)
	if _, _, errOut, err := runTQ(t, "hub", "sync", "now"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	ws, err := hubWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ws.Name, hubClasses(ws))
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CardByExternal("github", "7")
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	text := "Done. Ping tester@acme.example tq:allow-pii"
	if _, err := st.EnqueueOutbox(c.ID, store.OutboxCommentResults, map[string]any{"text": text}); err != nil {
		st.Close()
		t.Fatal(err)
	}
	st.Close()
	if _, _, errOut, err := runTQ(t, "hub", "sync", "now"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.comments) != 1 {
		t.Fatalf("%v", f.comments)
	}
	if strings.Contains(f.comments[0], "tester@acme.example") || !strings.Contains(f.comments[0], "Done.") {
		t.Fatalf("contact PII left the machine: %q", f.comments[0])
	}
}

func TestHubSyncRetry(t *testing.T) {
	syncWS(t)
	if _, _, errOut, err := runTQ(t, "hub", "sync", "now"); err != nil {
		t.Fatalf("%v %s", err, errOut)
	}
	ws, _ := hubWorkspace("acme")
	st, err := store.Open(ws.Name, hubClasses(ws))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := st.CardByExternal("github", "7")
	row, err := st.EnqueueOutbox(c.ID, store.OutboxCommentResults, map[string]any{"text": "hello"})
	if err == nil {
		_, err = st.MarkOutbox(row.ID, store.OutboxFailed, "502 bad gateway")
	}
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, _, _, err := runTQ(t, "hub", "sync", "retry", row.ID); err == nil {
		t.Fatal("an agent retried an outbound write")
	}
	unsetenv(t, "CLAUDECODE")
	_, out, errOut, err := runTQ(t, "hub", "sync", "retry", row.ID)
	if err != nil || !strings.Contains(out, "sent") {
		t.Fatalf("%v %q %s", err, out, errOut)
	}
	if _, _, _, err := runTQ(t, "hub", "sync", "retry", row.ID); err == nil {
		t.Fatal("retried a sent row")
	}
}

func TestHubPushRefusesAgents(t *testing.T) {
	syncWS(t)
	c := addCard(t, "x")
	t.Setenv("CLAUDECODE", "1")
	if _, _, _, err := runTQ(t, "hub", "push", c.ID); err == nil || !errors.Is(err, runner.ErrHumanOnly) && !strings.Contains(err.Error(), "human") {
		t.Fatalf("%v", err)
	}
}

func TestHubSyncWithoutTracker(t *testing.T) {
	hubWS(t)
	if _, _, _, err := runTQ(t, "hub", "sync", "now"); err == nil || !strings.Contains(err.Error(), "no tracker block") {
		t.Fatalf("%v", err)
	}
}

func TestSyncSiblingChainsOnApprove(t *testing.T) {
	syncWS(t)
	ws, err := hubWorkspace("acme")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := newHubService(ws, func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Store().Close()
	called := false
	svc.OnApprove = func(store.Card, store.Run) { called = true }
	run := syncSibling(func(string) {})(ws, svc)
	if run == nil {
		t.Fatal("no engine for a workspace with a tracker")
	}
	svc.OnApprove(store.Card{}, store.Run{})
	if !called {
		t.Fatal("the review export hook was dropped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); run(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the engine loop did not stop")
	}
	other := *ws
	other.Manifest.Tracker = nil
	if syncSibling(func(string) {})(&other, svc) != nil {
		t.Fatal("an engine for a workspace without a tracker")
	}
}
