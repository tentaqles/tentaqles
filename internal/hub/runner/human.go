package runner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func endsRun(kind, choice string) bool {
	return choice == "stop" || (kind == store.KindResult && choice == "approve")
}

// Answer carries a human's answer to the gate (spec §9.2). The hub only ever calls build-gate answer.
func (s *Service) Answer(ctx context.Context, cardID, decisionID, choice, note string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	d, err := s.st.Decision(decisionID)
	if err != nil {
		return AnswerOutcome{}, err
	}
	if d.CardID != cardID {
		return AnswerOutcome{}, fmt.Errorf("decision %s belongs to card %s, not %s", decisionID, d.CardID, cardID)
	}
	if d.Status == store.DecisionAnswered {
		return AnswerOutcome{Decision: d, AlreadyAnswered: true, Message: fmt.Sprintf("decision %s was already answered: %s by %s", d.ID, d.Choice, d.Actor)}, nil
	}
	if len(d.Options) > 0 && !slices.Contains(d.Options, choice) {
		return AnswerOutcome{}, fmt.Errorf("choice must be one of %s", strings.Join(d.Options, ", "))
	}
	card, err := s.st.Card(cardID)
	if err != nil {
		return AnswerOutcome{}, err
	}
	if d.HubOwned() {
		return s.answerHub(ctx, card, d, choice, note)
	}
	r, err := s.st.Run(d.RunID)
	if err != nil {
		return AnswerOutcome{}, fmt.Errorf("decision %s has no run in this hub (%v); answer it in your terminal with build-gate answer", d.ID, err)
	}
	if st := decodeState(r.State); st != nil && st.RolledBack {
		return AnswerOutcome{}, fmt.Errorf("the gate state of %s was rolled back; re-approve in your own terminal (build-gate approve %s) before answering", d.Slug, d.Slug)
	}
	if d.Slug == "" {
		return AnswerOutcome{}, fmt.Errorf("decision %s has no build slug; answer it in your terminal with build-gate answer", d.ID)
	}
	msg, err := s.cfg.Gate.Answer(ctx, s.RunDir(r), d.Slug, d.ID, choice, note, s.cfg.Actor)
	if err != nil {
		if gate.IsNotPending(err) {
			return s.answeredElsewhere(ctx, card, r, d)
		}
		// the gate may have recorded the answer before exiting non-zero: mirror what it holds
		s.refresh(ctx, card, &r, d.Slug)
		cur, _ := s.st.Decision(d.ID)
		return AnswerOutcome{Decision: cur, Message: msg}, err
	}
	s.refresh(ctx, card, &r, d.Slug)
	if cur, _ := s.st.Decision(d.ID); cur.Status != store.DecisionAnswered {
		// the gate confirmed the answer; record it even if its state document lags
		_, _ = s.st.UpsertDecision(store.Decision{ID: d.ID, Status: store.DecisionAnswered, Choice: choice, Note: note, Actor: s.cfg.Actor, AnsweredAt: s.stamp()})
	}
	out := AnswerOutcome{Message: msg}
	out.Decision, _ = s.st.Decision(d.ID)
	s.afterAnswer(ctx, card, r, d.Kind, choice, &out)
	return out, nil
}

// refresh re-reads the gate state of the run and mirrors it.
func (s *Service) refresh(ctx context.Context, card store.Card, r *store.Run, slug string) *gate.State {
	if slug == "" {
		slug = r.Slug
	}
	st, err := s.cfg.Gate.State(ctx, s.RunDir(*r), slug, false)
	if err != nil {
		return nil
	}
	r.State = st.Raw
	s.save(*r)
	s.mirror(card, *r, st)
	return st
}

func (s *Service) answeredElsewhere(ctx context.Context, card store.Card, r store.Run, d store.Decision) (AnswerOutcome, error) {
	s.refresh(ctx, card, &r, d.Slug)
	cur, _ := s.st.Decision(d.ID)
	out := AnswerOutcome{Decision: cur, AlreadyAnswered: true}
	if cur.Status != store.DecisionAnswered {
		out.Message = fmt.Sprintf("the gate no longer has decision %s pending; the board was refreshed", d.ID)
		return out, nil
	}
	out.Message = fmt.Sprintf("decision %s was already answered in the terminal (%s by %s)", d.ID, cur.Choice, cur.Actor)
	s.afterAnswer(ctx, card, r, cur.Kind, cur.Choice, &out)
	return out, nil
}

func (s *Service) afterAnswer(ctx context.Context, card store.Card, r store.Run, kind, choice string, out *AnswerOutcome) {
	switch {
	case kind == store.KindResult && choice == "approve":
		s.finishApproved(card, r)
	case choice == "stop":
		if cur, err := s.st.Run(r.ID); err == nil && cur.Active() {
			s.end(cur, card, store.StatusCancelled, "stopped by human")
		}
	default:
		if kind == store.KindResult && choice == "request-changes" {
			_ = s.st.Log(activity.Entry{Actor: s.human(), Verb: activity.VerbReviewFeedback, CardID: card.ID, RunID: r.ID})
			s.moveToKind(card, store.LaneActive)
		}
		if run, created, err := s.Enqueue(ctx, card.ID, "answered"); err == nil && created {
			out.Resumed = &run
		}
	}
}

var prRe = regexp.MustCompile(`https?://[^\s<>"']+/pull/\d+`)

// PRLink is the newest pull-request URL in the card's comments (ship posts it with tq hub comment).
func PRLink(st *store.Store, cardID string) string {
	cs, err := st.Comments(cardID)
	if err != nil {
		return ""
	}
	for i := len(cs) - 1; i >= 0; i-- {
		if m := prRe.FindString(cs[i].Body); m != "" {
			return m
		}
	}
	return ""
}

// PRLinks is PRLink for many cards with one comments query: only comments that contain "/pull/"
// (a substring every prRe match has) are read, newest first, so each card's first match is the
// one PRLink would return. A card without a PR link has no entry.
func PRLinks(st *store.Store, cardIDs []string) map[string]string {
	out := map[string]string{}
	cs, err := st.CommentsContaining(cardIDs, "/pull/")
	if err != nil {
		return out
	}
	for _, c := range cs {
		if _, ok := out[c.CardID]; ok {
			continue
		}
		if m := prRe.FindString(c.Body); m != "" {
			out[c.CardID] = m
		}
	}
	return out
}

// finishApproved: the gate ran finish for an approved result; close the run, move the card to done,
// tell the parent, export the review.
// The terminal poll and a hub answer can both see the same approval; only the first closes it.
func (s *Service) finishApproved(card store.Card, r store.Run) {
	s.enqMu.Lock()
	if cur, err := s.st.Run(r.ID); err == nil {
		r = cur
	}
	if r.Status == store.StatusFinished {
		s.enqMu.Unlock()
		return
	}
	if r.Active() {
		s.end(r, card, store.StatusFinished, "")
		r, _ = s.st.Run(r.ID)
	}
	s.enqMu.Unlock()
	if lane, err := s.st.FirstLane(store.LaneDone); err == nil {
		_, _ = s.st.MoveCard(card.ID, lane.Name, s.human())
	}
	_ = s.st.Log(activity.Entry{Actor: s.human(), Verb: activity.VerbReviewApprove, CardID: card.ID, RunID: r.ID})
	if card.ParentID != "" {
		msg := card.Title + ": approved"
		if pr := PRLink(s.st, card.ID); pr != "" {
			msg += ", PR " + pr
		}
		_, _ = s.st.AddComment(card.ParentID, store.AuthorSystem, "hub", msg, r.ID)
	}
	if s.OnApprove != nil {
		s.OnApprove(card, r)
	}
}

func (s *Service) answerHub(ctx context.Context, card store.Card, d store.Decision, choice, note string) (AnswerOutcome, error) {
	// A raise-and-resume note is checked before the decision is consumed: a bad note keeps it pending.
	var raised *store.Budget
	if choice == "raise-and-resume" && strings.TrimSpace(note) != "" {
		base := s.st.DefaultBudget()
		if r, err := s.st.Run(d.RunID); err == nil {
			base = r.Budget
		}
		nb, err := store.ParseBudget(note, base)
		if err != nil {
			return AnswerOutcome{}, fmt.Errorf("the note is not a budget (%v); the decision is still pending", err)
		}
		raised = &nb
	}
	d, err := s.st.AnswerHub(d.ID, choice, note, s.cfg.Actor)
	if err != nil {
		return AnswerOutcome{}, err
	}
	out := AnswerOutcome{Decision: d, Message: fmt.Sprintf("decision %s: %s by %s", d.ID, choice, s.cfg.Actor)}
	r, err := s.st.Run(d.RunID)
	if err != nil {
		return out, nil
	}
	switch choice {
	case "allow-once":
		tool, _ := d.Data["tool"].(string)
		// Under enqMu so a poll cannot Enqueue the run between the read and the save and start a
		// round without the tool.
		s.enqMu.Lock()
		if cur, err := s.st.Run(r.ID); err == nil {
			r = cur
		}
		if r.Active() && tool != "" {
			r.AllowedTools = []string{tool}
			r.Status, r.NotBefore = store.StatusQueued, s.stamp()
			s.save(r)
			out.Resumed = &r
		}
		s.enqMu.Unlock()
	case "raise-and-resume":
		b := r.Budget
		if raised != nil {
			b = *raised
		}
		phase := store.PhaseStart
		if r.Slug != "" {
			phase = store.PhaseResume
		}
		now := s.stamp()
		nr, err := s.st.CreateRun(store.Run{CardID: card.ID, Adapter: r.Adapter, OnRamp: store.OnRampResume, ProjectDir: r.ProjectDir,
			Worktree: r.Worktree, Slug: r.Slug, SessionID: r.SessionID, Phase: phase, Budget: b, Kind: r.Kind, QueuedAt: now, NotBefore: now})
		if err != nil {
			return out, err
		}
		ok, err := s.st.Checkout(card.ID, nr.ID)
		if err != nil || !ok {
			s.end(nr, card, store.StatusCancelled, "card locked by another run")
			return out, ErrLocked
		}
		out.Resumed = &nr
	case "stop":
		if r.Active() {
			s.end(r, card, store.StatusCancelled, "stopped by human")
		}
	}
	return out, nil
}

func agentLane(kind string) bool {
	return kind == store.LaneBacklog || kind == store.LaneTodo || kind == store.LaneOther
}

// Move is a human (or agent) moving a card; system moves inside the runner use moveToKind.
func (s *Service) Move(ctx context.Context, id, laneName string, cancelRun bool) (MoveOutcome, error) {
	card, err := s.st.Card(id)
	if err != nil {
		return MoveOutcome{}, err
	}
	to, err := s.st.Lane(laneName)
	if err != nil {
		return MoveOutcome{}, err
	}
	from, err := s.st.Lane(card.Lane)
	if err != nil {
		return MoveOutcome{}, err
	}
	// An agent only shuffles planning lanes; it never starts, cancels, reviews or closes work.
	if IsAgent(ctx) && (!agentLane(from.Kind) || !agentLane(to.Kind)) {
		return MoveOutcome{}, ErrHumanOnly
	}
	out := MoveOutcome{}
	act, ok, err := s.st.ActiveRun(id)
	if err != nil {
		return out, err
	}
	if ok && to.Kind != store.LaneActive && !(to.Kind == store.LaneReview && act.Status == store.StatusWaiting) &&
		(from.Kind == store.LaneActive || from.Kind == store.LaneReview || to.Kind == store.LaneDone) {
		if act.Status != store.StatusQueued && !cancelRun {
			return MoveOutcome{}, &RunActiveError{Run: act}
		}
		c, err := s.Cancel(ctx, act.ID)
		if err != nil {
			return out, err
		}
		out.Cancelled = &c
	}
	actor := s.human()
	if IsAgent(ctx) {
		actor = "agent"
	}
	res, err := s.st.MoveCard(id, to.Name, actor)
	if err != nil {
		return out, err
	}
	out.Card = res.Card
	if res.EnteredActive() && res.Card.HasTag(store.TagAgentReady) {
		run, created, err := s.Enqueue(ctx, id, "moved to "+to.Name)
		switch {
		case err == nil && created:
			out.Enqueued = &run
		case err != nil && !errors.Is(err, ErrLocked):
			return out, err
		}
	}
	return out, nil
}

// PendingResult is the card's pending result decision, the one the review page answers.
func (s *Service) PendingResult(cardID string) (store.Decision, bool) {
	ds, err := s.st.DecisionsForCard(cardID)
	if err != nil {
		return store.Decision{}, false
	}
	for _, d := range ds {
		if d.Kind == store.KindResult && d.Status == store.DecisionPending {
			return d, true
		}
	}
	return store.Decision{}, false
}

func (s *Service) Approve(ctx context.Context, cardID string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	d, ok := s.PendingResult(cardID)
	if !ok {
		return AnswerOutcome{}, fmt.Errorf("card %s has no pending result decision; use Mark done", cardID)
	}
	return s.Answer(ctx, cardID, d.ID, "approve", "")
}

func (s *Service) RequestChanges(ctx context.Context, cardID, note string) (AnswerOutcome, error) {
	if IsAgent(ctx) {
		return AnswerOutcome{}, ErrHumanOnly
	}
	if strings.TrimSpace(note) == "" {
		return AnswerOutcome{}, errors.New("say what to change: the note goes to the engine's request-changes path")
	}
	d, ok := s.PendingResult(cardID)
	if !ok {
		return AnswerOutcome{}, fmt.Errorf("card %s has no pending result decision", cardID)
	}
	if _, err := s.st.AddComment(cardID, store.AuthorHuman, s.human(), note, d.RunID); err != nil {
		return AnswerOutcome{}, err
	}
	return s.Answer(ctx, cardID, d.ID, "request-changes", note)
}

// MarkDone is the board action for a card with no pending result (no gate call).
func (s *Service) MarkDone(ctx context.Context, cardID string) (store.Card, error) {
	if IsAgent(ctx) {
		return store.Card{}, ErrHumanOnly
	}
	if _, ok := s.PendingResult(cardID); ok {
		return store.Card{}, fmt.Errorf("card %s has a pending result: approve it or request changes on the review page", cardID)
	}
	if act, ok, _ := s.st.ActiveRun(cardID); ok {
		return store.Card{}, &RunActiveError{Run: act}
	}
	lane, err := s.st.FirstLane(store.LaneDone)
	if err != nil {
		return store.Card{}, err
	}
	res, err := s.st.MoveCard(cardID, lane.Name, s.human())
	return res.Card, err
}

// pollWaiting notices answers given in the terminal (spec §9.2): waiting runs re-read the gate
// at most every PollEvery; nothing pending and nothing hub-owned pending means resume or close.
func (s *Service) pollWaiting(ctx context.Context) {
	runs, err := s.st.RunsWithStatus(store.StatusWaiting)
	if err != nil {
		return
	}
	now := s.cfg.Now()
	for _, r := range runs {
		if r.Slug == "" {
			continue
		}
		s.mu.Lock()
		last, seen := s.lastPoll[r.ID]
		due := !seen || now.Sub(last) >= s.cfg.PollEvery
		if due {
			s.lastPoll[r.ID] = now
		}
		s.mu.Unlock()
		if !due || s.hubPending(r) {
			continue
		}
		card, err := s.st.Card(r.CardID)
		if err != nil {
			continue
		}
		st := s.refresh(ctx, card, &r, r.Slug)
		if st == nil || st.RolledBack || st.Halted != nil || len(st.PendingDecisions) > 0 {
			continue // a rolled-back or halted build waits for the human
		}
		if st.Finished {
			s.finishApproved(card, r)
			continue
		}
		if last := lastAnswered(st); last != nil && last.Choice == "stop" {
			s.end(r, card, store.StatusCancelled, "stopped by human in the terminal")
			continue
		}
		_, _, _ = s.Enqueue(ctx, card.ID, "answered in the terminal")
	}
}

func (s *Service) hubPending(r store.Run) bool {
	ds, err := s.st.DecisionsForCard(r.CardID)
	if err != nil {
		return false
	}
	for _, d := range ds {
		if d.HubOwned() && d.Status == store.DecisionPending && d.RunID == r.ID {
			return true
		}
	}
	return false
}

func lastAnswered(st *gate.State) *gate.Decision {
	var best *gate.Decision
	for i := range st.Decisions {
		d := &st.Decisions[i]
		if d.Status == store.DecisionAnswered && (best == nil || d.AnsweredAt >= best.AnsweredAt) {
			best = d
		}
	}
	return best
}

var _ Ops = (*Service)(nil)
