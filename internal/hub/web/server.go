// Package web serves the hub's page and JSON API: embedded templates, plain JavaScript, SSE.
// Every screen answers what is happening, does it need me, what do I do. Every handler takes
// ?ws=<workspace>; the backend decides which workspaces exist and which are available.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

var (
	ErrUnavailable      = errors.New("workspace unavailable")
	ErrUnknownWorkspace = errors.New("unknown workspace")
)

type WorkspaceInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Backend interface {
	Workspaces() []WorkspaceInfo
	// Service returns the runner for an available workspace. It must never open the store of an
	// unavailable one: those return ErrUnavailable.
	Service(name string) (*runner.Service, error)
}

// StaticBackend serves fixed services (tests, and a single workspace).
type StaticBackend struct {
	Infos    []WorkspaceInfo
	Services map[string]*runner.Service
}

func (b StaticBackend) Workspaces() []WorkspaceInfo { return b.Infos }

func (b StaticBackend) Service(name string) (*runner.Service, error) {
	for _, i := range b.Infos {
		if i.Name != name {
			continue
		}
		if !i.Available {
			return nil, fmt.Errorf("%w: %s is %s", ErrUnavailable, name, i.Reason)
		}
		if s := b.Services[name]; s != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownWorkspace, name)
}

func sortInfos(xs []WorkspaceInfo) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}

type Options struct {
	Backend      Backend
	Mode         string        // single | all (shown in /api/ping)
	TokenHash    string        // sha256 hex of the LAN token; "" on a loopback bind
	LocalKey     string        // the CLI's key (serveinfo); "" disables it
	AllowedHosts []string      // Host headers accepted (DNS-rebinding guard); empty gives 421 unless a token is set
	PollEvery    time.Duration // SSE poll for writes by other processes; 0 → 1 s
	Bind         string        // the listening address, shown on the page ("" in tests)
}

type Server struct {
	opts Options
	tpl  *template.Template
	mux  *http.ServeMux
}

// extraRoutes lets later files (review, runs, activity) add their handlers.
var extraRoutes []func(*Server)

type page struct {
	Title        string
	WS           string
	RunID        string
	Mode         string
	Nav          string // the sidebar entry this page belongs to: board, decisions, runs, reviews, activity, connections, settings
	Bind         string
	Running      int // runs running now in WS
	Concurrency  int // WS's concurrency setting; 0 when no workspace resolved
	Workspaces   []WorkspaceInfo
	SwitchPath   string // where a workspace switcher link lands: this page, or the board for a card or run
	WaitingCount int
	Flash        string
	NewCard      *newCardForm // the dialog's choices; nil when no workspace resolved
	Data         any
}

// flashes is the only text a ?flash= key can put on a page: free text never travels in a URL.
var flashes = map[string]string{
	"answered":          "Answer recorded.",
	"already-answered":  "That decision was already answered.",
	"added":             "Card added.",
	"moved":             "Card moved.",
	"commented":         "Comment added.",
	"queued":            "Run queued.",
	"cancelled":         "Run cancelled.",
	"approved":          "Result approved.",
	"changes-requested": "Changes requested.",
	"done":              "Card marked done.",
	"tags-saved":        "Tags saved.",
	"settings-saved":    "Settings saved.",
}

func kTok(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

var funcs = template.FuncMap{
	"md":      func(s string) template.HTML { return RenderMarkdown(store.StripFrontMatter(s)) },
	"money":   func(f float64) string { return fmt.Sprintf("$%.2f", f) },
	"ktok":    kTok,
	"minutes": func(sec float64) string { return fmt.Sprintf("%.0f", sec/60) },
	"when": func(ts string) string {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t.Local().Format("2006-01-02 15:04")
		}
		return ts
	},
	"first": func(s string) string { l, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); return l },
	"join":  strings.Join,
	"deref": func(b *bool) bool { return b != nil && *b },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[fmt.Sprint(kv[i])] = kv[i+1]
		}
		return m
	},
	"pill":          pillClass,
	"statusLabel":   statusLabel,
	"laneTitle":     laneTitle,
	"words":         words,
	"optionLabel":   optionLabel,
	"pct":           pct,
	"clock":         clock,
	"ago":           ago,
	"prnum":         prNum,
	"available":     availableCount,
	"planning":      planningLane,
	"allInterfaces": allInterfaces,
}

// switchPages are the pages that exist in every workspace; card and run ids belong to one.
var switchPages = map[string]bool{"/waiting": true, "/runs": true, "/reviews": true, "/activity": true, "/connections": true, "/settings": true}

func switchPath(path string) string {
	if switchPages[path] {
		return path
	}
	return "/"
}

// allInterfaces: the hub listens on a wildcard address (0.0.0.0, ::, or no host).
func allInterfaces(bind string) bool {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind
	}
	ip := net.ParseIP(host)
	return host == "" || (ip != nil && ip.IsUnspecified())
}

// Templates parses every embedded template once; RenderReview (Task 11) uses it too.
func Templates() (*template.Template, error) {
	return template.New("hub").Funcs(funcs).ParseFS(assets, "templates/*.html")
}

func New(opts Options) (*Server, error) {
	if opts.Backend == nil {
		return nil, errors.New("web: no backend")
	}
	if opts.PollEvery <= 0 {
		opts.PollEvery = time.Second
	}
	tpl, err := Templates()
	if err != nil {
		return nil, err
	}
	s := &Server{opts: opts, tpl: tpl, mux: http.NewServeMux()}
	s.routes()
	for _, add := range extraRoutes {
		add(s)
	}
	return s, nil
}

func (s *Server) handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

func (s *Server) Handler() http.Handler { return s.guard(s.mux) }

func (s *Server) routes() {
	static, _ := fs.Sub(assets, ".")
	s.mux.Handle("GET /static/", http.FileServerFS(static))
	s.handle("GET /login", s.login)
	s.handle("GET /{$}", s.board)
	s.handle("GET /cards/{id}", s.cardPage)
	s.handle("GET /waiting", s.waitingPage)
	s.handle("GET /events", s.events)
	s.handle("POST /cards", s.formAdd)
	s.handle("POST /cards/{id}/move", s.formMove)
	s.handle("POST /cards/{id}/comment", s.formComment)
	s.handle("POST /cards/{id}/run", s.formRun)
	s.handle("POST /cards/{id}/decisions/{did}/answer", s.formAnswer)
	s.handle("POST /runs/{id}/cancel", s.formCancel)
	s.handle("GET /api/ping", s.apiPing)
	s.handle("GET /api/cards", s.apiList)
	s.handle("POST /api/cards", s.apiAdd)
	s.handle("GET /api/cards/{id}", s.apiShow)
	s.handle("POST /api/cards/{id}/move", s.apiMove)
	s.handle("POST /api/cards/{id}/run", s.apiRun)
	s.handle("POST /api/cards/{id}/comments", s.apiComment)
	s.handle("POST /api/cards/{id}/decisions/{did}/answer", s.apiAnswer)
	s.handle("POST /api/runs/{id}/cancel", s.apiCancel)
	s.handle("GET /api/waiting", s.apiWaiting)
}

func isAPI(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }

// reqCtx marks requests from an agent session (the CLI sets X-TQ-Hub-Agent: 1). A browser never
// sends the header, so it is honoured on every route: an agent cannot step around the policy by
// posting to the page's form routes instead of the API.
func (s *Server) reqCtx(r *http.Request) context.Context {
	if r.Header.Get(HeaderAgent) == "1" {
		return runner.WithAgent(r.Context())
	}
	return r.Context()
}

// service resolves ?ws= (default: the first available workspace) without opening an unavailable one.
func (s *Server) service(w http.ResponseWriter, r *http.Request) (*runner.Service, string, bool) {
	ws := r.URL.Query().Get("ws")
	if ws == "" {
		for _, i := range s.opts.Backend.Workspaces() {
			if i.Available {
				ws = i.Name
				break
			}
		}
	}
	if ws == "" {
		s.fail(w, r, fmt.Errorf("%w: this hub serves no trusted workspace", ErrUnknownWorkspace))
		return nil, "", false
	}
	svc, err := s.opts.Backend.Service(ws)
	if err != nil {
		s.fail(w, r, err)
		return nil, ws, false
	}
	return svc, ws, true
}

// pathID is a validated id path value; anything else answers 404 and never reaches a redirect.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request, name, prefix string) (string, bool) {
	id := r.PathValue(name)
	if !store.ValidID(prefix, id) {
		s.fail(w, r, fmt.Errorf("%s: %w", name, store.ErrNotFound))
		return "", false
	}
	return id, true
}

func errStatus(err error) (int, string) {
	var active *runner.RunActiveError
	switch {
	case errors.Is(err, runner.ErrHumanOnly):
		return http.StatusForbidden, "human_only"
	case errors.As(err, &active):
		return http.StatusConflict, "run_active"
	case errors.Is(err, runner.ErrLocked):
		return http.StatusConflict, "locked"
	case errors.Is(err, ErrUnavailable):
		return http.StatusForbidden, "unavailable"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, ErrUnknownWorkspace):
		return http.StatusNotFound, "not_found"
	}
	return http.StatusBadRequest, "bad_request"
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, code := errStatus(err)
	if isAPI(r) {
		body := map[string]any{"error": err.Error(), "code": code}
		var active *runner.RunActiveError
		if errors.As(err, &active) {
			body["run"] = active.Run
		}
		writeJSON(w, status, body)
		return
	}
	if r.Header.Get("X-TQ-Hub-Fetch") == "1" {
		http.Error(w, err.Error(), status)
		return
	}
	ws := r.URL.Query().Get("ws")
	p := s.basePage(r, http.StatusText(status), ws)
	p.Data = err.Error()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = s.tpl.ExecuteTemplate(w, "message", p)
}

func (s *Server) basePage(r *http.Request, title, ws string) page {
	p := page{Title: title, WS: ws, Mode: s.opts.Mode, Bind: s.opts.Bind, Workspaces: s.opts.Backend.Workspaces(),
		SwitchPath: switchPath(r.URL.Path), Flash: flashes[r.URL.Query().Get("flash")]}
	if ws != "" {
		if svc, err := s.opts.Backend.Service(ws); err == nil {
			st := svc.Store()
			if ds, err := st.PendingDecisions(); err == nil {
				p.WaitingCount = len(ds)
			}
			if rs, err := st.RunsWithStatus(store.StatusRunning); err == nil {
				p.Running = len(rs)
			}
			p.Concurrency = st.Concurrency()
			p.NewCard = newCardFormFor(svc)
		}
	}
	return p
}

// render executes name, or only name-main when ?fragment=main (the live refresh).
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Query().Get("fragment") == "main" {
		name += "-main"
	}
	if err := s.tpl.ExecuteTemplate(w, name, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("X-TQ-Hub-Fetch") == "1" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}
