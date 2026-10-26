package web

import (
	"encoding/json"
	"net/http"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// decodeStrict is decode that rejects unknown fields, so a misspelt key is an error instead of a
// silent no-op.
func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) apiPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "mode": s.opts.Mode, "workspaces": s.opts.Backend.Workspaces()})
}

func (s *Server) apiList(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	views, err := svc.List(s.reqCtx(r), store.CardFilter{Lane: q.Get("lane"), Kind: q.Get("kind"), Tag: q.Get("tag")}, q.Get("waiting") == "1")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, views)
}

type addRequest struct {
	Card   store.NewCard `json:"card"`
	Budget *store.Budget `json:"budget,omitempty"`
}

func (s *Server) apiAdd(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var req addRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := svc.Add(s.reqCtx(r), req.Card, req.Budget)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) apiShow(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
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
	writeJSON(w, 200, d)
}

func (s *Server) apiMove(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	var req struct {
		Lane      string `json:"lane"`
		CancelRun bool   `json:"cancel_run"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := svc.Move(s.reqCtx(r), id, req.Lane, req.CancelRun)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiRun(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	run, err := svc.Run(s.reqCtx(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) apiComment(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := svc.Comment(s.reqCtx(r), id, req.Body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) apiAnswer(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	var req struct {
		Choice string `json:"choice"`
		Note   string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := svc.Answer(s.reqCtx(r), id, r.PathValue("did"), req.Choice, req.Note)
	if err != nil && (out.Message != "" || out.Decision.ID != "") {
		// the gate recorded the answer but exited non-zero: the caller sees both
		status, code := errStatus(err)
		writeJSON(w, status, map[string]any{"error": err.Error(), "code": code, "outcome": out})
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiCancel(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
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
	writeJSON(w, 200, run)
}

func (s *Server) apiWaiting(w http.ResponseWriter, r *http.Request) {
	ws := r.URL.Query().Get("ws")
	if ws != "" {
		if _, _, ok := s.service(w, r); !ok {
			return
		}
	}
	writeJSON(w, 200, s.waitingItems(r, ws))
}
