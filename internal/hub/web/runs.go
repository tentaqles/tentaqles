package web

import (
	"io"
	"net/http"
	"os"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) { s.handle("GET /runs/{id}", s.runPage) })
}

type runData struct {
	Run     store.Run
	Card    store.Card
	LogTail string
	LogSize int64
	GateURL string
}

func tailFile(path string, max int64) (string, int64) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0
	}
	start := fi.Size() - max
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", fi.Size()
	}
	b, _ := io.ReadAll(f)
	return string(b), start + int64(len(b))
}

func (s *Server) runPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "r")
	if !ok {
		return
	}
	run, err := svc.Store().Run(id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	card, _ := svc.Store().Card(run.CardID)
	d := runData{Run: run, Card: card, GateURL: svc.GateURL()}
	d.LogTail, d.LogSize = tailFile(run.LogPath, 64<<10)
	p := s.basePage(r, "Run "+run.ID, ws)
	p.Nav = "runs"
	p.RunID = run.ID
	p.Data = d
	s.render(w, r, "run", p)
}
