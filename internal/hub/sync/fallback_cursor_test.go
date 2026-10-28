package sync

import (
	"context"
	"errors"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// switchRig is a rig whose credential chain starts with nothing (the MCP fallback) and can come
// back (the CLI login returns), with a clock the test moves.
type switchRig struct {
	*rig
	mu   gosync.Mutex
	live bool
	now  time.Time
	ad   Adapter // nil = the rig's fake
}

func newSwitchRig(t *testing.T) *switchRig {
	s := &switchRig{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	s.rig = newRig(t, func(c *EngineConfig) {
		c.Adapter = func(context.Context) (Adapter, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.live {
				return nil, cred.ErrNeedsSession
			}
			if s.ad != nil {
				return s.ad, nil
			}
			return s.rig.fake, nil
		}
		c.Now = func() time.Time {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.now
		}
	})
	return s
}

func (s *switchRig) setLive(v bool) { s.mu.Lock(); s.live = v; s.mu.Unlock() }
func (s *switchRig) advance(d time.Duration) {
	s.mu.Lock()
	s.now = s.now.Add(d)
	s.mu.Unlock()
}

// sinceTracker is the fake tracker with a ListChanged that honours since, as real trackers do.
type sinceTracker struct {
	*fakeTracker
	updated  map[string]time.Time
	gotSince []time.Time
}

func (f *sinceTracker) ListChanged(ctx context.Context, since time.Time) ([]Item, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotSince = append(f.gotSince, since)
	var out []Item
	newest := since
	for id, at := range f.updated {
		if at.After(since) {
			out = append(out, *f.items[id])
			if at.After(newest) {
				newest = at
			}
		}
	}
	return out, newest, nil
}

// An agent import never moves the engine's watermark: it keeps its own cursor (scope + "#mcp"),
// which sync status shows as fallback. The engine row is left as the engine wrote it.
func TestAgentImportKeepsItsOwnCursor(t *testing.T) {
	r := noSessionRig(t)
	if err := r.st.SetSyncState(store.SyncState{Provider: "github", Scope: r.eng.Scope(), Watermark: "2026-10-27T09:00:00Z",
		LastError: "needs-session", LastErrorAt: "2026-10-27T11:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Import(ImportInput{Items: []ImportItem{{ID: "1", Title: "t", StatusCategory: "todo"}},
		Watermark: "2026-10-27T12:00:00Z"}, true); err != nil {
		t.Fatal(err)
	}
	eng, _, _ := r.st.SyncState("github", r.eng.Scope())
	if eng.Watermark != "2026-10-27T09:00:00Z" || eng.LastOKAt != "" || eng.LastError != "needs-session" {
		t.Fatalf("an import rewrote the engine's sync state: %+v", eng)
	}
	fb, ok, _ := r.st.SyncState("github", r.eng.Scope()+FallbackScopeSuffix)
	if !ok || fb.Watermark != "2026-10-27T12:00:00Z" || fb.LastOKAt == "" {
		t.Fatalf("fallback cursor %+v (ok %v)", fb, ok)
	}
	ov, err := r.eng.Overview()
	if err != nil || ov.Fallback == nil || ov.Fallback.Watermark != fb.Watermark || len(ov.States) != 1 || ov.States[0].Watermark != eng.Watermark {
		t.Fatalf("overview %+v %v", ov, err)
	}
}

// The reviewer's case: PLAT-12 moves to In Review on the tracker; the agent imports it, the move
// is held, and the agent's watermark is past the item's update. When the CLI login comes back,
// the engine's poll (since its own watermark) still makes the move.
func TestHeldMoveIsMadeByTheEngineLater(t *testing.T) {
	s := newSwitchRig(t)
	st := &sinceTracker{fakeTracker: s.fake, updated: map[string]time.Time{}}
	s.ad = st
	// The engine polled at 11:45 before the login expired.
	if err := s.st.SetSyncState(store.SyncState{Provider: "github", Scope: s.eng.Scope(), Watermark: "2026-10-27T11:45:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "PLAT-12", Title: "Export", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	// 11:50: moved to In Review on the tracker. The agent imports at 12:00 with watermark 12:00.
	s.fake.items["PLAT-12"] = &Item{ID: "PLAT-12", Title: "Export", Status: "In Review", StatusCategory: "review", Rev: "2"}
	st.updated["PLAT-12"] = time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC)
	rep, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "PLAT-12", Title: "Export", Status: "In Review", StatusCategory: "review"}},
		Watermark: "2026-10-27T12:00:00Z"}, true)
	if err != nil || rep.Held != 1 {
		t.Fatalf("rep %+v err %v", rep, err)
	}
	if c := s.card("github", "PLAT-12"); c.Lane != "todo" || c.ExternalLane != "To Do" {
		t.Fatalf("an agent made the move: %+v", c)
	}
	s.setLive(true)
	s.tick()
	if len(st.gotSince) == 0 || !st.gotSince[0].Equal(time.Date(2026, 10, 27, 11, 45, 0, 0, time.UTC)) {
		t.Fatalf("the engine listed since %v, want its own 11:45 watermark", st.gotSince)
	}
	if c := s.card("github", "PLAT-12"); c.Lane != "review" || c.ExternalLane != "In Review" {
		t.Fatalf("the held move was never made: %+v", c)
	}
}

// A held move is queued as a deferred move, so the engine makes it even when its poll does not
// list the item again (the engine's own watermark already covers the update).
func TestHeldMoveIsQueuedAsDeferred(t *testing.T) {
	s := newSwitchRig(t)
	if _, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "31", Title: "Item", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "31", Title: "Item", Status: "In Progress", StatusCategory: "active"}}}, true); err != nil {
		t.Fatal(err)
	}
	c := s.card("github", "31")
	if _, ok := s.eng.deferred(c.ID); !ok {
		t.Fatal("the held move is not queued for the engine")
	}
	if !hasComment(t, s.st, c.ID, "In Progress") {
		t.Fatal("no comment tells the human the company board moved the item")
	}
	// A later agent import of the same state keeps it queued.
	if _, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "31", Title: "Item", Status: "In Progress", StatusCategory: "active"}}}, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.eng.deferred(c.ID); !ok {
		t.Fatal("a second agent import dropped the queued move")
	}
	s.setLive(true)
	s.tick() // the fake lists nothing new
	if got := s.card("github", "31"); got.Lane != "in progress" || got.ExternalLane != "In Progress" {
		t.Fatalf("the deferred move was not applied: %+v", got)
	}
}

// pending refuses while a background credential works: the engine sends those rows, and a
// listed row the agent also sent would reach the tracker twice.
func TestSendableRefusesWhenTheCredentialWorks(t *testing.T) {
	r := newRig(t)
	if _, _, err := r.eng.Sendable(context.Background()); !errors.Is(err, ErrCredentialWorks) {
		t.Fatalf("Sendable with a working credential: %v", err)
	}
}

// A row pending lists is claimed: the engine skips it until it is acked or the claim expires.
func TestSendableClaimsRowsFromTheEngine(t *testing.T) {
	s := newSwitchRig(t)
	if _, err := s.eng.Import(ImportInput{Items: []ImportItem{{ID: "41", Title: "Item", Status: "To Do", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	c := s.card("github", "41")
	s.fake.items["41"] = &Item{ID: "41", Status: "To Do"}
	a, err := s.st.EnqueueOutbox(c.ID, store.OutboxCommentResults, map[string]any{"text": "first"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.st.EnqueueOutbox(c.ID, store.OutboxCommentResults, map[string]any{"text": "second"})
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.eng.Sendable(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	// The login comes back before the agent's MCP write.
	s.setLive(true)
	s.flush()
	if len(s.fake.comments) != 0 {
		t.Fatalf("the engine sent rows the fallback claimed: %v", s.fake.comments)
	}
	if got, err := s.eng.Ack(a.ID, AckInput{Sent: true}, true); err != nil || got.Status != store.OutboxSent {
		t.Fatalf("%+v %v", got, err)
	}
	s.flush()
	if len(s.fake.comments) != 0 {
		t.Fatalf("an acked row was sent again, or b's claim was ignored: %v", s.fake.comments)
	}
	// b is never acked: after the claim expires the engine sends it.
	s.advance(claimTTL + time.Minute)
	s.flush()
	if len(s.fake.comments) != 1 || !strings.Contains(s.fake.comments[0], "second") {
		t.Fatalf("comments %v", s.fake.comments)
	}
	if got, _ := s.st.OutboxRow(b.ID); got.Status != store.OutboxSent {
		t.Fatalf("b %+v", got)
	}
}

// An ack --sent of a create_card row whose link fails leaves the row pending (with the reason), so
// a corrected ack can still link the card; an item id already linked to another card is refused.
func TestAckCreateLinkFailureKeepsRowPending(t *testing.T) {
	r := noSessionRig(t)
	if _, err := r.eng.Import(ImportInput{Items: []ImportItem{{ID: "77", Title: "Linked", StatusCategory: "todo"}}}, true); err != nil {
		t.Fatal(err)
	}
	local, err := r.st.AddCard(storeNewCard("Push me", "body"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	row, err := r.st.EnqueueOutbox(local.ID, store.OutboxCreateCard, map[string]any{"title": "Push me", "body": "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Ack(row.ID, AckInput{Sent: true, ItemID: "77"}, false); err == nil || !strings.Contains(err.Error(), "already linked") {
		t.Fatalf("an item id linked to another card was accepted: %v", err)
	}
	got, _ := r.st.OutboxRow(row.ID)
	if got.Status != store.OutboxPending || !strings.Contains(got.LastError, "77") {
		t.Fatalf("row after a failed link: %+v", got)
	}
	if c, _ := r.st.Card(local.ID); c.ExternalID != "" {
		t.Fatalf("card linked anyway: %+v", c)
	}
	if got, err := r.eng.Ack(row.ID, AckInput{Sent: true, ItemID: "78"}, false); err != nil || got.Status != store.OutboxSent {
		t.Fatalf("corrected ack: %+v %v", got, err)
	}
	if c := r.card("github", "78"); c.ID != local.ID {
		t.Fatalf("card %+v", c)
	}
}
