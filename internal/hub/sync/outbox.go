package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// The outbox (spec §4). Hub events are read from the activity log, so a move or an approval made
// by another tq process (tq hub move, tq hub answer) is seen as well as one made in the serving
// hub. The cursor is the activity rowid the engine has handled.
//
//   - card.move of a linked card, by anyone but sync: into an active lane -> move_on_start; into a
//     done lane -> close_item (unless it is the move an approval makes).
//   - review.approve: a linked card -> move_on_review plus comment_results (the summary and PR
//     link); an unlinked child of a linked parent -> comment_results on the parent's item.
//   - tq hub push -> create_card (Engine.Push).
const settingCursor = "sync.outbox_cursor"

// Payload keys.
type movePayload struct {
	Lane     string `json:"lane,omitempty"`      // the hub lane the card went to
	Kind     string `json:"kind,omitempty"`      // the lane kind to move the item to when Lane is gone
	BaseLane string `json:"base_lane,omitempty"` // the tracker status sync last saw when the event happened
	BaseRev  string `json:"base_rev,omitempty"`
}

type commentPayload struct {
	Text  string `json:"text"`
	Child string `json:"child,omitempty"` // the unlinked child card the comment is about
}

type createPayload struct {
	Title            string `json:"title"`
	Body             string `json:"body,omitempty"`
	ParentExternalID string `json:"parent_external_id,omitempty"`
}

type editPayload struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

type actEntry struct {
	rowid int64
	ts    string
	actor string
	verb  string
	card  string
	run   string
	data  map[string]any
}

func (e *Engine) cursor() (int64, bool) {
	v, ok, err := e.st.Setting(settingCursor)
	if err != nil || !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n, err == nil
}

func (e *Engine) setCursor(n int64) error {
	return e.st.SetSetting(settingCursor, strconv.FormatInt(n, 10))
}

// produce turns new activity into outbox rows.
func (e *Engine) produce(rep *Report) error {
	cur, ok := e.cursor()
	if !ok { // the first pass starts from now: history is never replayed to the tracker
		var max int64
		if err := e.st.DB.QueryRow(`SELECT COALESCE(MAX(rowid), 0) FROM activity`).Scan(&max); err != nil {
			return err
		}
		return e.setCursor(max)
	}
	rows, err := e.st.DB.Query(`SELECT rowid, ts, actor, verb, COALESCE(card_id, ''), COALESCE(run_id, ''), data FROM activity
		WHERE rowid > ? AND verb IN (?, ?) ORDER BY rowid LIMIT 500`, cur, activity.VerbCardMove, activity.VerbReviewApprove)
	if err != nil {
		return err
	}
	var es []actEntry
	for rows.Next() {
		var a actEntry
		var data string
		if err := rows.Scan(&a.rowid, &a.ts, &a.actor, &a.verb, &a.card, &a.run, &data); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(data), &a.data)
		es = append(es, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, a := range es {
		hold, err := e.handle(a, es[i+1:], rep)
		if err != nil {
			e.cfg.Logf("sync %s: activity %d: %v", e.cfg.Workspace, a.rowid, err)
		}
		if hold {
			return nil // the next pass sees it again
		}
		if err := e.setCursor(a.rowid); err != nil {
			return err
		}
	}
	return nil
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func linked(c store.Card) bool { return c.ExternalProvider != "" && c.ExternalID != "" }

// handle queues the rows for one entry. hold means: not yet (a done move whose approval may still
// be on its way).
func (e *Engine) handle(a actEntry, rest []actEntry, rep *Report) (bool, error) {
	if a.card == "" {
		return false, nil
	}
	c, err := e.st.Card(a.card)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch a.verb {
	case activity.VerbCardMove:
		if a.actor == ActorSync || !linked(c) || c.ExternalProvider != e.provider {
			return false, nil
		}
		to, err := e.st.Lane(str(a.data, "to"))
		if err != nil {
			return false, nil // the lane is gone
		}
		fromKind := ""
		if from, err := e.st.Lane(str(a.data, "from")); err == nil {
			fromKind = from.Kind
		}
		p := movePayload{Lane: to.Name, Kind: to.Kind, BaseLane: c.ExternalLane, BaseRev: c.ExternalRev}
		switch {
		case to.Kind == store.LaneActive && fromKind != store.LaneActive:
			return false, e.enqueue(c.ID, store.OutboxMoveOnStart, p, rep)
		case to.Kind == store.LaneDone && fromKind != store.LaneDone:
			for _, b := range rest {
				if b.verb == activity.VerbReviewApprove && b.card == a.card {
					return false, nil // the approval queues move_on_review instead
				}
			}
			if ts, err := time.Parse(time.RFC3339, a.ts); err == nil && e.cfg.Now().Sub(ts) < approveGrace {
				return true, nil
			}
			return false, e.enqueue(c.ID, store.OutboxCloseItem, p, rep)
		}
		return false, nil
	case activity.VerbReviewApprove:
		text := e.resultText(c)
		if linked(c) {
			if c.ExternalProvider != e.provider {
				return false, nil
			}
			if err := e.enqueue(c.ID, store.OutboxMoveOnReview, movePayload{Kind: store.LaneReview, BaseLane: c.ExternalLane,
				BaseRev: c.ExternalRev}, rep); err != nil {
				return false, err
			}
			return false, e.enqueue(c.ID, store.OutboxCommentResults, commentPayload{Text: text}, rep)
		}
		if c.ParentID == "" {
			return false, nil
		}
		p, err := e.st.Card(c.ParentID)
		if err != nil || !linked(p) || p.ExternalProvider != e.provider {
			return false, nil
		}
		return false, e.enqueue(p.ID, store.OutboxCommentResults, commentPayload{Text: text, Child: c.ID}, rep)
	}
	return false, nil
}

func (e *Engine) enqueue(cardID, action string, payload any, rep *Report) error {
	if _, err := e.st.EnqueueOutbox(cardID, action, payload); err != nil {
		return err
	}
	rep.Queued++
	return nil
}

var prRe = regexp.MustCompile(`https?://[^\s<>"']+/pull/\d+`) // as runner.PRLink

// resultText is the result comment for an approved card: its title, the result summary and the PR.
func (e *Engine) resultText(c store.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**%s**: approved in tq hub.", strings.TrimSpace(c.Title))
	if ds, err := e.st.DecisionsForCard(c.ID); err == nil {
		for _, d := range ds {
			if d.Kind == store.KindResult && strings.TrimSpace(d.Text) != "" {
				s := strings.TrimSpace(d.Text)
				if len(s) > 4000 {
					s = s[:4000] + " [...]"
				}
				b.WriteString("\n\n" + s)
				break
			}
		}
	}
	if cs, err := e.st.Comments(c.ID); err == nil {
		for i := len(cs) - 1; i >= 0; i-- {
			if m := prRe.FindString(cs[i].Body); m != "" {
				b.WriteString("\n\nPR: " + m)
				break
			}
		}
	}
	return b.String()
}

// Push queues a local card for the company board (tq hub push): a create_card row under its
// policy, as a child of the parent's item when the parent is linked.
func (e *Engine) Push(ctx context.Context, cardID string) (store.OutboxRow, error) {
	c, err := e.st.Card(cardID)
	if err != nil {
		return store.OutboxRow{}, err
	}
	if linked(c) {
		return store.OutboxRow{}, fmt.Errorf("card %s already mirrors %s %s", c.ID, c.ExternalProvider, c.ExternalID)
	}
	pending, err := e.st.ListOutbox(store.OutboxFilter{CardID: c.ID, Action: store.OutboxCreateCard})
	if err != nil {
		return store.OutboxRow{}, err
	}
	for _, o := range pending {
		if o.Status == store.OutboxPending {
			return store.OutboxRow{}, fmt.Errorf("card %s is already queued for the company board (outbox %s)", c.ID, o.ID)
		}
	}
	p := createPayload{Title: c.Title, Body: c.Body}
	if c.ParentID != "" {
		if parent, err := e.st.Card(c.ParentID); err == nil && linked(parent) && parent.ExternalProvider == e.provider {
			p.ParentExternalID = parent.ExternalID
		}
	}
	row, err := e.st.EnqueueOutbox(c.ID, store.OutboxCreateCard, p)
	if err == nil {
		e.Wake()
	}
	return row, err
}

// ---- send ----

const (
	choiceSend = "send"
	choiceSkip = "skip"
)

// errNoSession: the background credential chain has nothing; the row waits for /tentaqles:sync.
func noSession(err error) bool { return errors.Is(err, cred.ErrNeedsSession) }

// permanent errors fail a row at once; others are retried on later ticks up to maxAttempts.
func permanent(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnsupported) || errors.Is(err, store.ErrNotFound)
}

func (e *Engine) send(ctx context.Context, adapter func() (Adapter, error), rep *Report) error {
	rows, err := e.st.ListOutbox(store.OutboxFilter{Status: store.OutboxPending, Limit: 200})
	if err != nil {
		return err
	}
	var firstErr error
	for _, o := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now, err := e.decide(o, rep)
		if err != nil {
			e.cfg.Logf("sync %s: outbox %s: %v", e.cfg.Workspace, o.ID, err)
			continue
		}
		if !now {
			continue
		}
		ad, err := adapter()
		if err != nil {
			// No background credential (or the adapter is not built): the row stays pending.
			if firstErr == nil {
				firstErr = err
			}
			if !noSession(err) {
				_ = e.st.OutboxAttempt(o.ID, err.Error())
			}
			return firstErr
		}
		err = e.deliver(ctx, ad, o, rep)
		switch {
		case err == nil:
		case isRateLimit(err):
			_ = e.st.OutboxAttempt(o.ID, err.Error())
			// Rate limited after the retries: stop for this tick, the rest wait too.
			return err
		case permanent(err):
			_, _ = e.st.MarkOutbox(o.ID, store.OutboxFailed, err.Error())
			rep.Failed++
		default:
			if o.Attempts+1 >= maxAttempts {
				_, _ = e.st.MarkOutbox(o.ID, store.OutboxFailed, err.Error())
				rep.Failed++
			} else {
				_ = e.st.OutboxAttempt(o.ID, err.Error())
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// decide applies the write policy; true means send it now.
func (e *Engine) decide(o store.OutboxRow, rep *Report) (bool, error) {
	if o.DecisionID != "" {
		d, err := e.st.Decision(o.DecisionID)
		if err != nil {
			return false, err
		}
		if d.Status != store.DecisionAnswered {
			return false, nil
		}
		if d.Choice == choiceSend {
			return true, nil
		}
		if _, err := e.st.MarkOutbox(o.ID, store.OutboxSkipped, "skipped by "+d.Actor); err != nil {
			return false, err
		}
		rep.Skipped++
		return false, nil
	}
	switch e.st.SyncPolicyFor(o.Action) {
	case store.PolicyAuto:
		return true, nil
	case store.PolicyOff:
		if _, err := e.st.MarkOutbox(o.ID, store.OutboxSkipped, "policy off: sync.policy."+o.Action); err != nil {
			return false, err
		}
		rep.Dropped++
		return false, nil
	}
	d, err := e.st.AskHub(o.CardID, "", "", e.askText(o), []string{choiceSend, choiceSkip},
		map[string]any{"outbox": o.ID, "action": o.Action, "provider": e.provider})
	if err != nil {
		return false, err
	}
	if err := e.st.SetOutboxDecision(o.ID, d.ID); err != nil {
		return false, err
	}
	rep.Asked++
	return false, nil
}

func preview(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		s = s[:600] + " [...]"
	}
	return s
}

// askText is the permission question, with the redacted text that would leave the machine.
func (e *Engine) askText(o store.OutboxRow) string {
	c, _ := e.st.Card(o.CardID)
	item := e.provider
	if linked(c) {
		item = e.provider + " " + c.ExternalID
	}
	switch o.Action {
	case store.OutboxMoveOnStart, store.OutboxMoveOnReview, store.OutboxCloseItem:
		var p movePayload
		_ = json.Unmarshal(o.Payload, &p)
		where := p.Lane
		if where == "" {
			where = p.Kind
		}
		verb := "Move"
		if o.Action == store.OutboxCloseItem {
			verb = "Close"
		}
		return fmt.Sprintf("Sync (%s): %s %s on the company board to its %s status? send or skip.", o.Action, verb, item, where)
	case store.OutboxCommentResults:
		var p commentPayload
		_ = json.Unmarshal(o.Payload, &p)
		return fmt.Sprintf("Sync (%s): post this comment on %s? send or skip.\n\n%s", o.Action, item, preview(e.outbound(p.Text)))
	case store.OutboxCreateCard:
		var p createPayload
		_ = json.Unmarshal(o.Payload, &p)
		where := "as a new item"
		if p.ParentExternalID != "" {
			where = "as a child of " + e.provider + " " + p.ParentExternalID
		}
		return fmt.Sprintf("Sync (%s): create %q on the company board %s? send or skip.\n\n%s", o.Action,
			e.outbound(p.Title), where, preview(e.outbound(p.Body)))
	case store.OutboxEditText:
		return fmt.Sprintf("Sync (%s): update the title or description of %s on the company board? send or skip.", o.Action, item)
	}
	return fmt.Sprintf("Sync: send %s for %s? send or skip.", o.Action, item)
}

// deliver makes the tracker call for one row and marks it.
func (e *Engine) deliver(ctx context.Context, ad Adapter, o store.OutboxRow, rep *Report) error {
	c, err := e.st.Card(o.CardID)
	if err != nil {
		return err
	}
	sent := func() error {
		if _, err := e.st.MarkOutbox(o.ID, store.OutboxSent, ""); err != nil {
			return err
		}
		rep.Sent++
		return nil
	}
	skip := func(why string) error {
		if _, err := e.st.MarkOutbox(o.ID, store.OutboxSkipped, why); err != nil {
			return err
		}
		rep.Skipped++
		return nil
	}
	switch o.Action {
	case store.OutboxMoveOnStart, store.OutboxMoveOnReview, store.OutboxCloseItem:
		if !linked(c) {
			return skip("the card is not linked to a tracker item")
		}
		var p movePayload
		if err := json.Unmarshal(o.Payload, &p); err != nil {
			return fmt.Errorf("%w: bad payload", store.ErrNotFound)
		}
		lane, err := e.st.Lane(p.Lane)
		if err != nil {
			if lane, err = e.st.FirstLane(p.Kind); err != nil {
				return err
			}
		}
		if len(e.statuses) == 0 {
			if err := e.buildLaneMap(ctx, ad, rep); err != nil {
				return err
			}
		}
		status, ok := e.statusFor(lane)
		if !ok {
			return fmt.Errorf("%w: no %s status maps to lane %s", ErrUnsupported, e.provider, lane.Name)
		}
		it, err := retry(ctx, e, rep, func() (Item, error) { return ad.Get(ctx, c.ExternalID) })
		if err != nil {
			return err
		}
		if p.BaseLane != "" && it.Status != "" && !strings.EqualFold(it.Status, p.BaseLane) &&
			!(p.BaseRev != "" && it.Rev == p.BaseRev) {
			_, _ = e.st.AddComment(c.ID, store.AuthorSystem, ActorSync, fmt.Sprintf(
				"The company board moved %s %s to %q since this card moved; not moving it to %q.", e.provider, c.ExternalID, it.Status, status), "")
			return skip(fmt.Sprintf("conflict: the item is %q on the company board, newer than %q", it.Status, p.BaseLane))
		}
		if !strings.EqualFold(it.Status, status) {
			if _, err := retry(ctx, e, rep, func() (struct{}, error) { return struct{}{}, ad.Move(ctx, c.ExternalID, status) }); err != nil {
				return err
			}
		}
		_ = e.st.MarkSynced(c.ID, status, it.Rev)
		return sent()
	case store.OutboxCommentResults:
		if !linked(c) {
			return skip("the card is not linked to a tracker item")
		}
		var p commentPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil || strings.TrimSpace(p.Text) == "" {
			return fmt.Errorf("%w: empty comment", store.ErrNotFound)
		}
		text := e.outbound(p.Text)
		if _, err := retry(ctx, e, rep, func() (struct{}, error) { return struct{}{}, ad.Comment(ctx, c.ExternalID, text) }); err != nil {
			return err
		}
		return sent()
	case store.OutboxCreateCard:
		if linked(c) {
			return skip("the card is already linked to " + c.ExternalProvider + " " + c.ExternalID)
		}
		var p createPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil || strings.TrimSpace(p.Title) == "" {
			return fmt.Errorf("%w: no title", store.ErrNotFound)
		}
		in := NewItem{Title: e.outbound(p.Title), Body: e.outbound(p.Body), ParentID: p.ParentExternalID}
		it, err := retry(ctx, e, rep, func() (Item, error) { return ad.Create(ctx, in) })
		if err != nil {
			return err
		}
		if err := e.st.LinkCard(c.ID, store.ExternalLink{Provider: e.provider, ID: it.ID, URL: it.URL, Lane: it.Status, Rev: it.Rev}); err != nil {
			// Created on the tracker but not linked here: say so, never create it twice.
			_, _ = e.st.MarkOutbox(o.ID, store.OutboxSent, "created "+it.ID+" but linking failed: "+err.Error())
			rep.Sent++
			return nil
		}
		return sent()
	case store.OutboxEditText:
		if !linked(c) {
			return skip("the card is not linked to a tracker item")
		}
		var p editPayload
		if err := json.Unmarshal(o.Payload, &p); err != nil || (p.Title == nil && p.Body == nil) {
			return fmt.Errorf("%w: nothing to edit", store.ErrNotFound)
		}
		var title, body *string
		if p.Title != nil {
			t := e.outbound(*p.Title)
			title = &t
		}
		if p.Body != nil {
			b := e.outbound(*p.Body)
			body = &b
		}
		if _, err := retry(ctx, e, rep, func() (struct{}, error) { return struct{}{}, ad.Update(ctx, c.ExternalID, title, body) }); err != nil {
			return err
		}
		return sent()
	}
	return fmt.Errorf("%w: unknown outbox action %q", store.ErrNotFound, o.Action)
}
