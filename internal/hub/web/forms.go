package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// formKinds puts the common kinds first in the new-card form.
var formKinds = []string{"feature", "fix", "hotfix", "analysis", "chore", "project"}

// cardURL is a redirect target: ws named a served workspace and id passed store.ValidID.
func cardURL(ws, id string) string { return "/cards/" + id + "?ws=" + url.QueryEscape(ws) }

func flashURL(ws, id, key string) string { return cardURL(ws, id) + "&flash=" + key }

// splitTags reads a comma-separated tag field; the store trims, dedupes and masks.
func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) formAdd(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	parent := strings.TrimSpace(r.FormValue("parent"))
	if parent != "" && !store.ValidID("c", parent) {
		s.fail(w, r, fmt.Errorf("parent card: %w", store.ErrNotFound))
		return
	}
	nc := store.NewCard{Title: r.FormValue("title"), Body: r.FormValue("body"), Kind: r.FormValue("kind"),
		Lane: r.FormValue("lane"), ProjectDir: r.FormValue("project"), ParentID: parent, Tags: splitTags(r.FormValue("tags"))}
	if r.FormValue("agent_ready") == "1" {
		nc.Tags = append(nc.Tags, store.TagAgentReady)
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
	if d, err := svc.Store().Decision(r.PathValue("did")); err == nil && d.PackApproval() != "" {
		if err := s.packAnswerAllowed(r, d.ID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
