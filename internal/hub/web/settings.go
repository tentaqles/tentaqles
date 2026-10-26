package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
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
	Form          settingsForm      // the text each field shows
	Error         string            // set when a submit was refused: the page is shown again with it
	FieldErrors   map[string]string // input name -> message
}

// settingsForm is the text in each field: the saved values, or what the person typed when a submit
// was refused.
type settingsForm struct {
	Concurrency, USD, Tokens, Minutes, Rounds string
	Notify                                    bool
}

func savedForm(c runner.Settings) settingsForm {
	b := c.DefaultBudget
	return settingsForm{Concurrency: strconv.Itoa(c.Concurrency), USD: fmt.Sprint(b.USD), Tokens: fmt.Sprint(b.Tokens),
		Minutes: fmt.Sprint(b.Minutes), Rounds: fmt.Sprint(b.Rounds), Notify: c.Notify}
}

// fieldError is a refused form value; Field is the input's name.
type fieldError struct{ Field, Msg string }

func (e *fieldError) Error() string { return e.Msg }

func (s *Server) settingsData(svc *runner.Service) settingsData {
	cur := svc.Settings()
	return settingsData{Settings: cur, Mode: s.opts.Mode, Bind: s.opts.Bind, TokenRequired: s.opts.TokenHash != "",
		Workspaces: s.opts.Backend.Workspaces(), Form: savedForm(cur)}
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	p := s.basePage(r, "Settings", ws)
	p.Nav = "settings"
	p.Data = s.settingsData(svc)
	s.render(w, r, "settings", p)
}

// settingsRefused shows the settings page again with status 400, the typed values kept and the
// message next to the field it is about.
func (s *Server) settingsRefused(w http.ResponseWriter, r *http.Request, svc *runner.Service, ws string, err error) {
	if r.Header.Get("X-TQ-Hub-Fetch") == "1" { // the script reads a plain-text refusal
		s.fail(w, r, err)
		return
	}
	d := s.settingsData(svc)
	d.Form = settingsForm{
		Concurrency: r.PostFormValue("concurrency"), USD: r.PostFormValue("usd"), Tokens: r.PostFormValue("tokens"),
		Minutes: r.PostFormValue("minutes"), Rounds: r.PostFormValue("rounds"),
		Notify: strings.TrimSpace(r.PostFormValue("notify")) == "on" || (strings.TrimSpace(r.PostFormValue("notify")) != "off" && d.Settings.Notify),
	}
	d.Error = err.Error()
	var fe *fieldError
	if errors.As(err, &fe) {
		d.FieldErrors = map[string]string{fe.Field: fe.Msg}
	}
	p := s.basePage(r, "Settings", ws)
	p.Nav = "settings"
	p.Data = d
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	if err := s.tpl.ExecuteTemplate(w, "settings", p); err != nil {
		fmt.Fprintln(w, err.Error())
	}
}

var numberNoise = strings.NewReplacer("_", "", " ", "", "$", "")

// thousands is a number whose commas sit only between full groups of three digits ("1,500" or
// "2,000,000.50"); any other comma ("7,50", "1,5") is a typo or a decimal comma, never dropped.
var thousands = regexp.MustCompile(`^\d{1,3}(,\d{3})+(\.\d+)?$`)

// cleanNumber strips the noise a person types around a number; a comma survives (and the
// parse then refuses the value) unless it is a valid thousands separator.
func cleanNumber(s string) string {
	v := numberNoise.Replace(strings.TrimSpace(s))
	if strings.Contains(v, ",") && thousands.MatchString(v) {
		v = strings.ReplaceAll(v, ",", "")
	}
	return v
}

func positive(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && f > 0 }

// settingsFromForm turns the form's text into typed values; a blank field keeps cur. Nothing the
// form sent is stored as text: the runner validates the numbers and the store re-serialises them.
// A refused value comes back as a *fieldError naming its input.
func settingsFromForm(r *http.Request, cur runner.Settings) (runner.SettingsUpdate, error) {
	var u runner.SettingsUpdate
	field := func(k string) (string, bool) {
		v := cleanNumber(r.PostFormValue(k))
		return v, v != ""
	}
	if v, ok := field("concurrency"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxConcurrency {
			return u, &fieldError{"concurrency", "Concurrency: use a whole number from 1 to 16."}
		}
		u.Concurrency = &n
	}
	b, touched := cur.DefaultBudget, false
	if v, ok := field("usd"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || !positive(f) {
			return u, &fieldError{"usd", "Dollars: want a number, such as 15 or 7.50, above zero."}
		}
		b.USD, touched = f, true
	}
	if v, ok := field("tokens"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return u, &fieldError{"tokens", "Tokens: use a whole number above zero, such as 2,000,000."}
		}
		b.Tokens, touched = n, true
	}
	if v, ok := field("minutes"); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || !positive(f) {
			return u, &fieldError{"minutes", "Minutes: use a number above zero."}
		}
		b.Minutes, touched = f, true
	}
	if v, ok := field("rounds"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return u, &fieldError{"rounds", "Rounds: use a whole number above zero."}
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
		return u, &fieldError{"notify", "Notifications: choose on or off."}
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
		s.settingsRefused(w, r, svc, ws, err)
		return
	}
	if _, err := svc.UpdateSettings(s.reqCtx(r), u); err != nil {
		// A value the runner refuses (400) is shown on the form; human-only, not-found and the like
		// keep the plain error page.
		if status, _ := errStatus(err); status == http.StatusBadRequest {
			s.settingsRefused(w, r, svc, ws, err)
			return
		}
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
