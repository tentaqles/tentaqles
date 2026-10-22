package web

import (
	"net/http"
	"strconv"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /activity", s.activityPage) })
}

type activityData struct {
	Entries []activity.Entry
}

func (s *Server) activityPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	es, err := svc.Store().Activity(limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.basePage(r, "Activity", ws)
	p.Nav = "activity"
	p.Data = activityData{Entries: es}
	s.render(w, r, "activity", p)
}
