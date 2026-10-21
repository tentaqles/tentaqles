package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type webGate struct {
	mu      sync.Mutex
	states  map[string]*gate.State
	answers []string
}

func (g *webGate) State(_ context.Context, _, slug string, _ bool) (*gate.State, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.states[slug]
	if !ok {
		return nil, &gate.GateError{Code: 1, Msg: "no plan at docs/checkpoints/" + slug + ".json"}
	}
	cp := *st
	cp.Raw, _ = json.Marshal(st)
	return &cp, nil
}

func (g *webGate) Answer(_ context.Context, _, slug, id, choice, note, actor string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answers = append(g.answers, slug+" "+id+" "+choice+" "+actor)
	st := g.states[slug]
	var keep []gate.Decision
	found := false
	for _, p := range st.PendingDecisions {
		if p.ID == id {
			found = true
			st.Decisions = append(st.Decisions, gate.Decision{ID: id, Kind: p.Kind, Status: "answered", Choice: choice, Note: note, Actor: actor, AnsweredAt: "2026-10-21T12:00:00+00:00"})
			st.Finished = st.Finished || (p.Kind == "result" && choice == "approve")
			continue
		}
		keep = append(keep, p)
	}
	st.PendingDecisions = keep
	if !found {
		return "", &gate.GateError{Code: 1, Msg: "no pending decision '" + id + "'"}
	}
	return "decision " + id + ": " + choice + " by " + actor, nil
}

func (g *webGate) Slugs(string, time.Time) []string { return nil }

type nopAdapter struct{}

func (nopAdapter) Start(context.Context, adapter.RunRequest, io.Writer) (adapter.RunResult, error) {
	return adapter.RunResult{StopReason: "success", CostKnown: true}, nil
}

type env struct {
	t    *testing.T
	svcs map[string]*runner.Service
	gate *webGate
	root string
}

func newEnv(t *testing.T, workspaces ...string) *env {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		t.Setenv(k, "")
	}
	e := &env{t: t, svcs: map[string]*runner.Service{}, gate: &webGate{states: map[string]*gate.State{}}, root: testutil.TempDir(t)}
	for _, ws := range workspaces {
		st, err := store.Open(ws, []string{"all"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		svc, err := runner.New(runner.Config{Workspace: ws, Root: e.root, Store: st, Gate: e.gate,
			Adapters: map[string]adapter.Adapter{store.AdapterClaude: nopAdapter{}}, Actor: "hub:tester"})
		if err != nil {
			t.Fatal(err)
		}
		e.svcs[ws] = svc
	}
	return e
}

func (e *env) backend() StaticBackend {
	b := StaticBackend{Services: e.svcs}
	for ws := range e.svcs {
		b.Infos = append(b.Infos, WorkspaceInfo{Name: ws, Available: true})
	}
	sortInfos(b.Infos)
	return b
}

func (e *env) server(opts Options) *httptest.Server {
	e.t.Helper()
	if opts.Backend == nil {
		opts.Backend = e.backend()
	}
	s, err := New(opts)
	if err != nil {
		e.t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	e.t.Cleanup(ts.Close)
	return ts
}

// pendingDecision puts a gate decision on a card the way a round would have mirrored it.
func (e *env) pendingDecision(ws string, c store.Card, kind string) (store.Run, store.Decision) {
	e.t.Helper()
	svc := e.svcs[ws]
	r, _, err := svc.Enqueue(context.Background(), c.ID, "test")
	if err != nil {
		e.t.Fatal(err)
	}
	opts := []string{"approve", "edit", "stop"}
	if kind == store.KindResult {
		opts = []string{"approve", "request-changes", "stop"}
	}
	st := &gate.State{Slug: "demo", Workflow: "feature", PendingDecisions: []gate.Decision{{ID: "d1a2b3c4", Kind: kind, Text: "Approve the plan?", Options: opts}}}
	e.gate.mu.Lock()
	e.gate.states["demo"] = st
	e.gate.mu.Unlock()
	raw, _ := json.Marshal(st)
	r.Slug, r.Status, r.State = "demo", store.StatusWaiting, raw
	svc.Store().SaveRun(r)
	d := store.Decision{ID: "d1a2b3c4", CardID: c.ID, RunID: r.ID, Slug: "demo", Kind: kind, Text: "Approve the plan?", Options: opts, Status: store.DecisionPending}
	if _, err := svc.Store().UpsertDecision(d); err != nil {
		e.t.Fatal(err)
	}
	return r, d
}

func get(t *testing.T, ts *httptest.Server, path string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func origin(ts *httptest.Server) map[string]string { return map[string]string{"Origin": ts.URL} }
