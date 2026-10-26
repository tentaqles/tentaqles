package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("POST /cards/{id}/tags", s.formTags)
		s.handle("POST /api/cards/{id}", s.apiUpdate)
	})
}

type cardData struct {
	runner.CardDetail
	Lanes        []store.Lane
	LaneKind     string
	Active       *store.Run
	Run          *store.Run // the facts panel: the active run, else the newest
	PendingItems []decisionItem
	Answered     []store.Decision
	GateURL      string
	ReviewReady  bool
	Timeline     []tlItem
	LogTail      string
	LogSize      int64
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
	lanes, err := svc.Store().Lanes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := cardData{CardDetail: d, Lanes: lanes, GateURL: svc.GateURL(), Timeline: timeline(d.Runs, d.Decisions)}
	for _, l := range lanes {
		if strings.EqualFold(l.Name, d.Lane) {
			data.LaneKind = l.Kind
		}
	}
	for i := range d.Runs {
		if d.Runs[i].Active() {
			run := d.Runs[i]
			data.Active = &run
			break
		}
	}
	if data.Active != nil {
		data.Run = data.Active
		data.LogTail, data.LogSize = tailFile(data.Active.LogPath, 16<<10)
	} else if len(d.Runs) > 0 {
		run := d.Runs[0]
		data.Run = &run
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

// formTags replaces the card's tags with the form's list plus anything typed into "add". The
// runner decides whether the caller may (an agent only on a card in a planning lane).
func (s *Server) formTags(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	tags := append(splitTags(r.FormValue("tags")), splitTags(r.FormValue("add"))...)
	if _, err := svc.UpdateCard(s.reqCtx(r), id, store.CardUpdate{Tags: &tags}); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "tags-saved"))
}

type updateRequest struct {
	Title      *string   `json:"title,omitempty"`
	Body       *string   `json:"body,omitempty"`
	ProjectDir *string   `json:"project_dir,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
}

func (s *Server) apiUpdate(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	var req updateRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, errors.Join(errors.New("the body must be a JSON object"), err))
		return
	}
	c, err := svc.UpdateCard(s.reqCtx(r), id, store.CardUpdate{Title: req.Title, Body: req.Body, ProjectDir: req.ProjectDir, Tags: req.Tags})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}
