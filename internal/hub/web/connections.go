package web

import "net/http"

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /connections", s.connectionsPage) })
}

// connectionsPage shows the workspace's company tracker (spec B §10): the auth source and account,
// last sync, last error, next run, Sync now, the lane map and the six write-policy toggles. It
// reads the store only; the tracker is called by Sync now and the engine's poll.
func (s *Server) connectionsPage(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	p := s.basePage(r, "Connections", ws)
	p.Nav = "connections"
	p.Data = s.connectionsData(ws)
	s.render(w, r, "connections", p)
}
