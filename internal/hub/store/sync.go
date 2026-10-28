package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

// Board sync storage (spec B §5): the external link on cards, the per-provider watermark, the
// outbound outbox and the lane map. The engine (internal/hub/sync) owns the logic; these are the
// rows it reads and writes.

var (
	// ErrAlreadyLinked means another card already mirrors that tracker item, or the card is linked
	// to a different item.
	ErrAlreadyLinked = errors.New("already linked")
	// ErrOutboxFinal means the outbox row was already sent or skipped; it never changes again.
	ErrOutboxFinal = errors.New("outbox row is final")
)

// ExternalLink names one tracker item. Provider and ID identify it; URL, Lane (the tracker's
// status name) and Rev (its version, etag or updated-at) describe the last state sync saw.
type ExternalLink struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url,omitempty"`
	Lane     string `json:"lane,omitempty"`
	Rev      string `json:"rev,omitempty"`
}

func cleanRef(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%s: control characters are not allowed", field)
		}
	}
	if len(v) > max {
		return "", fmt.Errorf("%s: longer than %d bytes", field, max)
	}
	return v, nil
}

func (l ExternalLink) clean() (ExternalLink, error) {
	var err error
	out := l
	for _, f := range []struct {
		name string
		p    *string
		max  int
	}{{"provider", &out.Provider, 64}, {"external id", &out.ID, 512}, {"external url", &out.URL, 2048},
		{"external lane", &out.Lane, 256}, {"external rev", &out.Rev, 256}} {
		if *f.p, err = cleanRef(f.name, *f.p, f.max); err != nil {
			return l, err
		}
	}
	if out.Provider == "" || out.ID == "" {
		return l, errors.New("an external link needs a provider and an id")
	}
	return out, nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CardByExternal is the card linked to a tracker item, or ErrNotFound.
func (s *Store) CardByExternal(provider, externalID string) (Card, error) {
	c, err := scanCard(s.DB.QueryRow(`SELECT `+cardCols+` FROM cards c WHERE c.external_provider = ? AND c.external_id = ?`,
		strings.TrimSpace(provider), strings.TrimSpace(externalID)))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("card for %s %s: %w", provider, externalID, ErrNotFound)
	}
	return c, err
}

// LinkedCards lists the cards linked to provider ("" = any provider), oldest first.
func (s *Store) LinkedCards(provider string) ([]Card, error) {
	q := `SELECT ` + cardCols + ` FROM cards c WHERE c.external_provider IS NOT NULL AND c.external_id IS NOT NULL`
	var args []any
	if provider != "" {
		q += ` AND c.external_provider = ?`
		args = append(args, provider)
	}
	return s.cards(q+` ORDER BY c.created_at, c.id`, args...)
}

// LinkCard links an existing local card to a tracker item (after `tq hub push` created it there).
// Relinking the same item updates URL, lane and rev; a card linked elsewhere, or an item another
// card mirrors, is ErrAlreadyLinked.
func (s *Store) LinkCard(cardID string, l ExternalLink) error {
	l, err := l.clean()
	if err != nil {
		return err
	}
	c, err := s.Card(cardID)
	if err != nil {
		return err
	}
	if c.ExternalProvider != "" && (c.ExternalProvider != l.Provider || c.ExternalID != l.ID) {
		return fmt.Errorf("card %s is linked to %s %s: %w", cardID, c.ExternalProvider, c.ExternalID, ErrAlreadyLinked)
	}
	now := Now()
	_, err = s.DB.Exec(`UPDATE cards SET external_provider = ?, external_id = ?, external_url = ?, external_lane = ?, external_rev = ?,
		external_synced_at = ?, updated_at = ? WHERE id = ?`, l.Provider, l.ID, nullStr(l.URL), nullStr(l.Lane), nullStr(l.Rev), now, now, cardID)
	if isUnique(err) {
		return fmt.Errorf("%s %s is mirrored by another card: %w", l.Provider, l.ID, ErrAlreadyLinked)
	}
	if err != nil {
		return err
	}
	s.Publish(Change{Kind: "external", CardID: cardID})
	return nil
}

// MarkSynced records what sync last saw for a linked card: the tracker's status and revision.
func (s *Store) MarkSynced(cardID, externalLane, rev string) error {
	lane, err := cleanRef("external lane", externalLane, 256)
	if err != nil {
		return err
	}
	if rev, err = cleanRef("external rev", rev, 256); err != nil {
		return err
	}
	now := Now()
	res, err := s.DB.Exec(`UPDATE cards SET external_lane = ?, external_rev = ?, external_synced_at = ?, updated_at = ?
		WHERE id = ? AND external_provider IS NOT NULL AND external_id IS NOT NULL`, nullStr(lane), nullStr(rev), now, now, cardID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("linked card %s: %w", cardID, ErrNotFound)
	}
	s.Publish(Change{Kind: "external", CardID: cardID})
	return nil
}

// LinkedCard is one tracker item as sync imports it. On create every field is used; on an
// existing link only the external columns change (an empty Link.URL, Lane or Rev keeps the
// stored one), plus title and body when UpdateText is set
// (the engine sets it only when the local copy is unchanged since the last sync, spec §11).
// Lane moves never happen here: they go through MoveCard with the actor "sync".
type LinkedCard struct {
	Link        ExternalLink
	Title, Body string
	Kind        string   // on create; "" = feature
	Lane        string   // on create; "" = the first backlog lane
	ParentID    string   // on create
	ProjectDir  string   // on create
	Tags        []string // on create
	UpdateText  bool
}

// UpsertLinkedCard creates the card mirroring a tracker item, or updates the one that already
// does. Inbound text is masked like every stored text. It reports whether a card was created.
func (s *Store) UpsertLinkedCard(in LinkedCard, actor string) (Card, bool, error) {
	l, err := in.Link.clean()
	if err != nil {
		return Card{}, false, err
	}
	title := strings.TrimSpace(in.Title)
	existing, err := s.CardByExternal(l.Provider, l.ID)
	switch {
	case err == nil:
		return s.updateLinked(existing, l, in, title, actor)
	case !errors.Is(err, ErrNotFound):
		return Card{}, false, err
	}
	if title == "" {
		return Card{}, false, errors.New("a card needs a title")
	}
	kind := in.Kind
	if kind == "" {
		kind = "feature"
	}
	if !slices.Contains(CardKinds, kind) {
		return Card{}, false, fmt.Errorf("kind %q: use one of %s", kind, strings.Join(CardKinds, ", "))
	}
	var lane Lane
	if in.Lane == "" {
		lane, err = s.FirstLane(LaneBacklog)
	} else {
		lane, err = s.Lane(in.Lane)
	}
	if err != nil {
		return Card{}, false, err
	}
	if in.ParentID != "" {
		if _, err := s.Card(in.ParentID); err != nil {
			return Card{}, false, fmt.Errorf("parent %s: %w", in.ParentID, err)
		}
	}
	pd, err := cleanProjectDir(in.ProjectDir)
	if err != nil {
		return Card{}, false, err
	}
	tags, _ := json.Marshal(dedupe(s.maskTags(in.Tags)))
	now := Now()
	id := NewID("c")
	_, err = s.DB.Exec(`INSERT INTO cards(id, title, body, lane, kind, tags, parent_id, project_dir,
		external_provider, external_id, external_url, external_lane, external_rev, external_synced_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.Mask(title), s.Mask(in.Body), lane.Name, kind, string(tags), nullStr(in.ParentID), pd,
		l.Provider, l.ID, nullStr(l.URL), nullStr(l.Lane), nullStr(l.Rev), now, now, now)
	if isUnique(err) { // another process imported it meanwhile: update that card instead
		if existing, gerr := s.CardByExternal(l.Provider, l.ID); gerr == nil {
			return s.updateLinked(existing, l, in, title, actor)
		}
	}
	if err != nil {
		return Card{}, false, err
	}
	_ = s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardCreate, CardID: id,
		Data: map[string]any{"lane": lane.Name, "kind": kind, "external_provider": l.Provider}})
	s.Publish(Change{Kind: "card", CardID: id})
	c, err := s.Card(id)
	return c, err == nil, err
}

func (s *Store) updateLinked(c Card, l ExternalLink, in LinkedCard, title, actor string) (Card, bool, error) {
	// An empty URL, lane or rev means the import did not carry it (a partial item from
	// ListChanged): the stored value stays.
	q := `UPDATE cards SET external_url = COALESCE(?, external_url), external_lane = COALESCE(?, external_lane),
		external_rev = COALESCE(?, external_rev), external_synced_at = ?, updated_at = ?`
	now := Now()
	args := []any{nullStr(l.URL), nullStr(l.Lane), nullStr(l.Rev), now, now}
	fields := []any{"external"}
	if in.UpdateText {
		if title == "" {
			return c, false, errors.New("a card needs a title")
		}
		q += `, title = ?, body = ?`
		args = append(args, s.Mask(title), s.Mask(in.Body))
		fields = append(fields, "title", "body")
	}
	args = append(args, c.ID)
	if _, err := s.DB.Exec(q+` WHERE id = ?`, args...); err != nil {
		return c, false, err
	}
	_ = s.Log(activity.Entry{Actor: actor, Verb: activity.VerbCardUpdate, CardID: c.ID, Data: map[string]any{"fields": fields}})
	s.Publish(Change{Kind: "card", CardID: c.ID})
	got, err := s.Card(c.ID)
	return got, false, err
}

// SyncState is the engine's progress for one provider and scope in this workspace.
type SyncState struct {
	Workspace   string `json:"workspace"`
	Provider    string `json:"provider"`
	Scope       string `json:"scope"`
	Watermark   string `json:"watermark"`
	LastOKAt    string `json:"last_ok_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	LastErrorAt string `json:"last_error_at,omitempty"`
}

const syncStateCols = `workspace, provider, scope, watermark, COALESCE(last_ok_at, ''), COALESCE(last_error, ''), COALESCE(last_error_at, '')`

func scanSyncState(r scanner) (SyncState, error) {
	var st SyncState
	err := r.Scan(&st.Workspace, &st.Provider, &st.Scope, &st.Watermark, &st.LastOKAt, &st.LastError, &st.LastErrorAt)
	return st, err
}

// SyncState reads one provider/scope's state; ok is false when sync never ran for it.
func (s *Store) SyncState(provider, scope string) (SyncState, bool, error) {
	st, err := scanSyncState(s.DB.QueryRow(`SELECT `+syncStateCols+` FROM sync_state WHERE workspace = ? AND provider = ? AND scope = ?`,
		s.Workspace, provider, scope))
	if errors.Is(err, sql.ErrNoRows) {
		return SyncState{Workspace: s.Workspace, Provider: provider, Scope: scope}, false, nil
	}
	return st, err == nil, err
}

// SyncStates lists every provider/scope of this workspace, by provider then scope.
func (s *Store) SyncStates() ([]SyncState, error) {
	rows, err := s.DB.Query(`SELECT `+syncStateCols+` FROM sync_state WHERE workspace = ? ORDER BY provider, scope`, s.Workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncState
	for rows.Next() {
		st, err := scanSyncState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetSyncState writes the whole row (the workspace is always this store's). LastError is masked.
func (s *Store) SetSyncState(st SyncState) error {
	if strings.TrimSpace(st.Provider) == "" {
		return errors.New("sync state needs a provider")
	}
	_, err := s.DB.Exec(`INSERT INTO sync_state(workspace, provider, scope, watermark, last_ok_at, last_error, last_error_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(workspace, provider, scope) DO UPDATE SET watermark = excluded.watermark,
		last_ok_at = excluded.last_ok_at, last_error = excluded.last_error, last_error_at = excluded.last_error_at`,
		s.Workspace, st.Provider, st.Scope, st.Watermark, nullStr(st.LastOKAt), nullStr(s.Mask(st.LastError)), nullStr(st.LastErrorAt))
	if err == nil {
		s.Publish(Change{Kind: "sync"})
	}
	return err
}

// RecordSyncOK stores a successful poll: the new watermark ("" keeps the old one), the time, and
// clears the last error.
func (s *Store) RecordSyncOK(provider, scope, watermark string) error {
	st, _, err := s.SyncState(provider, scope)
	if err != nil {
		return err
	}
	if watermark != "" {
		st.Watermark = watermark
	}
	st.LastOKAt, st.LastError, st.LastErrorAt = Now(), "", ""
	return s.SetSyncState(st)
}

// RecordSyncError stores a failed poll's message (masked; never a token) and keeps the watermark.
func (s *Store) RecordSyncError(provider, scope, msg string) error {
	st, _, err := s.SyncState(provider, scope)
	if err != nil {
		return err
	}
	st.LastError, st.LastErrorAt = strings.TrimSpace(msg), Now()
	if st.LastError == "" {
		st.LastError = "error"
	}
	return s.SetSyncState(st)
}

// Outbox actions are the six write-policy keys (spec §5): each row is sent, asked about or dropped
// under the policy of the same name.
const (
	OutboxMoveOnStart    = "move_on_start"
	OutboxMoveOnReview   = "move_on_review"
	OutboxCommentResults = "comment_results"
	OutboxCreateCard     = "create_card"
	OutboxEditText       = "edit_text"
	OutboxCloseItem      = "close_item"
)

// OutboxActions lists every action, in settings order.
var OutboxActions = []string{OutboxMoveOnStart, OutboxMoveOnReview, OutboxCommentResults, OutboxCreateCard, OutboxEditText, OutboxCloseItem}

const (
	OutboxPending = "pending"
	OutboxSent    = "sent"
	OutboxSkipped = "skipped"
	OutboxFailed  = "failed"
)

var OutboxStatuses = []string{OutboxPending, OutboxSent, OutboxSkipped, OutboxFailed}

// OutboxRow is one outbound write waiting for (or done with) the tracker.
type OutboxRow struct {
	ID         string          `json:"id"`
	CardID     string          `json:"card_id"`
	Action     string          `json:"action"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"`
	DecisionID string          `json:"decision_id,omitempty"`
	Attempts   int             `json:"attempts"`
	LastError  string          `json:"last_error,omitempty"`
	CreatedAt  string          `json:"created_at"`
	SentAt     string          `json:"sent_at,omitempty"`
}

type OutboxFilter struct {
	Status string `json:"status,omitempty"`
	CardID string `json:"card_id,omitempty"`
	Action string `json:"action,omitempty"`
	Limit  int    `json:"limit,omitempty"` // 0 = no limit
}

const outboxCols = `id, card_id, action, payload, status, COALESCE(decision_id, ''), attempts, COALESCE(last_error, ''), created_at, COALESCE(sent_at, '')`

func scanOutbox(r scanner) (OutboxRow, error) {
	var o OutboxRow
	var payload string
	err := r.Scan(&o.ID, &o.CardID, &o.Action, &payload, &o.Status, &o.DecisionID, &o.Attempts, &o.LastError, &o.CreatedAt, &o.SentAt)
	o.Payload = json.RawMessage(payload)
	return o, err
}

// EnqueueOutbox queues a write for a card. payload is any JSON-marshalable value (nil = {});
// every string in it is masked before it is stored. Outbound redaction with the exit classes still
// happens when the engine sends it.
func (s *Store) EnqueueOutbox(cardID, action string, payload any) (OutboxRow, error) {
	if !slices.Contains(OutboxActions, action) {
		return OutboxRow{}, fmt.Errorf("outbox action %q: use one of %s", action, strings.Join(OutboxActions, ", "))
	}
	if _, err := s.Card(cardID); err != nil {
		return OutboxRow{}, err
	}
	raw := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return OutboxRow{}, fmt.Errorf("outbox payload: %w", err)
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return OutboxRow{}, err
		}
		if raw, err = json.Marshal(s.maskAny(v)); err != nil {
			return OutboxRow{}, err
		}
	}
	o := OutboxRow{ID: NewID("o"), CardID: cardID, Action: action, Payload: raw, Status: OutboxPending, CreatedAt: Now()}
	if _, err := s.DB.Exec(`INSERT INTO sync_outbox(id, card_id, action, payload, status, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		o.ID, o.CardID, o.Action, string(o.Payload), o.Status, o.CreatedAt); err != nil {
		return OutboxRow{}, err
	}
	s.Publish(Change{Kind: "outbox", CardID: cardID})
	return o, nil
}

func (s *Store) OutboxRow(id string) (OutboxRow, error) {
	o, err := scanOutbox(s.DB.QueryRow(`SELECT `+outboxCols+` FROM sync_outbox WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("outbox row %s: %w", id, ErrNotFound)
	}
	return o, err
}

// OutboxByDecision is the row an `ask` decision was created for.
func (s *Store) OutboxByDecision(decisionID string) (OutboxRow, error) {
	o, err := scanOutbox(s.DB.QueryRow(`SELECT `+outboxCols+` FROM sync_outbox WHERE decision_id = ? ORDER BY created_at LIMIT 1`, decisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("outbox row for decision %s: %w", decisionID, ErrNotFound)
	}
	return o, err
}

// ListOutbox lists rows oldest first.
func (s *Store) ListOutbox(f OutboxFilter) ([]OutboxRow, error) {
	q := `SELECT ` + outboxCols + ` FROM sync_outbox WHERE 1 = 1`
	var args []any
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.CardID != "" {
		q += ` AND card_id = ?`
		args = append(args, f.CardID)
	}
	if f.Action != "" {
		q += ` AND action = ?`
		args = append(args, f.Action)
	}
	q += ` ORDER BY created_at, rowid`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutboxRow
	for rows.Next() {
		o, err := scanOutbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetOutboxDecision records the `ask` decision created for a row.
func (s *Store) SetOutboxDecision(id, decisionID string) error {
	res, err := s.DB.Exec(`UPDATE sync_outbox SET decision_id = ? WHERE id = ?`, nullStr(decisionID), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("outbox row %s: %w", id, ErrNotFound)
	}
	return nil
}

// OutboxAttempt counts a failed try that will be retried (a 429, a timeout): the row stays as it is.
func (s *Store) OutboxAttempt(id, lastError string) error {
	res, err := s.DB.Exec(`UPDATE sync_outbox SET attempts = attempts + 1, last_error = ? WHERE id = ? AND status IN ('pending','failed')`,
		nullStr(s.Mask(strings.TrimSpace(lastError))), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.OutboxRow(id); err != nil {
			return err
		}
		return fmt.Errorf("outbox row %s: %w", id, ErrOutboxFinal)
	}
	return nil
}

// MarkOutbox moves a row to status. sent and skipped are final; failed may go back to pending (a
// retry). sent and failed count an attempt; sent stamps sent_at. lastError ("" keeps the old one)
// is masked.
func (s *Store) MarkOutbox(id, status, lastError string) (OutboxRow, error) {
	if !slices.Contains(OutboxStatuses, status) {
		return OutboxRow{}, fmt.Errorf("outbox status %q: use one of %s", status, strings.Join(OutboxStatuses, ", "))
	}
	cur, err := s.OutboxRow(id)
	if err != nil {
		return cur, err
	}
	if cur.Status == OutboxSent || cur.Status == OutboxSkipped {
		return cur, fmt.Errorf("outbox row %s is %s: %w", id, cur.Status, ErrOutboxFinal)
	}
	attempt := 0
	if status == OutboxSent || status == OutboxFailed {
		attempt = 1
	}
	var sentAt any
	if status == OutboxSent {
		sentAt = Now()
	}
	res, err := s.DB.Exec(`UPDATE sync_outbox SET status = ?, attempts = attempts + ?, sent_at = ?,
		last_error = COALESCE(?, last_error) WHERE id = ? AND status NOT IN ('sent','skipped')`,
		status, attempt, sentAt, nullStr(s.Mask(strings.TrimSpace(lastError))), id)
	if err != nil {
		return cur, err
	}
	if n, _ := res.RowsAffected(); n == 0 { // another process finished it meanwhile
		return cur, fmt.Errorf("outbox row %s: %w", id, ErrOutboxFinal)
	}
	s.Publish(Change{Kind: "outbox", CardID: cur.CardID})
	return s.OutboxRow(id)
}

// LaneMapEntry maps one tracker status to a hub lane for a provider and scope.
type LaneMapEntry struct {
	Provider       string `json:"provider"`
	Scope          string `json:"scope"`
	ExternalStatus string `json:"external_status"`
	Lane           string `json:"lane"`
}

// ReplaceLaneMap swaps the whole map of provider/scope in one transaction (it is rebuilt from the
// manifest when sync starts, never edited by hand). Every lane must exist; a refused map changes
// nothing. Statuses match case-insensitively; a repeated status keeps the last entry.
func (s *Store) ReplaceLaneMap(provider, scope string, entries []LaneMapEntry) error {
	if strings.TrimSpace(provider) == "" {
		return errors.New("lane map needs a provider")
	}
	clean := make([]LaneMapEntry, 0, len(entries))
	for _, e := range entries {
		st, err := cleanRef("external status", e.ExternalStatus, 256)
		if err != nil {
			return err
		}
		if st == "" {
			return errors.New("lane map: an external status is empty")
		}
		l, err := s.Lane(e.Lane)
		if err != nil {
			return fmt.Errorf("lane map %q: %w", st, err)
		}
		clean = append(clean, LaneMapEntry{Provider: provider, Scope: scope, ExternalStatus: st, Lane: l.Name})
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM lane_map WHERE provider = ? AND scope = ?`, provider, scope); err != nil {
		tx.Rollback()
		return err
	}
	for _, e := range clean {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO lane_map(provider, scope, external_status, lane) VALUES (?, ?, ?, ?)`,
			e.Provider, e.Scope, e.ExternalStatus, e.Lane); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LaneMap lists the map of provider/scope by status.
func (s *Store) LaneMap(provider, scope string) ([]LaneMapEntry, error) {
	rows, err := s.DB.Query(`SELECT provider, scope, external_status, lane FROM lane_map WHERE provider = ? AND scope = ?
		ORDER BY external_status`, provider, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LaneMapEntry
	for rows.Next() {
		var e LaneMapEntry
		if err := rows.Scan(&e.Provider, &e.Scope, &e.ExternalStatus, &e.Lane); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LaneFor is the hub lane a tracker status maps to; ok is false when the map has no entry.
func (s *Store) LaneFor(provider, scope, externalStatus string) (string, bool, error) {
	var lane string
	err := s.DB.QueryRow(`SELECT lane FROM lane_map WHERE provider = ? AND scope = ? AND external_status = ?`,
		provider, scope, strings.TrimSpace(externalStatus)).Scan(&lane)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return lane, err == nil, err
}
