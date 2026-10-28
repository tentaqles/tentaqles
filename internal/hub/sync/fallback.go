package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// The MCP fallback (spec B §7 source 4, §8, §9, §12). When no background credential works, the
// /tentaqles:sync skill reads the tracker with the agent's MCP connector and hands the items to
// Import (tq hub sync import), then asks Sendable (tq hub sync pending) which outbox
// rows it may deliver, sends each one with the connector, and reports back with Ack (tq hub sync
// ack). An agent may import only into planning lanes, only linked cards, and may ack only rows
// the policy sends (auto) or the human approved (an ask answered send). It never answers a
// decision.

// ImportItem is one tracker item as the fallback hands it in (JSON, snake_case).
type ImportItem struct {
	ID             string   `json:"id"`
	Key            string   `json:"key,omitempty"`
	URL            string   `json:"url,omitempty"`
	Title          string   `json:"title"`
	Body           string   `json:"body,omitempty"`
	Status         string   `json:"status,omitempty"`
	StatusCategory string   `json:"status_category,omitempty"` // backlog|todo|active|review|done
	ParentID       string   `json:"parent_id,omitempty"`
	Rev            string   `json:"rev,omitempty"`
	Acceptance     string   `json:"acceptance,omitempty"`
	Labels         []string `json:"labels,omitempty"`
}

// ImportInput is what tq hub sync import reads: the items, and optionally the watermark the
// connector query covered up to (RFC 3339), recorded on the fallback's own cursor
// (FallbackScopeSuffix), never as the engine's watermark.
//
// Statuses, when sent, are the tracker's workflow statuses (name + category); they rebuild the
// lane map as the engine's first poll does, so later moves know which status a lane means.
type ImportInput struct {
	Items     []ImportItem   `json:"items"`
	Statuses  []ImportStatus `json:"statuses,omitempty"`
	Watermark string         `json:"watermark,omitempty"`
}

// ImportStatus is one tracker status for the lane map.
type ImportStatus struct {
	Name     string `json:"name"`
	Category string `json:"category"` // backlog|todo|active|review|done
}

// laneMapFrom builds the lane map from statuses the fallback read (as buildLaneMap does from the
// adapter): the manifest override, else the category's first lane.
//
// A human import rebuilds the map. An agent's statuses are only a stopgap until the engine reads
// the adapter's (spec §5: the map comes from the manifest and the adapter, never from a user): they
// only add statuses the stored map lacks, never replace an entry, never map a status to a done
// lane by category (only a manifest override may), and leave the engine's in-memory statuses alone
// so its first poll with a working credential still builds the map from the adapter.
func (e *Engine) laneMapFrom(in []ImportStatus, agent bool) error {
	var sts []Status
	var entries []store.LaneMapEntry
	have := map[string]bool{}
	if agent {
		cur, err := e.st.LaneMap(e.provider, e.scope)
		if err != nil {
			return err
		}
		for _, m := range cur {
			have[strings.ToLower(m.ExternalStatus)] = true
			entries = append(entries, store.LaneMapEntry{ExternalStatus: m.ExternalStatus, Lane: m.Lane})
		}
	}
	added := 0
	for _, s := range in {
		st := Status{Name: strings.TrimSpace(s.Name), Category: strings.ToLower(strings.TrimSpace(s.Category))}
		if st.Name == "" {
			continue
		}
		sts = append(sts, st)
		if have[strings.ToLower(st.Name)] {
			continue
		}
		lane, ok := e.overrideLane(st.Name)
		if !ok {
			if agent && st.Category == store.LaneDone {
				continue
			}
			lane, ok = e.categoryLane(st.Category)
		}
		if ok {
			have[strings.ToLower(st.Name)] = true
			entries = append(entries, store.LaneMapEntry{ExternalStatus: st.Name, Lane: lane})
			added++
		}
	}
	if len(sts) == 0 || (agent && added == 0) {
		return nil
	}
	if err := e.st.ReplaceLaneMap(e.provider, e.scope, entries); err != nil {
		return err
	}
	if !agent {
		e.statuses, e.mapped = sts, true
	}
	return nil
}

// FallbackScopeSuffix marks the sync_state row of the MCP fallback's own cursor: scope + "#mcp".
// An import never writes the engine's row. The engine lists changes since its own watermark, so
// an import that read only part of the set (lossy MCP paging, missed children) or held a move
// never makes the engine skip a change. sync status shows this row as "fallback".
const FallbackScopeSuffix = "#mcp"

func (e *Engine) fallbackScope() string { return e.scope + FallbackScopeSuffix }

// importWatermark is the watermark an import records on the fallback cursor: the one sent,
// clamped to no later than now and to no earlier than the stored one. "" keeps the stored one.
func (e *Engine) importWatermark(sent string) string {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(sent))
	if err != nil {
		return ""
	}
	if now := time.Now(); t.After(now) {
		t = now
	}
	if st, ok, err := e.st.SyncState(e.provider, e.fallbackScope()); err == nil && ok {
		if prev := parseWatermark(st.Watermark); !prev.IsZero() && !t.After(prev) {
			return ""
		}
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// ParseImport reads an ImportInput, or a bare JSON array of items.
func ParseImport(raw []byte) (ImportInput, error) {
	raw = []byte(strings.TrimSpace(string(raw)))
	var in ImportInput
	if len(raw) == 0 {
		return in, errors.New("no input: send a JSON array of items or {\"items\": [...]}")
	}
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &in.Items); err != nil {
			return in, fmt.Errorf("items: %w", err)
		}
		return in, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, fmt.Errorf("import: %w", err)
	}
	return in, nil
}

func (i ImportItem) item() Item {
	return Item{ID: strings.TrimSpace(i.ID), Key: i.Key, URL: i.URL, Title: i.Title, Body: i.Body, Status: i.Status,
		StatusCategory: strings.ToLower(strings.TrimSpace(i.StatusCategory)), ParentID: strings.TrimSpace(i.ParentID), Rev: i.Rev,
		Acceptance: i.Acceptance, Labels: i.Labels}
}

// ImportResult is one item's outcome.
type ImportResult struct {
	ID     string `json:"id"`
	CardID string `json:"card_id,omitempty"`
	Result string `json:"result"` // created | updated | moved | held | refused
	Lane   string `json:"lane,omitempty"`
	Note   string `json:"note,omitempty"`
}

// ImportReport is what one import did.
type ImportReport struct {
	Provider string         `json:"provider"`
	Scope    string         `json:"scope"`
	Created  int            `json:"created"`
	Updated  int            `json:"updated"`
	Moved    int            `json:"moved"`
	Held     int            `json:"held"`    // lane moves an agent may not make (outside the planning lanes)
	Refused  int            `json:"refused"` // items without an id or a title
	Items    []ImportResult `json:"items"`
}

// planningKind: the lane kinds an agent may add cards to and move between (as runner.agentLane).
func planningKind(kind string) bool {
	return kind == store.LaneBacklog || kind == store.LaneTodo || kind == store.LaneOther
}

func (e *Engine) lock() (func(), error) {
	if !e.enabled() {
		return nil, ErrDisabled
	}
	e.mu.Lock()
	ok, err := e.acquire()
	if err != nil || !ok {
		e.mu.Unlock()
		if err == nil {
			err = ErrBusy
		}
		return nil, err
	}
	return func() { e.release(); e.mu.Unlock() }, nil
}

// Import mirrors items the fallback read into linked cards. A human import is a normal pull
// (lane moves through MoveCard as sync, conflicts as in a poll). An agent import creates cards
// only in planning lanes (an item whose status maps elsewhere lands in the first backlog lane),
// and moves a linked card only from a planning lane to a planning lane; any other move is held:
// the card keeps its lane and its last synced tracker status, and the human moves it.
func (e *Engine) Import(in ImportInput, agent bool) (ImportReport, error) {
	rep := ImportReport{Provider: e.provider, Scope: e.scope, Items: []ImportResult{}}
	unlock, err := e.lock()
	if err != nil {
		return rep, err
	}
	defer unlock()
	// Hub events first, as a poll does: a local move made before this import keeps the tracker
	// state it was made against. On the first pass this also starts the activity cursor, so the
	// board's later moves reach the outbox.
	if err := e.produce(&Report{}); err != nil {
		e.cfg.Logf("sync %s: outbox: %v", e.cfg.Workspace, err)
	}
	if err := e.laneMapFrom(in.Statuses, agent); err != nil {
		return rep, fmt.Errorf("statuses: %w", err)
	}
	var errs []error
	if !agent {
		// As a poll: moves that waited (for a run, or held from an agent import) first; an item in
		// this batch is newer and supersedes them.
		var r Report
		if err := e.applyDeferred(&r); err != nil {
			errs = append(errs, err)
		}
		rep.Moved += r.Moved
	}
	items := make([]Item, 0, len(in.Items))
	for _, ii := range in.Items {
		items = append(items, ii.item())
	}
	for _, it := range parentsFirst(items) {
		res, err := e.importOne(it, agent, &rep)
		if err != nil {
			res = ImportResult{ID: it.ID, Result: "refused", Note: err.Error()}
			rep.Refused++
			errs = append(errs, fmt.Errorf("item %s: %w", it.ID, err))
		}
		rep.Items = append(rep.Items, res)
	}
	if len(errs) == 0 {
		// The fallback's own cursor, never the engine's watermark (see FallbackScopeSuffix).
		if err := e.st.RecordSyncOK(e.provider, e.fallbackScope(), e.importWatermark(in.Watermark)); err != nil {
			return rep, err
		}
	}
	return rep, errors.Join(errs...)
}

func (e *Engine) importOne(it Item, agent bool, rep *ImportReport) (ImportResult, error) {
	if it.ID == "" {
		return ImportResult{}, errors.New("an item needs an id")
	}
	if strings.TrimSpace(it.Title) == "" && strings.TrimSpace(it.Key) == "" {
		return ImportResult{}, errors.New("an item needs a title")
	}
	if err := checkBodyBudget(itemBody(it)); err != nil {
		return ImportResult{}, err
	}
	res := ImportResult{ID: it.ID}
	var textHeld, moveHeld bool
	cur, err := e.st.CardByExternal(e.provider, it.ID)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return res, err
	}
	var r Report
	if !agent {
		if err := e.applyItem(it, &r); err != nil {
			return res, err
		}
	} else if !exists {
		lane := e.laneOf(it.Status, it.StatusCategory)
		if l, err := e.st.Lane(lane); err != nil || !planningKind(l.Kind) {
			if lane != "" {
				res.Note = fmt.Sprintf("the company status %q maps to lane %s, which an agent may not add to; the card starts in the backlog and the human moves it",
					strings.TrimSpace(it.Status), lane)
			}
			first, err := e.st.FirstLane(store.LaneBacklog)
			if err != nil {
				return res, err
			}
			lane = first.Name
		}
		in := store.LinkedCard{Link: store.ExternalLink{Provider: e.provider, ID: it.ID, URL: it.URL, Lane: it.Status, Rev: it.Rev},
			Title: strings.TrimSpace(it.Title), Body: itemBody(it), Lane: lane}
		if in.Title == "" {
			in.Title = strings.TrimSpace(it.Key)
		}
		if it.ParentID != "" {
			if p, perr := e.st.CardByExternal(e.provider, it.ParentID); perr == nil {
				in.ParentID = p.ID
			}
		}
		if _, _, err := e.st.UpsertLinkedCard(in, ActorSync); err != nil {
			return res, err
		}
		r.Created++
	} else {
		var notes []string
		if !e.agentMayRefreshText(cur) {
			// Held text: outside a planning lane, or while a run holds the card, the title and body
			// are the human's (spec A's UpdateCard refuses an agent text edit; a run's next round
			// reads this body). The link keeps its stored url too.
			notes = append(notes, fmt.Sprintf("card %s is in lane %s%s; an agent refreshes the text only of an unlocked card in a planning lane, so the title and body stay",
				cur.ID, cur.Lane, lockedNote(cur)))
			it.Title, it.Key, it.Body, it.Acceptance, it.URL = "", "", "", "", ""
			textHeld = true
		}
		target := e.laneOf(it.Status, it.StatusCategory)
		remoteMoved := strings.TrimSpace(it.Status) != "" && !strings.EqualFold(it.Status, cur.ExternalLane)
		var held deferredMove
		if remoteMoved && target != "" && !strings.EqualFold(target, cur.Lane) && !e.planningMove(cur.Lane, target) {
			// Held: the lane and the tracker status stay as sync last saw them, and the move is
			// queued as a deferred move, which the engine's next pass (or a human import) makes
			// even when its poll does not list the item again.
			notes = append(notes, fmt.Sprintf("the company board has it in %q (lane %s); an agent moves cards only between planning lanes, so the background sync or the human moves it",
				strings.TrimSpace(it.Status), target))
			held = deferredMove{Status: strings.TrimSpace(it.Status), Category: it.StatusCategory, Rev: it.Rev}
			it.Status, it.StatusCategory, it.Rev = "", "", ""
			moveHeld = true
		}
		if len(notes) > 0 {
			res.Result, res.Note = "held", strings.Join(notes, "; ")
			rep.Held++
		}
		prev, hadPrev := e.deferred(cur.ID)
		if err := e.applyItem(it, &r); err != nil { // clears a queued move: re-queued below when held
			return res, err
		}
		if moveHeld {
			if err := e.setDeferred(cur.ID, held); err != nil {
				return res, err
			}
			if !hadPrev || !strings.EqualFold(prev.Status, held.Status) {
				_, _ = e.st.AddComment(cur.ID, store.AuthorSystem, ActorSync, fmt.Sprintf(
					"The company board moved this item to %q. An agent's import (the MCP fallback) does not move cards out of the planning lanes; the background sync moves it on its next pass with a working credential, or move it yourself.",
					held.Status), "")
			}
		}
	}
	c, err := e.st.CardByExternal(e.provider, it.ID)
	if err != nil {
		return res, err
	}
	res.CardID, res.Lane = c.ID, c.Lane
	switch {
	case res.Result == "held":
	case r.Created > 0:
		res.Result = "created"
		rep.Created++
	case r.Moved > 0:
		res.Result = "moved"
		rep.Moved++
		rep.Updated++
	default:
		res.Result = "updated"
		rep.Updated++
	}
	if moveHeld && !textHeld { // the text still refreshed
		rep.Updated++
	}
	return res, nil
}

// agentMayRefreshText: the card is in a planning lane and no run holds it.
func (e *Engine) agentMayRefreshText(c store.Card) bool {
	if c.LockedByRun != "" {
		return false
	}
	l, err := e.st.Lane(c.Lane)
	return err == nil && planningKind(l.Kind)
}

func lockedNote(c store.Card) string {
	if c.LockedByRun != "" {
		return ", held by run " + c.LockedByRun
	}
	return ""
}

// checkBodyBudget refuses a body whose budget front matter a run could not read, as a card edit
// does (runner.checkBodyBudget).
func checkBodyBudget(body string) error {
	if _, err := store.CardBudget(store.Card{Body: body}, store.DefaultBudget); err != nil {
		return fmt.Errorf("card body front matter: %w", err)
	}
	return nil
}

// planningMove: both lanes are planning lanes.
func (e *Engine) planningMove(from, to string) bool {
	a, err := e.st.Lane(from)
	if err != nil {
		return false
	}
	b, err := e.st.Lane(to)
	return err == nil && planningKind(a.Kind) && planningKind(b.Kind)
}

// Sendable is one outbox row the fallback may deliver now, with the text exactly as it may leave
// the machine (redacted with the exit classes).
type Sendable struct {
	ID         string `json:"id"`
	CardID     string `json:"card_id"`
	Action     string `json:"action"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id,omitempty"` // the item to write to ("" for create_card)
	URL        string `json:"url,omitempty"`
	// Moves: the hub lane (and its kind) to move the item to, the tracker statuses that map to that
	// lane, and the status sync last saw: if the item is no longer in base_status, the board moved
	// it meanwhile, so ack --failed "conflict" instead of moving it.
	Lane       string   `json:"lane,omitempty"`
	LaneKind   string   `json:"lane_kind,omitempty"`
	Statuses   []string `json:"statuses,omitempty"`
	BaseStatus string   `json:"base_status,omitempty"`
	// Comments, creates and edits: the redacted text.
	Text             string  `json:"text,omitempty"`
	Title            *string `json:"title,omitempty"`
	Body             *string `json:"body,omitempty"`
	ParentExternalID string  `json:"parent_external_id,omitempty"`
	Approved         bool    `json:"approved,omitempty"` // an ask the human answered send
}

// ErrCredentialWorks: a background credential works, so the engine sends the outbox; the MCP
// fallback must not list rows the engine may send too (the tracker would get the write twice).
var ErrCredentialWorks = errors.New("a background credential works: the sync engine sends the outbox (tq hub sync now); the MCP fallback is only for needs-session")

// claimTTL is how long a row tq hub sync pending listed stays claimed by the fallback: the
// engine skips it meanwhile, so the agent's connector write and an engine send never both
// happen. An ack ends the claim; an expired one gives the row back to the engine.
const claimTTL = 15 * time.Minute

// credProbeTimeout bounds the credential check Sendable makes (the chain's probes are bounded too).
const credProbeTimeout = 20 * time.Second

const settingClaim = "sync.claim." // + outbox row id; the value is the claim's expiry (RFC 3339)

func (e *Engine) claim(id string, now time.Time) error {
	return e.st.SetSetting(settingClaim+id, now.Add(claimTTL).UTC().Format(time.RFC3339Nano))
}

// claimed reports whether the fallback holds row id now; an expired claim is dropped.
func (e *Engine) claimed(id string, now time.Time) bool {
	v, ok, err := e.st.Setting(settingClaim + id)
	if err != nil {
		return true // unsure: leave the row to the fallback's ack, or the next pass
	}
	if !ok {
		return false
	}
	if until, perr := time.Parse(time.RFC3339Nano, v); perr == nil && now.Before(until) {
		return true
	}
	e.unclaim(id)
	return false
}

func (e *Engine) unclaim(id string) {
	_, _ = e.st.DB.Exec(`DELETE FROM settings WHERE key = ?`, settingClaim+id)
}

// needsSession checks that the background credential chain has nothing (cred.ErrNeedsSession).
// A working credential is ErrCredentialWorks; any other failure is returned as it is.
func (e *Engine) needsSession(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, credProbeTimeout)
	defer cancel()
	_, err := e.cfg.Adapter(ctx)
	switch {
	case err == nil:
		return ErrCredentialWorks
	case noSession(err):
		return nil
	default:
		return fmt.Errorf("checking the background credential: %w", err)
	}
}

// Sendable applies the write policy to the pending rows (hub events are queued first, ask rows
// get their send/skip decision, off rows are dropped) and lists the rows that may be sent now:
// auto rows and ask rows the human answered send. It never calls the tracker. It refuses while a
// background credential works (ErrCredentialWorks), and claims each listed row for claimTTL so
// the engine does not send it too.
func (e *Engine) Sendable(ctx context.Context) ([]Sendable, Report, error) {
	rep := Report{Provider: e.provider, Scope: e.scope}
	if !e.enabled() {
		return nil, rep, ErrDisabled
	}
	if err := e.needsSession(ctx); err != nil {
		return nil, rep, err
	}
	unlock, err := e.lock()
	if err != nil {
		return nil, rep, err
	}
	defer unlock()
	now := e.cfg.Now()
	if err := e.produce(&rep); err != nil {
		e.cfg.Logf("sync %s: outbox: %v", e.cfg.Workspace, err)
	}
	rows, err := e.st.ListOutbox(store.OutboxFilter{Status: store.OutboxPending, Limit: 200})
	if err != nil {
		return nil, rep, err
	}
	out := []Sendable{}
	for _, o := range rows {
		send, err := e.decide(o, &rep)
		if err != nil {
			e.cfg.Logf("sync %s: outbox %s: %v", e.cfg.Workspace, o.ID, err)
			continue
		}
		if !send {
			continue
		}
		if cur, err := e.st.OutboxRow(o.ID); err == nil {
			o = cur
		}
		s, err := e.sendable(o)
		if err != nil {
			e.cfg.Logf("sync %s: outbox %s: %v", e.cfg.Workspace, o.ID, err)
			continue
		}
		if err := e.claim(o.ID, now); err != nil {
			return nil, rep, err
		}
		out = append(out, s)
	}
	return out, rep, nil
}

func (e *Engine) sendable(o store.OutboxRow) (Sendable, error) {
	c, err := e.st.Card(o.CardID)
	if err != nil {
		return Sendable{}, err
	}
	s := Sendable{ID: o.ID, CardID: o.CardID, Action: o.Action, Provider: e.provider, Approved: o.DecisionID != ""}
	if linked(c) {
		s.ExternalID, s.URL = c.ExternalID, c.ExternalURL
	}
	switch o.Action {
	case store.OutboxMoveOnStart, store.OutboxMoveOnReview, store.OutboxCloseItem:
		var p movePayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return s, err
		}
		lane, err := e.st.Lane(p.Lane)
		if err != nil {
			if lane, err = e.st.FirstLane(p.Kind); err != nil {
				return s, err
			}
		}
		s.Lane, s.LaneKind, s.BaseStatus = lane.Name, lane.Kind, p.BaseLane
		if lm, err := e.st.LaneMap(e.provider, e.scope); err == nil {
			for _, m := range lm {
				if strings.EqualFold(m.Lane, lane.Name) {
					s.Statuses = append(s.Statuses, m.ExternalStatus)
				}
			}
		}
		for k, v := range e.cfg.Tracker.Lanes {
			if l, ok := e.resolveLane(v); ok && strings.EqualFold(l, lane.Name) && !containsFold(s.Statuses, k) {
				s.Statuses = append(s.Statuses, k)
			}
		}
	case store.OutboxCommentResults:
		var p commentPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return s, err
		}
		s.Text = e.outbound(p.Text)
	case store.OutboxCreateCard:
		var p createPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return s, err
		}
		t, b := e.outbound(p.Title), e.outbound(p.Body)
		s.Title, s.Body, s.ParentExternalID = &t, &b, p.ParentExternalID
	case store.OutboxEditText:
		var p editPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return s, err
		}
		if p.Title != nil {
			t := e.outbound(*p.Title)
			s.Title = &t
		}
		if p.Body != nil {
			b := e.outbound(*p.Body)
			s.Body = &b
		}
	}
	return s, nil
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// ErrNotAllowed: an agent may not ack this outbox row (its policy asks and the human has not
// answered send, or it is off).
var ErrNotAllowed = errors.New("an agent may ack only outbox rows the policy sends (auto) or the human approved")

// AckInput is the fallback's report on one row it tried to deliver.
type AckInput struct {
	Sent   bool
	Why    string // with !Sent: why it failed (masked when stored)
	Status string // a move: the tracker status the item is in now ("" = the row's target is unknown)
	ItemID string // create_card: the new item's id (required with Sent)
	URL    string // create_card: its url
}

// Ack records the outcome of a row the fallback delivered: sent (a move also records the item's
// new status; a create links the card to the new item) or failed. An agent may ack only a row
// that is sendable now.
func (e *Engine) Ack(id string, in AckInput, agent bool) (store.OutboxRow, error) {
	unlock, err := e.lock()
	if err != nil {
		return store.OutboxRow{}, err
	}
	defer unlock()
	o, err := e.st.OutboxRow(id)
	if err != nil {
		return o, err
	}
	if o.Status != store.OutboxPending {
		return o, fmt.Errorf("outbox row %s is %s; only a pending row is acked", id, o.Status)
	}
	if agent {
		ok, err := e.agentMayAck(o)
		if err != nil {
			return o, err
		}
		if !ok {
			return o, fmt.Errorf("outbox row %s (%s): %w", id, o.Action, ErrNotAllowed)
		}
	}
	if !in.Sent {
		why := strings.TrimSpace(in.Why)
		if why == "" {
			why = "failed in the MCP fallback"
		}
		e.unclaim(id)
		return e.st.MarkOutbox(id, store.OutboxFailed, why)
	}
	c, err := e.st.Card(o.CardID)
	if err != nil {
		return o, err
	}
	switch o.Action {
	case store.OutboxCreateCard:
		itemID := strings.TrimSpace(in.ItemID)
		if itemID == "" {
			return o, errors.New("ack --sent of a create_card row needs --item-id: the id of the item the connector created")
		}
		// A link that fails leaves the row pending (and claimed) with the reason, so a corrected
		// ack can still link the card; marking it sent would leave the card unlinked for good.
		linkErr := func(err error) (store.OutboxRow, error) {
			err = fmt.Errorf("created %s but linking card %s failed: %w; ack again with the right --item-id", itemID, c.ID, err)
			_ = e.st.OutboxAttempt(id, err.Error())
			return o, err
		}
		if other, err := e.st.CardByExternal(e.provider, itemID); err == nil && other.ID != c.ID {
			return linkErr(fmt.Errorf("item %s is already linked to card %s", itemID, other.ID))
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return linkErr(err)
		}
		if err := e.st.LinkCard(c.ID, store.ExternalLink{Provider: e.provider, ID: itemID, URL: in.URL, Lane: in.Status}); err != nil {
			return linkErr(err)
		}
	case store.OutboxMoveOnStart, store.OutboxMoveOnReview, store.OutboxCloseItem:
		if st := strings.TrimSpace(in.Status); st != "" && linked(c) {
			_ = e.st.MarkSynced(c.ID, st, "")
			e.rebaseMoves(c.ID, o.ID, st, "")
		}
	}
	e.unclaim(id)
	return e.st.MarkOutbox(id, store.OutboxSent, "")
}

// agentMayAck: the row has no decision and its policy is auto, or its ask was answered send.
func (e *Engine) agentMayAck(o store.OutboxRow) (bool, error) {
	if o.DecisionID == "" {
		return e.st.SyncPolicyFor(o.Action) == store.PolicyAuto, nil
	}
	d, err := e.st.Decision(o.DecisionID)
	if err != nil {
		return false, err
	}
	return d.Status == store.DecisionAnswered && d.Choice == choiceSend, nil
}
