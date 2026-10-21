package web

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// formKinds puts the common kinds first in the new-card form.
var formKinds = []string{"feature", "fix", "hotfix", "analysis", "chore", "project"}

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
	p.Data = data
	s.render(w, r, "board", p)
}

type cardData struct {
	runner.CardDetail
	Lanes       []store.Lane
	Active      *store.Run
	Pending     []store.Decision
	Answered    []store.Decision
	GateURL     string
	ReviewReady bool
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
			data.Pending = append(data.Pending, dec)
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
	if data.Active != nil {
		p.RunID = data.Active.ID
	}
	p.Data = data
	s.render(w, r, "card", p)
}

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
	p := s.basePage(r, "Waiting on you", ws)
	items := s.waitingItems(r, "")
	p.Data = items
	if ws == "" {
		p.WaitingCount = len(items)
	}
	s.render(w, r, "waiting", p)
}

// cardURL is a redirect target: ws named a served workspace and id passed store.ValidID.
func cardURL(ws, id string) string { return "/cards/" + id + "?ws=" + url.QueryEscape(ws) }

func flashURL(ws, id, key string) string { return cardURL(ws, id) + "&flash=" + key }

func (s *Server) formAdd(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	nc := store.NewCard{Title: r.FormValue("title"), Body: r.FormValue("body"), Kind: r.FormValue("kind"),
		Lane: r.FormValue("lane"), ProjectDir: r.FormValue("project")}
	if r.FormValue("agent_ready") == "1" {
		nc.Tags = []string{store.TagAgentReady}
	}
	c, err := svc.Add(s.reqCtx(r), nc, nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, c.ID, "added"))
}

func (s *Server) formMove(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	if _, err := svc.Move(s.reqCtx(r), id, r.FormValue("lane"), r.FormValue("cancel_run") == "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "moved"))
}

func (s *Server) formComment(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	if _, err := svc.Comment(s.reqCtx(r), id, r.FormValue("body")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "commented"))
}

func (s *Server) formRun(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	if _, err := svc.Run(s.reqCtx(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "queued"))
}

func (s *Server) formAnswer(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	out, err := svc.Answer(s.reqCtx(r), id, r.PathValue("did"), r.FormValue("choice"), r.FormValue("note"))
	if err != nil {
		if out.Message != "" {
			// the gate may have recorded the answer before exiting non-zero: show both
			err = fmt.Errorf("%w (the gate said: %s)", err, out.Message)
		}
		s.fail(w, r, err)
		return
	}
	key := "answered"
	if out.AlreadyAnswered {
		key = "already-answered"
	}
	s.redirect(w, r, flashURL(ws, id, key))
}

func (s *Server) formCancel(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "r")
	if !ok {
		return
	}
	run, err := svc.Cancel(s.reqCtx(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !store.ValidID("c", run.CardID) {
		s.redirect(w, r, "/?ws="+url.QueryEscape(ws))
		return
	}
	s.redirect(w, r, flashURL(ws, run.CardID, "cancelled"))
}
