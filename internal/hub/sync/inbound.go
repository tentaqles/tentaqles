package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// Lane mapping (spec §5): the manifest's tracker.lanes override (a lane name or a lane kind),
// else the status category's first lane. The map is rebuilt from the tracker's statuses on the
// first pass of each engine.

// resolveLane turns a manifest lane value (a lane name, else a lane kind) into a lane name.
func (e *Engine) resolveLane(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	if l, err := e.st.Lane(v); err == nil {
		return l.Name, true
	}
	if slices.Contains(store.LaneKinds, strings.ToLower(v)) {
		if l, err := e.st.FirstLane(strings.ToLower(v)); err == nil {
			return l.Name, true
		}
	}
	return "", false
}

func (e *Engine) overrideLane(status string) (string, bool) {
	keys := make([]string, 0, len(e.cfg.Tracker.Lanes))
	for k := range e.cfg.Tracker.Lanes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(status)) {
			return e.resolveLane(e.cfg.Tracker.Lanes[k])
		}
	}
	return "", false
}

func (e *Engine) categoryLane(category string) (string, bool) {
	if !ValidCategory(category) {
		return "", false
	}
	l, err := e.st.FirstLane(category) // the categories are the lane kinds
	if err != nil {
		return "", false
	}
	return l.Name, true
}

// laneOf is the hub lane for a tracker status ("" when nothing maps it).
func (e *Engine) laneOf(status, category string) string {
	if strings.TrimSpace(status) != "" {
		if l, ok, err := e.st.LaneFor(e.provider, e.scope, status); err == nil && ok {
			return l
		}
		if l, ok := e.overrideLane(status); ok {
			return l
		}
	}
	if l, ok := e.categoryLane(category); ok {
		return l
	}
	return ""
}

func (e *Engine) buildLaneMap(ctx context.Context, ad Adapter, rep *Report) error {
	sts, err := retry(ctx, e, rep, func() ([]Status, error) { return ad.Statuses(ctx) })
	if err != nil {
		return fmt.Errorf("statuses: %w", err)
	}
	var entries []store.LaneMapEntry
	for _, s := range sts {
		lane, ok := e.overrideLane(s.Name)
		if !ok {
			lane, ok = e.categoryLane(s.Category)
		}
		if ok && strings.TrimSpace(s.Name) != "" {
			entries = append(entries, store.LaneMapEntry{ExternalStatus: s.Name, Lane: lane})
		}
	}
	if err := e.st.ReplaceLaneMap(e.provider, e.scope, entries); err != nil {
		return err
	}
	e.statuses, e.mapped = sts, true
	return nil
}

// statusFor is the tracker status an outbound move to lane uses, in the tracker's order: the first
// status of the lane's own category that maps to the lane, else any status that maps to it (a
// manifest override), else the first of the lane's category.
func (e *Engine) statusFor(lane store.Lane) (string, bool) {
	maps := func(s Status) bool {
		l := e.laneOf(s.Name, s.Category)
		return l != "" && strings.EqualFold(l, lane.Name)
	}
	for _, s := range e.statuses {
		if s.Category == lane.Kind && maps(s) {
			return s.Name, true
		}
	}
	for _, s := range e.statuses {
		if maps(s) {
			return s.Name, true
		}
	}
	for _, s := range e.statuses {
		if s.Category == lane.Kind {
			return s.Name, true
		}
	}
	return "", false
}

func parseWatermark(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}

// pull lists the items changed since the watermark and mirrors each into its linked card.
func (e *Engine) pull(ctx context.Context, adapter func() (Adapter, error), rep *Report) error {
	ad, err := adapter()
	if err != nil {
		return err
	}
	if !e.mapped {
		if err := e.buildLaneMap(ctx, ad, rep); err != nil {
			return err
		}
	}
	state, _, err := e.st.SyncState(e.provider, e.scope)
	if err != nil {
		return err
	}
	type listed struct {
		items []Item
		wm    time.Time
	}
	got, err := retry(ctx, e, rep, func() (listed, error) {
		items, wm, err := ad.ListChanged(ctx, parseWatermark(state.Watermark))
		return listed{items, wm}, err
	})
	if err != nil {
		return fmt.Errorf("list changed: %w", err)
	}
	rep.Pulled = len(got.items)
	var errs []error
	// Moves that waited for a run first: an item in this batch is newer and supersedes them.
	if err := e.applyDeferred(rep); err != nil {
		errs = append(errs, err)
	}
	for _, it := range parentsFirst(got.items) {
		if err := e.applyItem(it, rep); err != nil {
			errs = append(errs, fmt.Errorf("item %s: %w", it.ID, err))
		}
	}
	wm := ""
	if !got.wm.IsZero() {
		wm = got.wm.UTC().Format(time.RFC3339Nano)
		rep.Watermark = wm
	}
	if len(errs) > 0 {
		// The watermark stays: the next pass lists these items again.
		return errors.Join(errs...)
	}
	return e.st.RecordSyncOK(e.provider, e.scope, wm)
}

// parentsFirst orders items so a parent in the batch is imported before its children.
func parentsFirst(items []Item) []Item {
	in := map[string]bool{}
	for _, it := range items {
		in[it.ID] = true
	}
	done := map[string]bool{}
	out := make([]Item, 0, len(items))
	for len(out) < len(items) {
		progress := false
		for _, it := range items {
			if done[it.ID] {
				continue
			}
			if it.ParentID == "" || !in[it.ParentID] || done[it.ParentID] || it.ParentID == it.ID {
				out = append(out, it)
				done[it.ID] = true
				progress = true
			}
		}
		if !progress { // a cycle: take the rest as they come
			for _, it := range items {
				if !done[it.ID] {
					out = append(out, it)
					done[it.ID] = true
				}
			}
		}
	}
	return out
}

// itemBody is the card body for an item: its description plus the acceptance criteria.
func itemBody(it Item) string {
	body := strings.TrimSpace(it.Body)
	if a := strings.TrimSpace(it.Acceptance); a != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "## Acceptance\n\n" + a
	}
	return body
}

// localTextEdited: someone other than sync changed the card's title or body since sync last
// wrote them (its card.create, or a card.update that carried the text). The anchor is that write,
// not external_synced_at: every pull stamps the latter, also the pulls that kept a local edit.
func (e *Engine) localTextEdited(c store.Card) bool {
	var anchor int64
	if err := e.st.DB.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM activity WHERE card_id = ? AND actor = ? AND (verb = ?
		OR (verb = ? AND (data LIKE '%"title"%' OR data LIKE '%"body"%')))`, c.ID, ActorSync, activity.VerbCardCreate,
		activity.VerbCardUpdate).Scan(&anchor); err != nil {
		return true // unsure: keep the local copy
	}
	var n int
	err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM activity WHERE card_id = ? AND verb = ? AND actor != ? AND rowid > ?
		AND (data LIKE '%"title"%' OR data LIKE '%"body"%')`, c.ID, activity.VerbCardUpdate, ActorSync, anchor).Scan(&n)
	return err != nil || n > 0
}

// localMoveSinceSync: the card moved here since sync last saw the item (a move by anyone but sync
// after external_synced_at, or an outbound move still waiting in the outbox). A move a pull has
// already seen past is not a conflict any more.
func (e *Engine) localMoveSinceSync(c store.Card) bool {
	var n int
	if err := e.st.DB.QueryRow(`SELECT COUNT(*) FROM activity WHERE card_id = ? AND verb = ? AND actor != ? AND ts > ?`,
		c.ID, activity.VerbCardMove, ActorSync, c.ExternalSyncedAt).Scan(&n); err != nil || n > 0 {
		return true
	}
	rows, err := e.st.ListOutbox(store.OutboxFilter{CardID: c.ID, Status: store.OutboxPending})
	if err != nil {
		return true
	}
	for _, o := range rows {
		if isMove(o.Action) {
			return true
		}
	}
	return false
}

func isMove(action string) bool {
	return action == store.OutboxMoveOnStart || action == store.OutboxMoveOnReview || action == store.OutboxCloseItem
}

// ---- inbound moves deferred while a run holds the card ----

const settingDeferred = "sync.deferred." // + card id; the value is a deferredMove

type deferredMove struct {
	Status   string `json:"status"`
	Category string `json:"category,omitempty"`
	Rev      string `json:"rev,omitempty"`
}

func (e *Engine) deferred(cardID string) (deferredMove, bool) {
	v, ok, err := e.st.Setting(settingDeferred + cardID)
	if err != nil || !ok {
		return deferredMove{}, false
	}
	var d deferredMove
	if json.Unmarshal([]byte(v), &d) != nil || d.Status == "" {
		return deferredMove{}, false
	}
	return d, true
}

func (e *Engine) setDeferred(cardID string, d deferredMove) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return e.st.SetSetting(settingDeferred+cardID, string(b))
}

func (e *Engine) clearDeferred(cardID string) {
	_, _ = e.st.DB.Exec(`DELETE FROM settings WHERE key = ?`, settingDeferred+cardID)
}

// applyDeferred applies the remote moves that waited for a run to release its card.
func (e *Engine) applyDeferred(rep *Report) error {
	rows, err := e.st.DB.Query(`SELECT key FROM settings WHERE key LIKE ? ORDER BY key`, settingDeferred+"%")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, strings.TrimPrefix(k, settingDeferred))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		d, ok := e.deferred(id)
		c, err := e.st.Card(id)
		if !ok || errors.Is(err, store.ErrNotFound) || (err == nil && (!linked(c) || c.ExternalProvider != e.provider)) {
			e.clearDeferred(id)
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if c.LockedByRun != "" {
			continue
		}
		if err := e.applyItem(Item{ID: c.ExternalID, Status: d.Status, StatusCategory: d.Category, Rev: d.Rev}, rep); err != nil {
			errs = append(errs, fmt.Errorf("deferred move of %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) applyItem(it Item, rep *Report) error {
	if strings.TrimSpace(it.ID) == "" {
		return errors.New("the tracker returned an item without an id")
	}
	lane := e.laneOf(it.Status, it.StatusCategory)
	link := store.ExternalLink{Provider: e.provider, ID: it.ID, URL: it.URL, Lane: it.Status, Rev: it.Rev}
	title, body := strings.TrimSpace(it.Title), itemBody(it)
	if title == "" {
		title = strings.TrimSpace(it.Key)
	}
	cur, err := e.st.CardByExternal(e.provider, it.ID)
	if errors.Is(err, store.ErrNotFound) {
		in := store.LinkedCard{Link: link, Title: title, Body: body, Lane: lane}
		if it.ParentID != "" {
			if p, perr := e.st.CardByExternal(e.provider, it.ParentID); perr == nil {
				in.ParentID = p.ID
			}
		}
		if _, created, err := e.st.UpsertLinkedCard(in, ActorSync); err != nil {
			return err
		} else if created {
			rep.Created++
		} else {
			rep.Updated++
		}
		return nil
	}
	if err != nil {
		return err
	}
	// A fresh state of the item supersedes a move that waited for a run.
	prev, hadDeferred := e.deferred(cur.ID)
	if hadDeferred {
		e.clearDeferred(cur.ID)
	}
	remoteMoved := strings.TrimSpace(it.Status) != "" && !strings.EqualFold(it.Status, cur.ExternalLane)
	needsMove := remoteMoved && lane != "" && !strings.EqualFold(lane, cur.Lane)
	defer_ := needsMove && cur.LockedByRun != ""
	in := store.LinkedCard{Link: link, Title: title, Body: body}
	if defer_ {
		// The card keeps the external lane and rev it had, so an outbound move made meanwhile (an
		// approval) is checked against the state before this remote move and skipped.
		in.Link.Lane, in.Link.Rev = "", ""
	}
	if title != "" && (e.st.Mask(title) != cur.Title || e.st.Mask(body) != cur.Body) && !e.localTextEdited(cur) {
		in.UpdateText = true
	}
	if _, _, err := e.st.UpsertLinkedCard(in, ActorSync); err != nil {
		return err
	}
	rep.Updated++
	if !needsMove {
		return nil
	}
	if defer_ {
		d := deferredMove{Status: it.Status, Category: it.StatusCategory, Rev: it.Rev}
		if err := e.setDeferred(cur.ID, d); err != nil {
			return err
		}
		if !hadDeferred || !strings.EqualFold(prev.Status, d.Status) {
			_, _ = e.st.AddComment(cur.ID, store.AuthorSystem, ActorSync, fmt.Sprintf(
				"The company board moved this item to %q while run %s holds the card; sync moves it when the run ends.",
				it.Status, cur.LockedByRun), "")
		}
		return nil
	}
	localMoved := false
	if cur.ExternalLane != "" {
		if was := e.laneOf(cur.ExternalLane, ""); was != "" && !strings.EqualFold(was, cur.Lane) {
			localMoved = e.localMoveSinceSync(cur)
		}
	}
	if localMoved {
		rep.Conflicts++
		_, _ = e.st.AddComment(cur.ID, store.AuthorSystem, ActorSync, fmt.Sprintf(
			"Lane conflict: this card moved to %s here and the company board moved the item to %q since the last sync. The company board wins: moved to %s.",
			cur.Lane, it.Status, lane), "")
	}
	if _, err := e.st.MoveCard(cur.ID, lane, ActorSync); err != nil {
		return err
	}
	rep.Moved++
	return nil
}
