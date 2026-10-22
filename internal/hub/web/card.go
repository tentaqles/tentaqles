package web

import (
	"net/http"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

type cardData struct {
	runner.CardDetail
	Lanes        []store.Lane
	Active       *store.Run
	PendingItems []decisionItem
	Answered     []store.Decision
	GateURL      string
	ReviewReady  bool
}

func (s *Server) cardPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	d, err := svc.Show(s.reqCtx(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	lanes, _ := svc.Store().Lanes()
	data := cardData{CardDetail: d, Lanes: lanes, GateURL: svc.GateURL()}
	for i := range d.Runs {
		if d.Runs[i].Active() {
			run := d.Runs[i]
			data.Active = &run
			break
		}
	}
	for _, dec := range d.Decisions {
		if dec.Status == store.DecisionPending {
			data.PendingItems = append(data.PendingItems, newDecisionItem(svc, ws, d.Card, dec))
		} else {
			data.Answered = append(data.Answered, dec)
		}
	}
	if _, ok := svc.PendingResult(d.ID); ok {
		data.ReviewReady = true
	}
	for _, run := range d.Runs {
		if run.Status == store.StatusFinished {
			data.ReviewReady = true
		}
	}
	p := s.basePage(r, d.Title, ws)
	p.Nav = "board"
	if data.Active != nil {
		p.RunID = data.Active.ID
	}
	p.Data = data
	s.render(w, r, "card", p)
}
