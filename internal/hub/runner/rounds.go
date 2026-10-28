package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// Enqueue coalesces wakeups: an active run is reused (a waiting run with nothing pending is
// resumed); otherwise a new run is created and the card checked out in one statement.
func (s *Service) Enqueue(ctx context.Context, cardID, reason string) (store.Run, bool, error) {
	s.enqMu.Lock()
	defer s.enqMu.Unlock()
	card, err := s.st.Card(cardID)
	if err != nil {
		return store.Run{}, false, err
	}
	act, ok, err := s.st.ActiveRun(cardID)
	if err != nil {
		return store.Run{}, false, err
	}
	if ok {
		if act.Status != store.StatusWaiting {
			return act, false, nil
		}
		n, err := s.st.PendingCount(cardID)
		if err != nil || n > 0 {
			return act, false, err
		}
		act.Status, act.NotBefore = store.StatusQueued, s.stamp()
		if act.Slug != "" {
			act.Phase = store.PhaseResume
		}
		return act, true, s.st.SaveRun(act)
	}
	onRamp, phase, slug, wt := OnRampFor(card.Kind), store.PhaseStart, "", ""
	if prev, err := s.st.RunsForCard(cardID); err == nil {
		for _, p := range prev {
			if p.Slug != "" {
				onRamp, phase, slug, wt = store.OnRampResume, store.PhaseResume, p.Slug, p.Worktree
				break
			}
		}
	}
	budget, berr := store.CardBudget(card, s.st.DefaultBudget())
	if berr != nil {
		budget = s.st.DefaultBudget()
		s.comment(card, store.Run{}, "The card's budget front matter is unreadable ("+berr.Error()+"); the workspace default budget applies.")
	}
	now := s.stamp()
	run, err := s.st.CreateRun(store.Run{CardID: cardID, Adapter: s.adapterFor(card), OnRamp: onRamp, ProjectDir: card.ProjectDir,
		Slug: slug, Worktree: wt, Phase: phase, Budget: budget, Kind: card.Kind, QueuedAt: now, NotBefore: now})
	if err != nil {
		return store.Run{}, false, err
	}
	got, err := s.st.Checkout(cardID, run.ID)
	if err != nil {
		return run, false, err
	}
	if !got {
		cur, _ := s.st.Card(cardID)
		// No active run exists for this card, so a lock left by an ended or missing run is an orphan.
		if holder := cur.LockedByRun; holder != "" {
			if hr, herr := s.st.Run(holder); herr != nil || !hr.Active() {
				if err := s.st.ForceRelease(cardID); err != nil {
					return run, false, err
				}
				if got, err = s.st.Checkout(cardID, run.ID); err != nil {
					return run, false, err
				}
				if got {
					s.comment(cur, run, fmt.Sprintf("The hub released an orphan lock left by run %s (that run had ended or no longer exists) and started this run (%s).", holder, reason))
					return run, true, nil
				}
				cur, _ = s.st.Card(cardID)
			}
		}
		run.Status, run.EndedAt, run.StopReason = store.StatusCancelled, now, "card locked by run "+cur.LockedByRun
		s.save(run)
		s.comment(cur, run, fmt.Sprintf("A run was requested (%s) but the card is locked by run %s; the request was dropped. If that run is gone, `tq hub cancel %s` releases the card.",
			reason, cur.LockedByRun, cur.LockedByRun))
		return run, false, ErrLocked
	}
	return run, true, nil
}

// Tick polls waiting runs for answers given in the terminal, then starts due queued runs up to the workspace's concurrency limit, oldest first.
func (s *Service) Tick(ctx context.Context) error {
	_, err := s.tick(ctx)
	return err
}

// dueScan bounds how many due runs one tick looks at: a run waiting for its project's lock is
// passed over without using a concurrency slot.
const dueScan = 200

// tick is Tick, also reporting how many rounds it started.
func (s *Service) tick(ctx context.Context) (int, error) {
	if ctx.Err() != nil {
		return 0, nil // shutting down: Start's select may pick the ticker after ctx ended
	}
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	s.pollWaiting(ctx)
	limit := s.st.Concurrency()
	s.mu.Lock()
	free := limit - len(s.flights)
	s.mu.Unlock()
	if free <= 0 {
		return 0, nil
	}
	due, err := s.st.DueRuns(s.cfg.Now(), max(free, dueScan))
	if err != nil {
		return 0, err
	}
	started := 0
	for _, r := range due {
		if started >= free {
			break
		}
		if s.startRound(ctx, r) {
			started++
		}
	}
	return started, nil
}

// startRound starts r's next round; false means it did not (not queued any more, or waiting
// for its project's pre-relocation lock).
func (s *Service) startRound(parent context.Context, r store.Run) bool {
	// The queued → running transition happens under mu, after a fresh read, so a run Cancel is
	// ending (or one another path already moved on) is never started.
	startable := func() (store.Run, bool) {
		if _, busy := s.flights[r.ID]; busy || s.cancelling[r.ID] {
			return store.Run{}, false
		}
		cur, err := s.st.Run(r.ID)
		if err != nil || cur.Status != store.StatusQueued {
			return cur, false
		}
		return cur, true
	}
	s.mu.Lock()
	cur, ok := startable()
	s.mu.Unlock()
	if !ok {
		return false
	}
	r = cur
	if needsProjectLock(r) {
		// The lock's write transaction runs without mu: another process holding the DB write lock
		// would otherwise stall Cancel and the flight bookkeeping for the whole busy timeout.
		if s.beforeProjectLock != nil {
			s.beforeProjectLock()
		}
		ok, holder, err := s.takeProjectLock(r)
		if err != nil || !ok {
			if err == nil {
				s.noteProjectWait(r, holder)
			}
			return false // stays queued; a later tick tries again
		}
	}
	s.mu.Lock()
	if cur, ok = startable(); !ok {
		s.mu.Unlock()
		// Cancelled (or moved on) while the lock was taken: an ended run gives the lock back.
		if needsProjectLock(r) && cur.ID != "" && !cur.Active() {
			s.releaseProjectLock(r)
		}
		return false
	}
	r = cur
	first := r.StartedAt == ""
	r.Status = store.StatusRunning
	if first {
		r.StartedAt = s.stamp()
	}
	if err := s.st.SaveRun(r); err != nil {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(parent)
	f := &flight{cancel: cancel, done: make(chan struct{})}
	s.flights[r.ID] = f
	s.mu.Unlock()
	finish := func() {
		s.mu.Lock()
		delete(s.flights, r.ID)
		s.mu.Unlock()
		cancel()
		close(f.done)
	}
	if first {
		_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunStart, CardID: r.CardID, RunID: r.ID,
			Data: map[string]any{"adapter": r.Adapter, "on_ramp": r.OnRamp, "kind": r.Kind}})
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer finish()
		s.round(ctx, f, r)
	}()
	return true
}

func decodeState(raw json.RawMessage) *gate.State {
	if len(raw) == 0 {
		return nil
	}
	var st gate.State
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	st.Raw = raw
	return &st
}

// shutdownStateRead bounds the gate read made while the hub stops.
const shutdownStateRead = 5 * time.Second

func (s *Service) round(ctx context.Context, f *flight, r store.Run) {
	card, err := s.st.Card(r.CardID)
	if err != nil {
		s.end(r, store.Card{}, store.StatusFailed, "card missing")
		return
	}
	if why := r.Exceeded(); why != "" {
		s.overBudget(r, card, why)
		return
	}
	dir := s.RunDir(r)
	if !s.preflight(ctx, r, card, dir) {
		return // held for the human at zero cost: trust, folder or git
	}
	ad, ok := s.cfg.Adapters[r.Adapter]
	if !ok {
		s.fail(r, card, "adapter "+r.Adapter+" is not configured", "")
		return
	}
	prevKey := StateKey(decodeState(r.State))
	if err := os.MkdirAll(filepath.Dir(r.LogPath), 0o700); err != nil {
		s.fail(r, card, "cannot create the runs folder", err.Error())
		return
	}
	logf, err := os.OpenFile(r.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		s.fail(r, card, "cannot open the run log", err.Error())
		return
	}
	mw := redact.NewWriter(logf, s.st.Classes())
	start := s.cfg.Now()
	fmt.Fprintf(mw, "\n=== round %d at %s in %s\n", r.Rounds+1, start.UTC().Format(time.RFC3339), dir)
	req := adapter.RunRequest{Workspace: s.cfg.Workspace, Dir: dir, Prompt: Prompt(r, card, s.humanNotes(card.ID)), SessionID: r.SessionID,
		Env: adapter.LaunchEnv(s.cfg.Env(), HubTaskJSON(s.cfg.Workspace, card)), AllowedTools: r.AllowedTools,
		Limit: RoundLimit(r), Classes: s.st.Classes()}
	res, runErr := ad.Start(ctx, req, mw)
	mw.Close()
	logf.Close()
	secs := s.cfg.Now().Sub(start).Seconds()
	if cur, err := s.st.Run(r.ID); err == nil {
		if !cur.Active() {
			return // ended elsewhere while the round ran: apply nothing over it
		}
		r = cur
	}
	r.Rounds++
	r.Seconds += secs
	r.Usage = r.Usage.Add(store.Usage(res.Usage))
	r.CostUSD += res.CostUSD
	r.AllowedTools = []string{} // allow-once means one round
	if res.SessionID != "" {
		r.SessionID = res.SessionID
	}
	rec := store.RoundRecord{N: r.Rounds, StartedAt: start.UTC().Format(time.RFC3339), Seconds: secs, Usage: store.Usage(res.Usage),
		CostUSD: res.CostUSD, CostUnknown: !res.CostKnown && r.Adapter == store.AdapterClaude, ExitCode: res.ExitCode, Subtype: res.StopReason}
	if ctx.Err() != nil && !f.explicit.Load() {
		// The hub is stopping: the run goes back to the queue with its lock and resumes on the next start.
		rec.Outcome = "interrupted"
		r.RoundLog = append(r.RoundLog, rec)
		// A plan written during the cut round must make the restart resume it, not re-send the on-ramp.
		rctx, rcancel := context.WithTimeout(context.Background(), shutdownStateRead)
		if st := s.readState(rctx, &r, card, start); st != nil {
			r.State = st.Raw
			s.mirror(card, r, st)
		}
		rcancel()
		r.Status, r.NotBefore = store.StatusQueued, s.stamp()
		if r.Slug != "" && len(r.State) > 0 {
			r.Phase = store.PhaseResume
		}
		s.save(r)
		s.comment(card, r, fmt.Sprintf("The hub stopped during round %d; the run resumes when the hub starts.", r.Rounds))
		return
	}
	if ctx.Err() != nil {
		rec.Outcome = "cancelled"
		r.RoundLog = append(r.RoundLog, rec)
		s.end(r, card, store.StatusCancelled, "cancelled by human")
		s.comment(card, r, "The run was cancelled. The gate state stays as it is; `tq hub run "+card.ID+"` resumes from it.")
		return
	}
	if runErr != nil {
		rec.Outcome = "start-error"
		r.RoundLog = append(r.RoundLog, rec)
		s.fail(r, card, "the adapter could not start", "The adapter could not start: "+runErr.Error())
		return
	}
	if rec.CostUnknown {
		s.comment(card, r, fmt.Sprintf("Round %d reported no cost and no priced usage; the dollar budget could not count it (rounds and minutes still apply).", r.Rounds))
	}
	if res.OverBudget != "" {
		s.stopInsideRound(ctx, r, card, rec, start, res.OverBudget)
		return
	}
	final := strings.TrimSpace(res.Transcript)
	if text := StripRelocate(final); text != "" {
		_, _ = s.st.AddComment(card.ID, store.AuthorAgent, "agent", text, r.ID)
	}
	st := s.readState(ctx, &r, card, start)
	if st != nil {
		r.State = st.Raw
		s.mirror(card, r, st)
	}
	tool := ""
	if len(res.PermissionDenials) > 0 {
		tool = adapter.ToolSpec(res.PermissionDenials[0])
	}
	out := Classify(Observation{ExitCode: res.ExitCode, Subtype: res.StopReason, FinalText: final, State: st,
		PrevStateKey: prevKey, Relocations: r.Relocations, PermissionTool: tool, Retried: r.Retried, PreSlug: r.Slug == ""})
	rec.Outcome = string(out.Verdict)
	r.RoundLog = append(r.RoundLog, rec)
	s.apply(r, card, out, st, tool, final, res.ExitCode)
}

// readState reads the gate state for the run's slug, or finds the slug: the build folder newer than
// the run whose plan task.hub is this card.
func (s *Service) readState(ctx context.Context, r *store.Run, card store.Card, roundStart time.Time) *gate.State {
	dir := s.RunDir(*r)
	if r.Slug != "" {
		st, err := s.cfg.Gate.State(ctx, dir, r.Slug, false)
		if err == nil {
			return st
		}
		if !gate.IsNoPlan(err) {
			s.comment(card, *r, "Could not read the gate state of "+r.Slug+": "+err.Error())
		}
		return nil
	}
	since := roundStart.Add(-time.Minute)
	if t, err := time.Parse(time.RFC3339, r.StartedAt); err == nil && t.Add(-time.Minute).Before(since) {
		since = t.Add(-time.Minute)
	}
	for _, slug := range s.cfg.Gate.Slugs(dir, since) {
		st, err := s.cfg.Gate.State(ctx, dir, slug, false)
		if err == nil && st.Task != nil && st.Task.Hub == card.ID {
			r.Slug = slug
			return st
		}
	}
	return nil
}

// mirror copies the gate's decision records into the store, keyed by the gate's ids.
func (s *Service) mirror(card store.Card, r store.Run, st *gate.State) {
	for _, p := range st.PendingDecisions {
		_, _ = s.st.UpsertDecision(store.Decision{ID: p.ID, CardID: card.ID, RunID: r.ID, Slug: st.Slug, Kind: p.Kind,
			Checkpoint: string(p.Checkpoint), Reviewer: p.Reviewer, Text: p.Text, Options: p.Options, Status: store.DecisionPending, AskedAt: p.AskedAt})
	}
	for _, d := range st.Decisions {
		if d.Status != store.DecisionAnswered {
			continue
		}
		_, _ = s.st.UpsertDecision(store.Decision{ID: d.ID, CardID: card.ID, RunID: r.ID, Slug: st.Slug, Kind: d.Kind,
			Checkpoint: string(d.Checkpoint), Reviewer: d.Reviewer, Options: d.Options, Status: store.DecisionAnswered,
			Choice: d.Choice, Note: d.Note, Actor: d.Actor, AskedAt: d.AskedAt, AnsweredAt: d.AnsweredAt})
	}
}

// apply records the verdict. st is the gate state read after the round (nil when there is no plan
// yet); a requeued run whose plan exists resumes the build instead of re-sending the on-ramp.
func (s *Service) apply(r store.Run, card store.Card, out Outcome, st *gate.State, tool, final string, exit int) {
	if st != nil && r.Slug != "" {
		switch out.Verdict {
		case VerdictMaxTurns, VerdictRetry, VerdictPermission, VerdictUsageLimit:
			r.Phase = store.PhaseResume
		}
	}
	switch out.Verdict {
	case VerdictRelocate:
		if !s.relocateAllowed(r, out.Relocate.Worktree, out.Relocate.Slug) {
			s.fail(r, card, "relocate outside workspace", fmt.Sprintf("The run asked to move into %s, which is neither inside the workspace root %s nor the build worktree next to the project folder; the hand-off was refused.", out.Relocate.Worktree, s.cfg.Root))
			return
		}
		r.Worktree, r.Slug = out.Relocate.Worktree, out.Relocate.Slug
		r.Relocations = append(r.Relocations, out.Relocate.Slug)
		r.Phase, r.SessionID = store.PhaseRelocated, "" // a session belongs to its folder
		if st != nil && st.Slug == out.Relocate.Slug {
			r.Phase = store.PhaseResume
		}
		r.Status, r.NotBefore = store.StatusQueued, s.stamp()
		s.save(r)
		s.releaseProjectLock(r) // the project folder is free for the next card's first round
		s.comment(card, r, "The build moved into its worktree "+r.Worktree+" ("+r.Slug+"); the next round starts there.")
	case VerdictRelocateLoop:
		s.fail(r, card, out.StopReason, "The run asked to move into the worktree of "+out.Relocate.Slug+" a second time and was stopped (relocate loop). Check that the session in the worktree has the gate hooks active (build-gate ping).")
	case VerdictRolledBack:
		s.fail(r, card, out.StopReason, fmt.Sprintf("The gate state of %s was rolled back or altered. Re-approve in your own terminal (build-gate approve %s); the hub answers nothing for this build until then.", r.Slug, r.Slug))
	case VerdictHalted:
		where := ""
		if st := decodeState(r.State); st != nil && st.Halted != nil && st.Halted.Checkpoint != "" {
			where = " at checkpoint " + string(st.Halted.Checkpoint)
		}
		s.fail(r, card, out.StopReason, "The build halted"+where+": "+out.StopReason+". The last findings are in the plan; fix it, then `tq hub run "+card.ID+"` resumes.")
	case VerdictWaiting:
		r.Status = store.StatusWaiting
		s.save(r)
		n, result := 0, false
		if st := decodeState(r.State); st != nil {
			n = len(st.PendingDecisions)
			for _, p := range st.PendingDecisions {
				result = result || p.Kind == store.KindResult
			}
		}
		_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunWait, CardID: card.ID, RunID: r.ID, Data: map[string]any{"decisions": n}})
		if result {
			s.moveToKind(card, store.LaneReview)
		}
		s.notify("tq hub: waiting on you", card.Title)
	case VerdictFinished:
		s.end(r, card, store.StatusFinished, "")
		s.moveToKind(card, store.LaneReview)
	case VerdictPermission:
		_, _ = s.st.AskHub(card.ID, r.ID, r.Slug, "The run was refused the tool "+tool+". Allow it for one round, or stop?",
			[]string{"allow-once", "stop"}, map[string]any{"tool": tool})
		r.Status = store.StatusWaiting
		s.save(r)
		s.notify("tq hub: permission needed", card.Title+": "+tool)
	case VerdictUsageLimit:
		cost := 0.0
		if n := len(r.RoundLog); n > 0 {
			cost = r.RoundLog[n-1].CostUSD
		}
		s.hold(r, card, out.StopReason, "The agent hit a usage limit (out of usage credits, or a usage or rate limit). Resume when credits are available again (the session continues where it stopped), or stop?",
			fmt.Sprintf("Round %d ended on a usage limit; its cost ($%.2f) is kept and the run is not failed.", r.Rounds, cost))
	case VerdictQuestion:
		// The run stopped on a question before any build slug existed: the hub asks it, and the
		// human's answer (a card comment) reaches the same run's next prompt.
		if _, err := s.st.AskHubQuestion(card.ID, r.ID, tailText(final, questionMax)); err != nil {
			// never wait with nothing to answer
			s.fail(r, card, "question not recorded", "The hub could not record the run's question ("+err.Error()+"), so the run cannot wait on you; it was failed. The question is the agent's comment above; answer it with a comment, then `tq hub run "+card.ID+"`.")
			return
		}
		r.Status, r.StopReason = store.StatusWaiting, out.StopReason
		s.save(r)
		_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunWait, CardID: card.ID, RunID: r.ID, Data: map[string]any{"question": true}})
		s.notify("tq hub: waiting on you", card.Title+": the run asked a question")
	case VerdictMaxTurns:
		r.Status, r.NotBefore = store.StatusQueued, s.stamp()
		s.save(r)
	case VerdictRetry:
		if r.Budget.Rounds > 0 && r.Rounds >= r.Budget.Rounds {
			s.fail(r, card, fmt.Sprintf("exit %d", exit), fmt.Sprintf("The run failed (exit %d) and has no rounds left.\n\nLast log lines:\n%s", exit, tailLines(r.LogPath, 20)))
			return
		}
		r.Retried++
		r.Status, r.NotBefore = store.StatusQueued, s.cfg.Now().Add(s.cfg.RetryDelay).UTC().Format(time.RFC3339)
		s.save(r)
		s.comment(card, r, fmt.Sprintf("Round %d exited with code %d and the gate state did not change; retrying once in %s.\n\nLast log lines:\n%s",
			r.Rounds, exit, s.cfg.RetryDelay, tailLines(r.LogPath, 20)))
	case VerdictExitFailed:
		s.fail(r, card, out.StopReason, fmt.Sprintf("The run failed again (exit %d).\n\nLast log lines:\n%s", exit, tailLines(r.LogPath, 20)))
	default:
		s.fail(r, card, out.StopReason, "The run ended without a stop.\n\n"+tailText(final, 2000))
	}
}

// questionMax bounds a hub question's text; the question is at the end of the final message.
const questionMax = 4000

func (s *Service) overBudget(r store.Run, card store.Card, limit string) {
	b := r.Budget
	s.end(r, card, store.StatusOverBudget, "budget: "+limit)
	_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbBudgetStop, CardID: card.ID, RunID: r.ID, Data: map[string]any{"limit": limit}})
	s.comment(card, r, fmt.Sprintf("Budget reached (%s): tokens %d of %d, $%.2f of $%.2f, %.0f of %.0f minutes, %d of %d rounds. No further round was started.",
		limit, r.Usage.Tokens(), b.Tokens, r.CostUSD, b.USD, r.Seconds/60, b.Minutes, r.Rounds, b.Rounds))
	_, _ = s.st.AskHub(card.ID, r.ID, r.Slug, fmt.Sprintf("The run stopped at its %s budget. Raise the budget and resume (a note like tokens=4000000,usd=20 sets the new limits; an empty note repeats the same budget), or stop?", limit),
		[]string{"raise-and-resume", "stop"}, map[string]any{"limit": limit, "budget": b.String()})
}

func (s *Service) end(r store.Run, card store.Card, status, reason string) {
	if cur, err := s.st.Run(r.ID); err == nil && !cur.Active() {
		return // already ended (cancel, shutdown race or a second path): never end or release twice
	}
	r.Status, r.StopReason, r.EndedAt = status, reason, s.stamp()
	s.save(r)
	s.releaseProjectLock(r)
	if r.CardID != "" {
		_ = s.st.Release(r.CardID, r.ID)
	}
	_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunEnd, CardID: r.CardID, RunID: r.ID,
		Data: map[string]any{"status": status, "cost_usd": r.CostUSD, "tokens": r.Usage.Tokens(), "rounds": r.Rounds, "kind": r.Kind}})
	if status == store.StatusFailed || status == store.StatusOverBudget {
		s.notify("tq hub: run "+status, card.Title+": "+reason)
	}
}

func (s *Service) fail(r store.Run, card store.Card, reason, comment string) {
	s.end(r, card, store.StatusFailed, reason)
	if comment != "" {
		s.comment(card, r, comment)
	}
}

func (s *Service) comment(card store.Card, r store.Run, text string) {
	if card.ID != "" {
		_, _ = s.st.AddComment(card.ID, store.AuthorSystem, "hub", text, r.ID)
	}
}

func (s *Service) save(r store.Run) { _ = s.st.SaveRun(r) }

func (s *Service) notify(title, body string) {
	if s.cfg.Notify != nil && s.st.Notify() {
		_ = s.cfg.Notify(context.Background(), s.st.Mask(title), s.st.Mask(body))
	}
}

// moveToKind moves the card to the first lane of kind (a system move: it triggers nothing).
func (s *Service) moveToKind(card store.Card, kind string) {
	lane, err := s.st.FirstLane(kind)
	if err != nil {
		return
	}
	if cur, err := s.st.Card(card.ID); err == nil {
		if l, err := s.st.Lane(cur.Lane); err == nil && l.Kind == kind {
			return
		}
	}
	_, _ = s.st.MoveCard(card.ID, lane.Name, "hub")
}

// relocateAllowed accepts a hand-off target inside the workspace root, or the build skill's
// sibling worktree of the project folder: ../<project basename>-build-<slug>.
func (s *Service) relocateAllowed(r store.Run, worktree, slug string) bool {
	wt := filepath.Clean(worktree)
	if pathWithin(filepath.Clean(s.cfg.Root), wt) {
		return true
	}
	r.Worktree = ""
	proj := filepath.Clean(s.RunDir(r))
	return samePath(filepath.Dir(wt), filepath.Dir(proj)) && samePath(filepath.Base(wt), filepath.Base(proj)+"-build-"+slug)
}

func foldPath(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool { return foldPath(a) == foldPath(b) }

// pathWithin reports whether p is root or lies below it (both cleaned and absolute).
func pathWithin(root, p string) bool {
	if root == "" || !filepath.IsAbs(root) {
		return false
	}
	rel, err := filepath.Rel(foldPath(root), foldPath(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func tailLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return "(no log)"
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}

func tailText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++ // never cut a character in half
	}
	return "…" + s[i:]
}

// RecoverStale fails runs left "running" by a hub that stopped (no live round in this process),
// releases card locks held by runs that ended or no longer exist, and drops project locks whose
// run ended, relocated or no longer exists.
func (s *Service) RecoverStale() error {
	runs, err := s.st.RunsWithStatus(store.StatusRunning)
	if err != nil {
		return err
	}
	for _, r := range runs {
		s.mu.Lock()
		_, live := s.flights[r.ID]
		s.mu.Unlock()
		if live {
			continue
		}
		card, _ := s.st.Card(r.CardID)
		s.fail(r, card, "the hub stopped during this round", "The hub stopped while this run's round was in progress. The gate state is kept; `tq hub run "+r.CardID+"` resumes it.")
	}
	locks, err := s.st.Locks()
	if err != nil {
		return err
	}
	for _, l := range locks {
		if !slices.Contains(store.ActiveStatuses, l.RunStatus) {
			_ = s.st.ForceRelease(l.CardID)
		}
	}
	return s.recoverProjectLocks()
}

func (s *Service) Start(ctx context.Context) {
	_ = s.RecoverStale()
	_ = s.Tick(ctx)
	t := time.NewTicker(s.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, f := range s.flights {
				f.cancel()
			}
			s.mu.Unlock()
			s.wg.Wait()
			return
		case <-t.C:
			_ = s.Tick(ctx)
		}
	}
}

func (s *Service) Wait() { s.wg.Wait() }

// Drain runs every due run to its next stop, then returns (tq hub serve --drain).
func (s *Service) Drain(ctx context.Context) error {
	_ = s.RecoverStale()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started, err := s.tick(ctx)
		if err != nil {
			return err
		}
		s.Wait()
		if started == 0 {
			return nil // what is due waits for a project lock held by a run that waits on the human
		}
		due, err := s.st.DueRuns(s.cfg.Now(), 1)
		if err != nil {
			return err
		}
		if len(due) == 0 {
			return nil
		}
	}
}

// Cancel stops a run: a live round is terminated (signal, 10 s grace, kill); a queued, waiting
// or stale run is ended. The gate state stays as it is.
func (s *Service) Cancel(ctx context.Context, runID string) (store.Run, error) {
	if IsAgent(ctx) {
		return store.Run{}, ErrHumanOnly
	}
	r, err := s.st.Run(runID)
	if err != nil {
		return r, err
	}
	if !r.Active() {
		return r, fmt.Errorf("run %s is already %s", runID, r.Status)
	}
	// Marking the run under mu means no tick starts it from here on; a live flight is told the
	// cancel is explicit, so its round ends the run as cancelled rather than requeueing it.
	s.mu.Lock()
	f, live := s.flights[runID]
	s.cancelling[runID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancelling, runID)
		s.mu.Unlock()
	}()
	if live {
		f.explicit.Store(true)
		f.cancel()
		select {
		case <-f.done:
		case <-ctx.Done():
			return r, ctx.Err()
		case <-time.After(adapter.DefaultGrace + 15*time.Second):
			return r, fmt.Errorf("run %s did not stop in time", runID)
		}
	}
	s.enqMu.Lock() // a concurrent Enqueue cannot resume the run between this read and the end
	defer s.enqMu.Unlock()
	r, err = s.st.Run(runID)
	if err != nil || !r.Active() {
		return r, err // the live round ended it
	}
	card, _ := s.st.Card(r.CardID)
	reason := "cancelled by human"
	if r.Status == store.StatusRunning && !live {
		reason = "cancelled by human (stale: no hub was running this round)"
	}
	s.end(r, card, store.StatusCancelled, reason)
	s.comment(card, r, "The run was cancelled. The gate state stays as it is; `tq hub run "+r.CardID+"` resumes from it.")
	return s.st.Run(runID)
}
