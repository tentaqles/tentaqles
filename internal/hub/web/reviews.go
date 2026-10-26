package web

import (
	"net/http"
	"sort"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /reviews", s.reviewsPage) })
}

type reviewRow struct {
	Card     store.Card
	Run      *store.Run
	Decision *store.Decision // the pending result decision, for the waiting list
}

type reviewsData struct {
	Waiting  []reviewRow // a result decision waits on the human
	Finished []reviewRow // a finished run without a result record, card not done yet
}

func (s *Server) reviewsPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	st := svc.Store()
	ds, err := st.PendingDecisions()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	seen := map[string]bool{}
	d := reviewsData{}
	for i := range ds {
		dec := ds[i]
		if dec.Kind != store.KindResult || seen[dec.CardID] {
			continue
		}
		card, err := st.Card(dec.CardID)
		if err != nil {
			continue
		}
		row := reviewRow{Card: card, Decision: &dec}
		if run, err := st.Run(dec.RunID); err == nil {
			row.Run = &run
		}
		seen[card.ID] = true
		d.Waiting = append(d.Waiting, row)
	}
	runs, err := st.RunsWithStatus(store.StatusFinished)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].EndedAt > runs[j].EndedAt })
	for i := range runs {
		run := runs[i]
		if seen[run.CardID] {
			continue
		}
		card, err := st.Card(run.CardID)
		if err != nil {
			continue
		}
		if lane, err := st.Lane(card.Lane); err != nil || lane.Kind == store.LaneDone {
			continue
		}
		seen[card.ID] = true
		d.Finished = append(d.Finished, reviewRow{Card: card, Run: &run})
	}
	p := s.basePage(r, "Reviews", ws)
	p.Nav = "reviews"
	p.Data = d
	s.render(w, r, "reviews", p)
}
