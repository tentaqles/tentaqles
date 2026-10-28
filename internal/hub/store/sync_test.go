package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func entryFor(who string) activity.Entry {
	return activity.Entry{Actor: "hub:" + who, Verb: activity.VerbSettingsUpdate}
}

func TestSchemaV2AddsSyncTables(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if v, _ := s.SchemaVersion(); v < 2 {
		t.Fatalf("schema %d, want >= 2", v)
	}
	for _, tbl := range []string{"sync_state", "sync_outbox", "lane_map"} {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing (%d, %v)", tbl, n, err)
		}
	}
	for _, col := range []string{"external_rev", "external_synced_at"} {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('cards') WHERE name = ?`, col).Scan(&n); err != nil || n != 1 {
			t.Fatalf("cards.%s missing", col)
		}
	}
}

// TestSchemaV1DatabaseMigratesInPlace: a hub.db written by the previous tq keeps its cards.
func TestSchemaV1DatabaseMigratesInPlace(t *testing.T) {
	isolate(t)
	all := migrations
	migrations = all[:1]
	s, err := Open("acme", []string{"all"})
	if err != nil {
		migrations = all
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`INSERT INTO cards(id, title, lane, kind, external_provider, external_id, created_at, updated_at)
		VALUES ('c-aaaaaaaaaaaa', 'old card', 'backlog', 'feature', 'github', '7', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	s.Close()
	migrations = all
	if err != nil {
		t.Fatal(err)
	}
	c := Card{ID: "c-aaaaaaaaaaaa"}
	s2 := mustOpen(t, "acme")
	if v, _ := s2.SchemaVersion(); v != len(migrations) {
		t.Fatalf("schema %d", v)
	}
	got, err := s2.Card(c.ID)
	if err != nil || got.Title != "old card" || got.ExternalID != "7" || got.ExternalRev != "" || got.ExternalSyncedAt != "" {
		t.Fatalf("card after migration: %+v, %v", got, err)
	}
	if byExt, err := s2.CardByExternal("github", "7"); err != nil || byExt.ID != c.ID {
		t.Fatalf("CardByExternal after migration: %+v %v", byExt, err)
	}
}

func TestExternalIDIsUnique(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	a, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	b, _ := s.AddCard(NewCard{Title: "b"}, "tester")
	// Unlinked cards (NULL provider/id) never collide.
	if err := s.LinkCard(a.ID, ExternalLink{Provider: "github", ID: "7", URL: "https://example.test/7"}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkCard(b.ID, ExternalLink{Provider: "github", ID: "7"}); !errors.Is(err, ErrAlreadyLinked) {
		t.Fatalf("second link of the same item: %v", err)
	}
	if err := s.LinkCard(b.ID, ExternalLink{Provider: "jira", ID: "7"}); err != nil {
		t.Fatalf("same id, other provider: %v", err)
	}
	got, err := s.CardByExternal("github", "7")
	if err != nil || got.ID != a.ID || got.ExternalURL != "https://example.test/7" {
		t.Fatalf("CardByExternal: %+v %v", got, err)
	}
	if _, err := s.CardByExternal("github", "8"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing external: %v", err)
	}
	if err := s.LinkCard(a.ID, ExternalLink{Provider: "", ID: "1"}); err == nil {
		t.Fatal("empty provider accepted")
	}
}

func TestUpsertLinkedCardCreatesThenUpdates(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	parent, _ := s.AddCard(NewCard{Title: "parent", Kind: "project"}, "tester")
	in := LinkedCard{
		Link:  ExternalLink{Provider: "azure-devops", ID: "123", URL: "https://example.test/123", Lane: "Active", Rev: "4"},
		Title: "Fix login for tester@example.com", Body: "body", Kind: "fix", ParentID: parent.ID, Tags: []string{"bug", "bug"},
	}
	c, created, err := s.UpsertLinkedCard(in, "sync")
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if c.ExternalProvider != "azure-devops" || c.ExternalID != "123" || c.ExternalLane != "Active" || c.ExternalRev != "4" ||
		c.ExternalSyncedAt == "" || c.Kind != "fix" || c.ParentID != parent.ID || c.Lane != "backlog" || len(c.Tags) != 1 {
		t.Fatalf("created card: %+v", c)
	}
	if strings.Contains(c.Title, "tester@example.com") {
		t.Fatalf("inbound text not masked: %q", c.Title)
	}
	// Second pass: same external id, new rev; text kept unless UpdateText.
	in.Link.Rev, in.Link.Lane, in.Title = "5", "Resolved", "new title"
	c2, created, err := s.UpsertLinkedCard(in, "sync")
	if err != nil || created || c2.ID != c.ID {
		t.Fatalf("update: %+v %v %v", c2, created, err)
	}
	if c2.ExternalRev != "5" || c2.ExternalLane != "Resolved" || c2.Title == "new title" || c2.Lane != "backlog" {
		t.Fatalf("update changed the wrong columns: %+v", c2)
	}
	in.UpdateText = true
	c3, _, err := s.UpsertLinkedCard(in, "sync")
	if err != nil || c3.Title != "new title" || c3.Body != "body" {
		t.Fatalf("UpdateText: %+v %v", c3, err)
	}
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM cards WHERE external_id = '123'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d cards for one external item", n)
	}
	if _, _, err := s.UpsertLinkedCard(LinkedCard{Link: ExternalLink{Provider: "github", ID: "1"}}, "sync"); err == nil {
		t.Fatal("a new linked card without a title was accepted")
	}
	links, err := s.LinkedCards("azure-devops")
	if err != nil || len(links) != 1 || links[0].ID != c.ID {
		t.Fatalf("LinkedCards: %+v %v", links, err)
	}
}

func TestMarkSynced(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	if err := s.MarkSynced(c.ID, "Done", "9"); err == nil {
		t.Fatal("MarkSynced on an unlinked card")
	}
	_ = s.LinkCard(c.ID, ExternalLink{Provider: "linear", ID: "ACME-1"})
	if err := s.MarkSynced(c.ID, "Done", "9"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Card(c.ID)
	if got.ExternalLane != "Done" || got.ExternalRev != "9" || got.ExternalSyncedAt == "" {
		t.Fatalf("%+v", got)
	}
}

func TestSyncStateRoundTrip(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if _, ok, err := s.SyncState("jira", "acme/Platform"); ok || err != nil {
		t.Fatalf("empty state: %v %v", ok, err)
	}
	if err := s.RecordSyncOK("jira", "acme/Platform", "2026-10-27T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSyncError("jira", "acme/Platform", "429 from tester@example.com"); err != nil {
		t.Fatal(err)
	}
	st, ok, err := s.SyncState("jira", "acme/Platform")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if st.Workspace != "acme" || st.Watermark != "2026-10-27T10:00:00Z" || st.LastOKAt == "" || st.LastErrorAt == "" ||
		!strings.Contains(st.LastError, "429") || strings.Contains(st.LastError, "tester@example.com") {
		t.Fatalf("%+v", st)
	}
	// A later OK clears the error and keeps the newer watermark.
	if err := s.RecordSyncOK("jira", "acme/Platform", "2026-10-27T11:00:00Z"); err != nil {
		t.Fatal(err)
	}
	st, _, _ = s.SyncState("jira", "acme/Platform")
	if st.LastError != "" || st.LastErrorAt != "" || st.Watermark != "2026-10-27T11:00:00Z" {
		t.Fatalf("after OK: %+v", st)
	}
	if err := s.SetSyncState(SyncState{Provider: "github", Scope: "acme", Watermark: "w"}); err != nil {
		t.Fatal(err)
	}
	all, err := s.SyncStates()
	if err != nil || len(all) != 2 || all[0].Provider != "github" {
		t.Fatalf("SyncStates: %+v %v", all, err)
	}
	if err := s.SetSyncState(SyncState{Provider: ""}); err == nil {
		t.Fatal("empty provider accepted")
	}
}

func TestOutboxLifecycle(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	c, _ := s.AddCard(NewCard{Title: "a"}, "tester")
	if _, err := s.EnqueueOutbox(c.ID, "nonsense", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := s.EnqueueOutbox("c-missingmissin", OutboxCommentResults, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card: %v", err)
	}
	row, err := s.EnqueueOutbox(c.ID, OutboxCommentResults, map[string]any{"markdown": "done by tester@example.com", "n": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID("o", row.ID) || row.Status != OutboxPending || row.Attempts != 0 || row.CreatedAt == "" {
		t.Fatalf("%+v", row)
	}
	var p map[string]any
	if err := json.Unmarshal(row.Payload, &p); err != nil || strings.Contains(p["markdown"].(string), "tester@example.com") || p["n"] != float64(2) {
		t.Fatalf("payload not masked or not JSON: %s %v", row.Payload, err)
	}
	move, _ := s.EnqueueOutbox(c.ID, OutboxMoveOnStart, map[string]string{"status": "Active"})
	if err := s.SetOutboxDecision(move.ID, "d-abcdefghijkl"); err != nil {
		t.Fatal(err)
	}
	pend, err := s.ListOutbox(OutboxFilter{Status: OutboxPending})
	if err != nil || len(pend) != 2 || pend[0].ID != row.ID {
		t.Fatalf("pending: %+v %v", pend, err)
	}
	if got, _ := s.OutboxRow(move.ID); got.DecisionID != "d-abcdefghijkl" {
		t.Fatalf("decision id: %+v", got)
	}
	if got, err := s.OutboxByDecision("d-abcdefghijkl"); err != nil || got.ID != move.ID {
		t.Fatalf("OutboxByDecision: %+v %v", got, err)
	}
	if err := s.OutboxAttempt(row.ID, "429 rate limited"); err != nil {
		t.Fatal(err)
	}
	r1, _ := s.OutboxRow(row.ID)
	if r1.Attempts != 1 || r1.Status != OutboxPending || r1.LastError != "429 rate limited" {
		t.Fatalf("after attempt: %+v", r1)
	}
	sent, err := s.MarkOutbox(row.ID, OutboxSent, "")
	if err != nil || sent.Status != OutboxSent || sent.SentAt == "" || sent.Attempts != 2 {
		t.Fatalf("sent: %+v %v", sent, err)
	}
	if _, err := s.MarkOutbox(row.ID, OutboxFailed, "late"); !errors.Is(err, ErrOutboxFinal) {
		t.Fatalf("a sent row changed: %v", err)
	}
	failed, err := s.MarkOutbox(move.ID, OutboxFailed, "boom")
	if err != nil || failed.Status != OutboxFailed || failed.LastError != "boom" {
		t.Fatalf("failed: %+v %v", failed, err)
	}
	if retry, err := s.MarkOutbox(move.ID, OutboxPending, ""); err != nil || retry.Status != OutboxPending {
		t.Fatalf("retry a failed row: %+v %v", retry, err)
	}
	if _, err := s.MarkOutbox(move.ID, "bogus", ""); err == nil {
		t.Fatal("bogus status accepted")
	}
	if _, err := s.MarkOutbox("o-missingmissin", OutboxSent, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing row: %v", err)
	}
	byCard, _ := s.ListOutbox(OutboxFilter{CardID: c.ID})
	if len(byCard) != 2 {
		t.Fatalf("by card: %d", len(byCard))
	}
}

func TestLaneMapReplaceAndLookup(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.ReplaceLaneMap("jira", "acme/Platform", []LaneMapEntry{
		{ExternalStatus: "In Review", Lane: "review"}, {ExternalStatus: "QA", Lane: "review"}, {ExternalStatus: "To Do", Lane: "todo"},
	}); err != nil {
		t.Fatal(err)
	}
	if lane, ok, err := s.LaneFor("jira", "acme/Platform", "in review"); err != nil || !ok || lane != "review" {
		t.Fatalf("LaneFor case-insensitive: %q %v %v", lane, ok, err)
	}
	if _, ok, _ := s.LaneFor("jira", "other", "QA"); ok {
		t.Fatal("scope leaked")
	}
	if err := s.ReplaceLaneMap("jira", "acme/Platform", []LaneMapEntry{{ExternalStatus: "Done", Lane: "done"}}); err != nil {
		t.Fatal(err)
	}
	m, err := s.LaneMap("jira", "acme/Platform")
	if err != nil || len(m) != 1 || m[0].ExternalStatus != "Done" || m[0].Provider != "jira" || m[0].Scope != "acme/Platform" {
		t.Fatalf("after replace: %+v %v", m, err)
	}
	if err := s.ReplaceLaneMap("jira", "acme/Platform", []LaneMapEntry{{ExternalStatus: "", Lane: "done"}}); err == nil {
		t.Fatal("empty status accepted")
	}
	if m, _ := s.LaneMap("jira", "acme/Platform"); len(m) != 1 {
		t.Fatal("a refused replace changed the map")
	}
	if err := s.ReplaceLaneMap("jira", "acme/Platform", []LaneMapEntry{{ExternalStatus: "Done", Lane: "nope"}}); err == nil {
		t.Fatal("unknown hub lane accepted")
	}
}

func TestSyncSettingsDefaultsAndValidation(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	got := s.SyncSettings(true)
	want := SyncSettings{Enabled: true, Interval: 300, Policy: SyncPolicy{
		MoveOnStart: PolicyAuto, MoveOnReview: PolicyAuto, CommentResults: PolicyAuto,
		CreateCard: PolicyAsk, EditText: PolicyAsk, CloseItem: PolicyAsk}}
	if got != want {
		t.Fatalf("defaults %+v", got)
	}
	if s.SyncSettings(false).Enabled {
		t.Fatal("enabled without a tracker block")
	}
	if err := s.SetSyncInterval(59); err == nil {
		t.Fatal("interval below 60 accepted")
	}
	if err := s.SetSyncInterval(60); err != nil || s.SyncInterval() != 60 {
		t.Fatal(err)
	}
	if err := s.SetSyncPolicy(OutboxCreateCard, "maybe"); err == nil {
		t.Fatal("bad policy value accepted")
	}
	if err := s.SetSyncPolicy("move_on_lunch", PolicyAuto); err == nil {
		t.Fatal("bad policy key accepted")
	}
	if err := s.SetSyncPolicy(OutboxCreateCard, PolicyOff); err != nil || s.SyncPolicyFor(OutboxCreateCard) != PolicyOff {
		t.Fatal(err)
	}
	if err := s.SetSyncEnabled(false); err != nil || s.SyncSettings(true).Enabled {
		t.Fatal("explicit off ignored")
	}
	// A hand-edited bad value falls back to the default.
	_ = s.SetSetting(SettingSyncInterval, "5")
	_ = s.SetSetting(SettingSyncPolicyPrefix+OutboxEditText, "yes")
	if s.SyncInterval() != DefaultSyncInterval || s.SyncPolicyFor(OutboxEditText) != PolicyAsk {
		t.Fatal("bad stored values not ignored")
	}
}

func TestSetSettingsWritesSyncKeysAtomically(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	iv, on := 120, false
	if err := s.SetSettings(SettingsWrite{SyncInterval: &iv, SyncEnabled: &on, SyncPolicy: map[string]string{OutboxCloseItem: PolicyAuto}},
		entryFor("tester")); err != nil {
		t.Fatal(err)
	}
	got := s.SyncSettings(true)
	if got.Interval != 120 || got.Enabled || got.Policy.CloseItem != PolicyAuto {
		t.Fatalf("%+v", got)
	}
	bad := 10
	if err := s.SetSettings(SettingsWrite{SyncInterval: &bad, SyncPolicy: map[string]string{OutboxEditText: PolicyAuto}}, entryFor("tester")); err == nil {
		t.Fatal("bad interval accepted")
	}
	if err := s.SetSettings(SettingsWrite{SyncPolicy: map[string]string{OutboxEditText: "sometimes"}}, entryFor("tester")); err == nil {
		t.Fatal("bad policy accepted")
	}
	if s.SyncPolicyFor(OutboxEditText) != PolicyAsk {
		t.Fatal("a refused write changed a policy")
	}
}
