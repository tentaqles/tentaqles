package web

import (
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// newCardForm is what the dialog offers: the kinds, the lanes a card may start in (review and done
// are for finished work), and the project cards a new card can hang under.
type newCardForm struct {
	Kinds   []string
	Lanes   []store.Lane
	Parents []store.Card
}

func newCardFormFor(svc *runner.Service) *newCardForm {
	f := &newCardForm{Kinds: formKinds}
	if lanes, err := svc.Store().Lanes(); err == nil {
		for _, l := range lanes {
			if l.Kind != store.LaneReview && l.Kind != store.LaneDone {
				f.Lanes = append(f.Lanes, l)
			}
		}
	}
	if ps, err := svc.Store().ListCards(store.CardFilter{Kind: "project"}); err == nil {
		f.Parents = ps
	}
	return f
}
