package web

import "net/http"

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /connections", s.connectionsPage) })
}

// connectionsPage is static until board sync ships (release 2 part B): it explains what is coming
// and connects nothing.
func (s *Server) connectionsPage(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	p := s.basePage(r, "Connections", ws)
	p.Nav = "connections"
	s.render(w, r, "connections", p)
}
