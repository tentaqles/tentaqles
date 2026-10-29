package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
)

// A pack approval (spec C §2.11) is answered only from a browser session:
// the session cookie this server set, plus a same-origin form post that
// carries the CSRF token rendered into that session's page. The local key
// (X-TQ-Hub-Local), the header token and the API never answer one: they
// are what an agent's process can reach.

// CookieSession names the browser session cookie.
const CookieSession = "tq_hub_session"

type sessionCtxKey struct{}

type sessions struct {
	once sync.Once
	key  []byte
	ids  sync.Map
}

func (ss *sessions) init() {
	ss.once.Do(func() {
		ss.key = make([]byte, 32)
		_, _ = rand.Read(ss.key)
	})
}

func (s *Server) sess() *sessions {
	s.packSessions.init()
	return &s.packSessions
}

// withSession returns r carrying its browser session id: the valid cookie
// it sent, or (for a page GET) a new session whose cookie is set on w.
func (s *Server) withSession(w http.ResponseWriter, r *http.Request) *http.Request {
	ss := s.sess()
	if c, err := r.Cookie(CookieSession); err == nil {
		if _, ok := ss.ids.Load(c.Value); ok {
			return r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, c.Value))
		}
	}
	if r.Method != http.MethodGet || r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/events") {
		return r
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	ss.ids.Store(id, true)
	http.SetCookie(w, &http.Cookie{Name: CookieSession, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	return r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, id))
}

func sessionOf(r *http.Request) string {
	id, _ := r.Context().Value(sessionCtxKey{}).(string)
	return id
}

// csrfFor is the CSRF token of a pack approval's form in r's session.
func (s *Server) csrfFor(r *http.Request, decisionID string) string {
	sid := sessionOf(r)
	if sid == "" {
		return ""
	}
	m := hmac.New(sha256.New, s.sess().key)
	m.Write([]byte(sid + "\x00" + decisionID))
	return hex.EncodeToString(m.Sum(nil))
}

// errPackAnswerPath refuses a pack-approval answer that is not a browser form.
var errPackAnswerPath = errors.New("a pack approval is answered in the browser (the board's form) or in your own terminal with tq hub answer; the local key, a token header and the API cannot answer it")

// packAnswerAllowed: a browser session cookie this server set (sent as a
// cookie, not created for this request), no local key, no header token,
// same origin, and the form's CSRF token for this decision.
func (s *Server) packAnswerAllowed(r *http.Request, decisionID string) error {
	if r.Header.Get(serveinfo.HeaderLocal) != "" || r.Header.Get(HeaderToken) != "" || r.Header.Get("Authorization") != "" {
		return errPackAnswerPath
	}
	c, err := r.Cookie(CookieSession)
	if err != nil || c.Value != sessionOf(r) {
		return errPackAnswerPath
	}
	if !s.sameOrigin(r) {
		return errPackAnswerPath
	}
	want := s.csrfFor(r, decisionID)
	if want == "" || !eq(r.FormValue("csrf"), want) {
		return errPackAnswerPath
	}
	return nil
}
