package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tentaqles/tentaqles/internal/setup"
)

// Owned by WS3 (spec E §10.3): the read-only guard view. It shows the
// resolved mode, the category table and the 24h counts, and has no form and
// no POST route: web forms are scriptable from an agent's curl, and the hub
// API has no human proof. The mode changes through `tq mode edit` or the
// desktop app.

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /guard", s.guardPage) })
}

// guardViewFor reads a workspace's guard view (a seam for tests).
var guardViewFor = func(ws string) (setup.GuardView, error) {
	w, err := setup.FindWorkspace(ws)
	if err != nil {
		return setup.GuardView{}, err
	}
	return setup.GuardViewFor(w, time.Now()), nil
}

type guardData struct {
	View   setup.GuardView
	Counts string // "3 confirms and 1 block"
	Error  string
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (s *Server) guardPage(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	p := s.basePage(r, "Guard mode", ws)
	p.Nav = "guard"
	var d guardData
	if v, err := guardViewFor(ws); err != nil {
		d.Error = err.Error()
	} else {
		d.View = v
		d.Counts = countNoun(v.Confirms, "confirm") + " and " + countNoun(v.Blocks, "block")
	}
	p.Data = d
	s.render(w, r, "guard", p)
}
