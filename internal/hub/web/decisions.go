package web

import (
	"net/http"
	"sort"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
)

// waitingItems gathers pending decisions across the available workspaces; an unavailable one is
// never opened, so it is skipped.
func (s *Server) waitingItems(r *http.Request, only string) []runner.WaitingItem {
	items := []runner.WaitingItem{}
	for _, info := range s.opts.Backend.Workspaces() {
		if !info.Available || (only != "" && info.Name != only) {
			continue
		}
		svc, err := s.opts.Backend.Service(info.Name)
		if err != nil {
			continue
		}
		got, err := svc.Waiting(s.reqCtx(r))
		if err != nil {
			continue
		}
		items = append(items, got...)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Decision.AskedAt > items[j].Decision.AskedAt })
	return items
}

func (s *Server) waitingPage(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("ws")
	if ws != "" {
		if _, _, ok := s.service(w, r); !ok {
			return
		}
	}
	p := s.basePage(r, "Decisions", ws)
	p.Nav = "decisions"
	items := s.waitingItems(r, "")
	p.Data = items
	if ws == "" {
		p.WaitingCount = len(items)
	}
	s.render(w, r, "waiting", p)
}
