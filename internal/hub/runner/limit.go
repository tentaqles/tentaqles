package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// RoundLimit is what is left of the run's budget for its next round: the claude adapter stops the
// round inside it once its spend reaches this, so a round cannot run far past the budget. A limit
// the run has no budget for stays zero (none); one already used up leaves the smallest share, so
// the adapter still stops at once.
func RoundLimit(r store.Run) adapter.Limit {
	b := r.Budget
	var l adapter.Limit
	if b.Tokens > 0 {
		l.Tokens = max(b.Tokens-r.Usage.Tokens(), 1)
	}
	if b.USD > 0 {
		l.USD = max(b.USD-r.CostUSD, 1e-9)
	}
	if b.Minutes > 0 {
		l.Minutes = max(b.Minutes-r.Seconds/60, 1e-9)
	}
	return l
}

// stopInsideRound ends a run whose round the adapter stopped at the budget: the round and what it
// spent are recorded, the gate state read (so a resume finds the build), and the run stops
// over-budget as between rounds. Its session id is kept, so raise-and-resume continues it.
func (s *Service) stopInsideRound(ctx context.Context, r store.Run, card store.Card, rec store.RoundRecord, start time.Time, limit string) {
	rec.Outcome = "over-budget"
	r.RoundLog = append(r.RoundLog, rec)
	if st := s.readState(ctx, &r, card, start); st != nil {
		r.State = st.Raw
		s.mirror(card, r, st)
	}
	s.comment(card, r, fmt.Sprintf("Round %d was stopped inside the round at the run's %s budget (it spent $%.2f and %d tokens before the stop); the session is kept, so raise-and-resume continues it.",
		r.Rounds, limit, rec.CostUSD, rec.Usage.Tokens()))
	s.overBudget(r, card, limit)
}
