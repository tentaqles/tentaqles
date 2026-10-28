package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
)

// fakeTracker is an in-memory tracker: items keyed by id, a call log, and scripted 429s.
type fakeTracker struct {
	mu       gosync.Mutex
	statuses []Status
	items    map[string]*Item
	changed  []string // ids ListChanged returns next
	wm       time.Time
	next     int
	limited  map[string]int // method -> remaining 429s
	calls    []string
	comments []string // "id: text"
	created  []NewItem
}

func newFake() *fakeTracker {
	return &fakeTracker{
		statuses: []Status{{"To Do", CategoryTodo}, {"In Progress", CategoryActive}, {"QA", CategoryActive},
			{"In Review", CategoryReview}, {"Done", CategoryDone}},
		items: map[string]*Item{}, limited: map[string]int{}, wm: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}
}

func (f *fakeTracker) put(it Item) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if it.Rev == "" {
		it.Rev = "1"
	}
	c := it
	f.items[it.ID] = &c
	f.changed = append(f.changed, it.ID)
}

// remoteMove is a move someone made on the company board.
func (f *fakeTracker) remoteMove(id, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := f.items[id]
	it.Status = status
	it.Rev = fmt.Sprint(len(it.Rev) + 1)
	f.changed = append(f.changed, id)
}

func (f *fakeTracker) limit(method string) error {
	if f.limited[method] > 0 {
		f.limited[method]--
		return &RateLimitError{Provider: "github", RetryAfter: 2 * time.Second}
	}
	return nil
}

func (f *fakeTracker) log(s string) { f.calls = append(f.calls, s) }

func (f *fakeTracker) Name() string                              { return "github" }
func (f *fakeTracker) Check(ctx context.Context) (string, error) { return "tester", nil }
func (f *fakeTracker) Statuses(ctx context.Context) ([]Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Status(nil), f.statuses...), nil
}
func (f *fakeTracker) ListChanged(ctx context.Context, since time.Time) ([]Item, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.limit("list"); err != nil {
		return nil, time.Time{}, err
	}
	var out []Item
	for _, id := range f.changed {
		out = append(out, *f.items[id])
	}
	f.changed = nil
	f.wm = f.wm.Add(time.Minute)
	return out, f.wm, nil
}
func (f *fakeTracker) Get(ctx context.Context, id string) (Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return *it, nil
}
func (f *fakeTracker) Move(ctx context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.limit("move"); err != nil {
		return err
	}
	f.log("move " + id + " " + status)
	f.items[id].Status = status
	f.items[id].Rev += "+"
	return nil
}
func (f *fakeTracker) Comment(ctx context.Context, id, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.limit("comment"); err != nil {
		return err
	}
	f.log("comment " + id)
	f.comments = append(f.comments, id+": "+markdown)
	return nil
}
func (f *fakeTracker) Create(ctx context.Context, in NewItem) (Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	it := Item{ID: fmt.Sprint(900 + f.next), URL: fmt.Sprintf("https://tracker.example/acme/%d", 900+f.next), Title: in.Title,
		Body: in.Body, Status: "To Do", StatusCategory: CategoryTodo, ParentID: in.ParentID, Rev: "1"}
	f.items[it.ID] = &it
	f.created = append(f.created, in)
	f.log("create " + in.Title)
	return it, nil
}
func (f *fakeTracker) Update(ctx context.Context, id string, title, body *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("update " + id)
	return nil
}
func (f *fakeTracker) SetLabels(ctx context.Context, id string, add, remove []string) error {
	return nil
}

func (f *fakeTracker) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type rig struct {
	t     *testing.T
	st    *store.Store
	fake  *fakeTracker
	eng   *Engine
	slept []time.Duration
}

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
}

// newRig opens a store for acme whose stored text keeps e-mail addresses (classes public), so the
// outbound redaction is what the tests see.
func newRig(t *testing.T, mut ...func(*EngineConfig)) *rig {
	t.Helper()
	isolateHome(t)
	st, err := store.Open("acme", []string{"public"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &rig{t: t, st: st, fake: newFake()}
	cfg := EngineConfig{Workspace: "acme", Store: st,
		Tracker: &manifest.Tracker{Provider: "github", Scope: map[string]string{"org": "acme"},
			Lanes: map[string]string{"QA": "review"}},
		ExitClasses: []string{"checksummed", "pii", "secrets"},
		Adapter:     func(context.Context) (Adapter, error) { return r.fake, nil },
		Sleep: func(ctx context.Context, d time.Duration) error {
			r.slept = append(r.slept, d)
			return nil
		},
	}
	for _, m := range mut {
		m(&cfg)
	}
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.eng = eng
	return r
}

func (r *rig) tick() Report {
	r.t.Helper()
	rep, err := r.eng.Tick(context.Background())
	if err != nil {
		r.t.Fatalf("tick: %v (%+v)", err, rep)
	}
	return rep
}

func (r *rig) flush() Report {
	r.t.Helper()
	rep, err := r.eng.Flush(context.Background())
	if err != nil {
		r.t.Fatalf("flush: %v (%+v)", err, rep)
	}
	return rep
}

func (r *rig) card(provider, id string) store.Card {
	r.t.Helper()
	c, err := r.st.CardByExternal(provider, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *rig) outbox(action string) []store.OutboxRow {
	r.t.Helper()
	rows, err := r.st.ListOutbox(store.OutboxFilter{Action: action})
	if err != nil {
		r.t.Fatal(err)
	}
	return rows
}

// linked imports one item at status and returns its card.
func (r *rig) linked(id, status, category string) store.Card {
	r.t.Helper()
	r.fake.put(Item{ID: id, Key: "#" + id, URL: "https://tracker.example/acme/" + id, Title: "Item " + id, Body: "body " + id,
		Status: status, StatusCategory: category})
	r.tick()
	return r.card("github", id)
}

func hasComment(t *testing.T, st *store.Store, cardID, sub string) bool {
	t.Helper()
	cs, err := st.Comments(cardID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if strings.Contains(c.Body, sub) {
			return true
		}
	}
	return false
}

func TestPullImportsMapsLanesAndWatermark(t *testing.T) {
	r := newRig(t)
	r.fake.put(Item{ID: "10", URL: "https://tracker.example/acme/10", Title: "Parent", Body: "the parent", Status: "In Progress",
		StatusCategory: CategoryActive, Acceptance: "- it works"})
	r.fake.put(Item{ID: "11", Title: "Child", Status: "QA", StatusCategory: CategoryActive, ParentID: "10"})
	r.fake.put(Item{ID: "12", Title: "Odd", Status: "Parked", StatusCategory: "weird"})
	rep := r.tick()
	if rep.Created != 3 {
		t.Fatalf("created %d: %+v", rep.Created, rep)
	}
	p := r.card("github", "10")
	if p.Lane != "in progress" || p.ExternalLane != "In Progress" || p.ExternalURL == "" || !strings.Contains(p.Body, "it works") {
		t.Fatalf("parent %+v", p)
	}
	c := r.card("github", "11")
	if c.Lane != "review" || c.ParentID != p.ID {
		t.Fatalf("child: the manifest maps QA to review and keeps the parent: %+v", c)
	}
	if o := r.card("github", "12"); o.Lane != "backlog" {
		t.Fatalf("an unmapped status lands in backlog: %s", o.Lane)
	}
	st, ok, _ := r.st.SyncState("github", "org=acme")
	if !ok || st.Watermark == "" || st.LastOKAt == "" || st.LastError != "" {
		t.Fatalf("state %+v", st)
	}
	if lane, ok, _ := r.st.LaneFor("github", "org=acme", "In Review"); !ok || lane != "review" {
		t.Fatalf("lane map: %q %v", lane, ok)
	}

	// The tracker edits the text: an unchanged local copy follows it; a locally edited one keeps its own.
	r.fake.mu.Lock()
	r.fake.items["10"].Title = "Parent renamed"
	r.fake.items["11"].Title = "Child renamed"
	r.fake.changed = []string{"10", "11"}
	r.fake.mu.Unlock()
	mine := "My child"
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Title: &mine}, "tester"); err != nil {
		t.Fatal(err)
	}
	rep = r.tick()
	if rep.Updated != 2 || rep.Created != 0 {
		t.Fatalf("%+v", rep)
	}
	if got := r.card("github", "10").Title; got != "Parent renamed" {
		t.Fatalf("parent title %q", got)
	}
	if got := r.card("github", "11").Title; got != "My child" {
		t.Fatalf("a local edit since the last sync is kept: %q", got)
	}
}

func TestSyncMoveNeverStartsARun(t *testing.T) {
	r := newRig(t)
	c := r.linked("20", "To Do", CategoryTodo)
	tags := []string{store.TagAgentReady}
	if _, err := r.st.UpdateCard(c.ID, store.CardUpdate{Tags: &tags}, "tester"); err != nil {
		t.Fatal(err)
	}
	svc, err := runner.New(runner.Config{Workspace: "acme", Store: r.st})
	if err != nil {
		t.Fatal(err)
	}
	r.fake.remoteMove("20", "In Progress")
	if rep := r.tick(); rep.Moved != 1 {
		t.Fatalf("%+v", rep)
	}
	if got := r.card("github", "20"); got.Lane != "in progress" || got.ExternalLane != "In Progress" {
		t.Fatalf("%+v", got)
	}
	_ = svc.Tick(context.Background())
	if _, ok, _ := r.st.ActiveRun(c.ID); ok {
		t.Fatal("a sync move into an active lane started a run")
	}
	if runs, _ := r.st.RunsForCard(c.ID); len(runs) != 0 {
		t.Fatalf("runs %v", runs)
	}
	if rows := r.outbox(""); len(rows) != 0 {
		t.Fatalf("an inbound move queued an outbound write: %+v", rows)
	}
}

func TestConflictTrackerWinsAndStaleOutboundSkipped(t *testing.T) {
	r := newRig(t)
	c := r.linked("30", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	r.fake.remoteMove("30", "Done")
	rep := r.tick()
	if rep.Conflicts != 1 {
		t.Fatalf("%+v", rep)
	}
	got := r.card("github", "30")
	if got.Lane != "done" {
		t.Fatalf("the tracker wins an inbound conflict: %s", got.Lane)
	}
	if !hasComment(t, r.st, c.ID, "conflict") {
		t.Fatal("no system comment records the conflict")
	}
	rows := r.outbox(store.OutboxMoveOnStart)
	if len(rows) != 1 || rows[0].Status != store.OutboxSkipped {
		t.Fatalf("the local move is skipped against the newer remote state: %+v", rows)
	}
	for _, call := range r.fake.callLog() {
		if strings.HasPrefix(call, "move ") {
			t.Fatalf("moved the item back: %v", r.fake.callLog())
		}
	}
	if !hasComment(t, r.st, c.ID, "not moving") {
		t.Fatal("no comment on the skipped outbound move")
	}
}

func TestPolicyAutoSendsMoveOnStart(t *testing.T) {
	r := newRig(t)
	c := r.linked("40", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	rep := r.flush()
	if rep.Queued != 1 || rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if calls := r.fake.callLog(); len(calls) != 1 || calls[0] != "move 40 In Progress" {
		t.Fatalf("%v", calls)
	}
	if got := r.card("github", "40"); got.ExternalLane != "In Progress" {
		t.Fatalf("external lane %q", got.ExternalLane)
	}
	if rows := r.outbox(store.OutboxMoveOnStart); rows[0].Status != store.OutboxSent || rows[0].SentAt == "" {
		t.Fatalf("%+v", rows)
	}
	// Our own move comes back from the tracker: no conflict, no move.
	r.fake.changed = []string{"40"}
	if rep := r.tick(); rep.Moved != 0 || rep.Conflicts != 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestPolicyAskCreatesDecisionThenSendOrSkip(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncPolicy(store.OutboxMoveOnStart, store.PolicyAsk); err != nil {
		t.Fatal(err)
	}
	a := r.linked("50", "To Do", CategoryTodo)
	b := r.linked("51", "To Do", CategoryTodo)
	for _, c := range []store.Card{a, b} {
		if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
			t.Fatal(err)
		}
	}
	if rep := r.flush(); rep.Asked != 2 || rep.Sent != 0 {
		t.Fatalf("%+v", rep)
	}
	if rep := r.flush(); rep.Asked != 0 {
		t.Fatalf("asked twice: %+v", rep)
	}
	rows := r.outbox(store.OutboxMoveOnStart)
	if len(rows) != 2 || rows[0].DecisionID == "" || rows[0].Status != store.OutboxPending {
		t.Fatalf("%+v", rows)
	}
	d, err := r.st.Decision(rows[0].DecisionID)
	if err != nil || d.Kind != store.KindPermission || strings.Join(d.Options, ",") != "send,skip" {
		t.Fatalf("%+v %v", d, err)
	}
	if len(r.fake.callLog()) != 0 {
		t.Fatal("ask sent before the answer")
	}
	if _, err := r.st.AnswerHub(rows[0].DecisionID, "send", "", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.AnswerHub(rows[1].DecisionID, "skip", "", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 || rep.Skipped != 1 {
		t.Fatalf("%+v", rep)
	}
	rows = r.outbox(store.OutboxMoveOnStart)
	if rows[0].Status != store.OutboxSent || rows[1].Status != store.OutboxSkipped {
		t.Fatalf("%+v", rows)
	}
	if calls := r.fake.callLog(); len(calls) != 1 || calls[0] != "move 50 In Progress" {
		t.Fatalf("%v", calls)
	}
}

func TestPolicyOffDrops(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncPolicy(store.OutboxMoveOnStart, store.PolicyOff); err != nil {
		t.Fatal(err)
	}
	c := r.linked("60", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Dropped != 1 || rep.Sent != 0 || rep.Asked != 0 {
		t.Fatalf("%+v", rep)
	}
	rows := r.outbox(store.OutboxMoveOnStart)
	if len(rows) != 1 || rows[0].Status != store.OutboxSkipped || rows[0].DecisionID != "" || !strings.Contains(rows[0].LastError, "off") {
		t.Fatalf("%+v", rows)
	}
	if len(r.fake.callLog()) != 0 {
		t.Fatal("off sent anyway")
	}
}

func TestRateLimitRetriesThenGivesUpForTheTick(t *testing.T) {
	r := newRig(t)
	c := r.linked("70", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	r.fake.limited["move"] = 2
	rep := r.flush()
	if rep.Sent != 1 || rep.Retried != 2 || len(r.slept) != 2 || r.slept[0] != 2*time.Second {
		t.Fatalf("two 429s then success: %+v slept %v", rep, r.slept)
	}

	// Three 429s in a row: the row stays pending with the error; the poll records last_error.
	b := r.linked("71", "To Do", CategoryTodo)
	if _, err := r.st.MoveCard(b.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	r.fake.limited["move"] = 3
	if _, err := r.eng.Flush(context.Background()); !isRateLimit(err) {
		t.Fatalf("got %v", err)
	}
	rows, _ := r.st.ListOutbox(store.OutboxFilter{CardID: b.ID})
	if len(rows) != 1 || rows[0].Status != store.OutboxPending || rows[0].Attempts != 1 || !strings.Contains(rows[0].LastError, "rate limited") {
		t.Fatalf("%+v", rows)
	}
	if st, _, _ := r.st.SyncState("github", "org=acme"); !strings.Contains(st.LastError, "rate limited") {
		t.Fatalf("send error not in sync_state: %+v", st)
	}
	r.fake.limited["list"] = 3
	r.fake.put(Item{ID: "72", Title: "late", Status: "To Do", StatusCategory: CategoryTodo})
	before, _, _ := r.st.SyncState("github", "org=acme")
	if _, err := r.eng.Tick(context.Background()); err == nil {
		t.Fatal("a poll that stayed rate limited reported success")
	}
	after, _, _ := r.st.SyncState("github", "org=acme")
	if after.Watermark != before.Watermark || !strings.Contains(after.LastError, "rate limited") || after.LastErrorAt == "" {
		t.Fatalf("before %+v after %+v", before, after)
	}
	// The send does not depend on the poll: the waiting row went out in the same tick.
	if rows, _ := r.st.ListOutbox(store.OutboxFilter{CardID: b.ID}); rows[0].Status != store.OutboxSent {
		t.Fatalf("%+v", rows)
	}
	// The next tick recovers: the item comes in and the error clears.
	rep = r.tick()
	if rep.Created != 1 {
		t.Fatalf("%+v", rep)
	}
	if st, _, _ := r.st.SyncState("github", "org=acme"); st.LastError != "" {
		t.Fatalf("%+v", st)
	}
}

func TestRedactionOfOutboundText(t *testing.T) {
	r := newRig(t)
	c := r.linked("80", "In Progress", CategoryActive)
	fakeToken := "ghp_" + strings.Repeat("x", 36) // built at run time: a known-fake fixture
	text := "Done. Ping tester@acme.example tq:allow-pii\nCPF 529.982.247-25 and token " + fakeToken
	if _, err := r.st.EnqueueOutbox(c.ID, store.OutboxCommentResults, map[string]any{"text": text}); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.comments) != 1 {
		t.Fatalf("%v", r.fake.comments)
	}
	got := r.fake.comments[0]
	for _, leak := range []string{"tester@acme.example", "529.982.247-25", fakeToken, "tq:allow-pii"} {
		if strings.Contains(got, leak) {
			t.Fatalf("%q left the machine: %s", leak, got)
		}
	}
	if !strings.Contains(got, "[REDACTED:email]") || !strings.Contains(got, "Done.") {
		t.Fatalf("%s", got)
	}
}

func approve(t *testing.T, st *store.Store, c store.Card) {
	t.Helper()
	if _, err := st.MoveCard(c.ID, "done", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := st.Log(activity.Entry{Actor: "tester", Verb: activity.VerbReviewApprove, CardID: c.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestApproveQueuesReviewMoveAndResultComment(t *testing.T) {
	r := newRig(t)
	c := r.linked("90", "In Progress", CategoryActive)
	if _, err := r.st.AddComment(c.ID, store.AuthorAgent, "agent", "PR https://github.com/acme/app/pull/7", ""); err != nil {
		t.Fatal(err)
	}
	approve(t, r.st, c)
	rep := r.flush()
	if rep.Sent != 2 {
		t.Fatalf("%+v", rep)
	}
	if rows := r.outbox(store.OutboxCloseItem); len(rows) != 0 {
		t.Fatalf("an approval is not a close: %+v", rows)
	}
	calls := r.fake.callLog()
	if len(calls) != 2 || calls[0] != "move 90 In Review" || calls[1] != "comment 90" {
		t.Fatalf("%v", calls)
	}
	if !strings.Contains(r.fake.comments[0], "https://github.com/acme/app/pull/7") || !strings.Contains(r.fake.comments[0], "approved") {
		t.Fatalf("%v", r.fake.comments)
	}

	// An unlinked child of a linked parent: the comment goes on the parent's item.
	p := r.linked("91", "In Progress", CategoryActive)
	kid, err := r.st.AddCard(store.NewCard{Title: "local child", ParentID: p.ID}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	approve(t, r.st, kid)
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if last := r.fake.comments[len(r.fake.comments)-1]; !strings.HasPrefix(last, "91: ") || !strings.Contains(last, "local child") {
		t.Fatalf("%v", r.fake.comments)
	}
}

func TestDoneWithoutApproveClosesUnderPolicy(t *testing.T) {
	r := newRig(t, func(c *EngineConfig) { c.Now = func() time.Time { return time.Now().Add(time.Minute) } })
	c := r.linked("95", "In Progress", CategoryActive)
	if _, err := r.st.MoveCard(c.ID, "done", "tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Asked != 1 {
		t.Fatalf("close_item asks by default: %+v", rep)
	}
	if err := r.st.SetSyncPolicy(store.OutboxCloseItem, store.PolicyAuto); err != nil {
		t.Fatal(err)
	}
	rows := r.outbox(store.OutboxCloseItem)
	if _, err := r.st.AnswerHub(rows[0].DecisionID, "send", "", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if calls := r.fake.callLog(); calls[len(calls)-1] != "move 95 Done" {
		t.Fatalf("%v", calls)
	}
}

func TestPushCreatesChildOfParentItem(t *testing.T) {
	r := newRig(t)
	p := r.linked("100", "In Progress", CategoryActive)
	kid, err := r.st.AddCard(store.NewCard{Title: "Write docs", Body: "mail tester@acme.example", ParentID: p.ID}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	row, err := r.eng.Push(context.Background(), kid.ID)
	if err != nil || row.Action != store.OutboxCreateCard {
		t.Fatalf("%+v %v", row, err)
	}
	if _, err := r.eng.Push(context.Background(), kid.ID); err == nil {
		t.Fatal("a second push while one is pending")
	}
	if rep := r.flush(); rep.Asked != 1 {
		t.Fatalf("create_card asks by default: %+v", rep)
	}
	row, _ = r.st.OutboxRow(row.ID)
	if _, err := r.st.AnswerHub(row.DecisionID, "send", "", "hub:tester"); err != nil {
		t.Fatal(err)
	}
	if rep := r.flush(); rep.Sent != 1 {
		t.Fatalf("%+v", rep)
	}
	if len(r.fake.created) != 1 || r.fake.created[0].ParentID != "100" || strings.Contains(r.fake.created[0].Body, "tester@acme.example") {
		t.Fatalf("%+v", r.fake.created)
	}
	got, _ := r.st.Card(kid.ID)
	if got.ExternalProvider != "github" || got.ExternalID != "901" || got.ExternalURL == "" {
		t.Fatalf("%+v", got)
	}
	if _, err := r.eng.Push(context.Background(), kid.ID); err == nil {
		t.Fatal("pushed a linked card")
	}
}

func TestLeaseKeepsASecondEngineOut(t *testing.T) {
	r := newRig(t)
	other, err := NewEngine(EngineConfig{Workspace: "acme", Store: r.st, Tracker: r.eng.cfg.Tracker,
		Adapter: func(context.Context) (Adapter, error) { return r.fake, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := other.acquire()
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := r.eng.Tick(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	other.release()
	r.tick()
}

func TestDisabledAndNoTracker(t *testing.T) {
	r := newRig(t)
	if err := r.st.SetSyncEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Tick(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("%v", err)
	}
	if _, err := NewEngine(EngineConfig{Workspace: "acme", Store: r.st}); !errors.Is(err, ErrNoTracker) {
		t.Fatalf("%v", err)
	}
}

func TestOverviewAndOutboxJSON(t *testing.T) {
	r := newRig(t)
	r.linked("110", "To Do", CategoryTodo)
	ov, err := r.eng.Overview()
	if err != nil {
		t.Fatal(err)
	}
	if ov.Provider != "github" || ov.Scope != "org=acme" || !ov.Enabled || len(ov.States) != 1 || len(ov.LaneMap) == 0 || ov.NextAt == "" {
		t.Fatalf("%+v", ov)
	}
	if _, err := json.Marshal(ov); err != nil {
		t.Fatal(err)
	}
}

func TestRunLoopStopsWithContext(t *testing.T) {
	r := newRig(t, func(c *EngineConfig) { c.OutboxEvery = 20 * time.Millisecond })
	c := r.linked("120", "To Do", CategoryTodo)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.eng.Run(ctx) }()
	if _, err := r.st.MoveCard(c.ID, "in progress", "tester"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := r.outbox(store.OutboxMoveOnStart)
		if len(rows) == 1 && rows[0].Status == store.OutboxSent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the loop never sent the move: %+v", rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
