package web

import (
	"net/http"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

type laneView struct {
	Lane  store.Lane
	Cards []runner.CardView
}

type boardData struct {
	Lanes []laneView
	Kinds []string
}

func (s *Server) board(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	lanes, err := svc.Store().Lanes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	views, err := svc.List(s.reqCtx(r), store.CardFilter{}, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := boardData{Kinds: formKinds}
	for _, l := range lanes {
		lv := laneView{Lane: l}
		for _, v := range views {
			if strings.EqualFold(v.Lane, l.Name) {
				lv.Cards = append(lv.Cards, v)
			}
		}
		data.Lanes = append(data.Lanes, lv)
	}
	p := s.basePage(r, "Board", ws)
	p.Nav = "board"
	p.Data = data
	s.render(w, r, "board", p)
}
