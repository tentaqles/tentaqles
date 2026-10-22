package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
)

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("GET /settings", s.settingsPage)
		s.handle("POST /settings", s.formSettings)
		s.handle("GET /api/settings", s.apiSettings)
		s.handle("POST /api/settings", s.apiUpdateSettings)
	})
}

type settingsData struct {
	Settings      runner.Settings
	Mode, Bind    string
	TokenRequired bool
	Workspaces    []WorkspaceInfo
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	p := s.basePage(r, "Settings", ws)
	p.Nav = "settings"
	p.Data = settingsData{Settings: svc.Settings(), Mode: s.opts.Mode, Bind: s.opts.Bind, TokenRequired: s.opts.TokenHash != "", Workspaces: s.opts.Backend.Workspaces()}
	s.render(w, r, "settings", p)
}

var numberNoise = strings.NewReplacer(",", "", "_", "", " ", "", "$", "")

// settingsFromForm turns the form's text into typed values; a blank field keeps cur. Nothing the
// form sent is stored as text: the runner validates the numbers and the store re-serialises them.
func settingsFromForm(r *http.Request, cur runner.Settings) (runner.SettingsUpdate, error) {
	var u runner.SettingsUpdate
	field := func(k string) (string, bool) {
		v := numberNoise.Replace(strings.TrimSpace(r.PostFormValue(k)))
		return v, v != ""
	}
	if v, ok := field("concurrency"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return u, fmt.Errorf("concurrency: want a whole number from 1 to 16")
		}
		u.Concurrency = &n
	}
	b, touched := cur.DefaultBudget, false
	if v, ok := field("usd"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return u, fmt.Errorf("dollars: want a number, such as 15 or 7.50")
		}
		b.USD, touched = f, true
	}
	if v, ok := field("tokens"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return u, fmt.Errorf("tokens: want a whole number, such as 2,000,000")
		}
		b.Tokens, touched = n, true
	}
	if v, ok := field("minutes"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return u, fmt.Errorf("minutes: want a number")
		}
		b.Minutes, touched = f, true
	}
	if v, ok := field("rounds"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return u, fmt.Errorf("rounds: want a whole number")
		}
		b.Rounds, touched = n, true
	}
	if touched {
		u.DefaultBudget = &b
	}
	switch strings.TrimSpace(r.PostFormValue("notify")) {
	case "on":
		on := true
		u.Notify = &on
	case "off":
		off := false
		u.Notify = &off
	case "":
	default:
		return u, fmt.Errorf("notifications: use on or off")
	}
	return u, nil
}

func (s *Server) formSettings(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	u, err := settingsFromForm(r, svc.Settings())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := svc.UpdateSettings(s.reqCtx(r), u); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, "/settings?ws="+url.QueryEscape(ws)+"&flash=settings-saved")
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, svc.Settings())
}

func (s *Server) apiUpdateSettings(w http.ResponseWriter, r *http.Request) {
	svc, _, ok := s.service(w, r)
	if !ok {
		return
	}
	var u runner.SettingsUpdate
	if err := decode(r, &u); err != nil {
		s.fail(w, r, err)
		return
	}
	got, err := svc.UpdateSettings(s.reqCtx(r), u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, got)
}
