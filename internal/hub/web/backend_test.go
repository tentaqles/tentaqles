package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/adapter"
	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// fourWorkspaces registers acme, globex and initech (trusted) and evil (untrusted) under one base.
func fourWorkspaces(t *testing.T) (*registry.Config, string) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		t.Setenv(k, "")
	}
	base := filepath.Join(home, "work")
	cfg := &registry.Config{}
	os.MkdirAll(base, 0o755)
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"acme", "globex", "initech", "evil"} {
		root := filepath.Join(base, name)
		os.MkdirAll(root, 0o755)
		mp := filepath.Join(root, manifest.FileName)
		os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: "+name+"\nidentities: { gh: {} }\n"), 0o600)
		if name != "evil" {
			h, _ := trust.HashFile(mp)
			trust.Allow(h)
		}
	}
	return cfg, base
}

func TestServeAllUntrustedNeverOpened(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	entries := DiscoverAll(cfg)
	if len(entries) != 4 {
		t.Fatalf("%+v", entries)
	}
	var opened int32
	g := &webGate{states: map[string]*gate.State{}}
	factory := func(ws *resolve.Workspace) (*runner.Service, error) {
		atomic.AddInt32(&opened, 1)
		if ws.Name == "evil" {
			t.Errorf("the factory was called for the untrusted workspace")
		}
		st, err := store.Open(ws.Name, []string{"all"})
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { st.Close() })
		svc, err := runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Gate: g,
			Adapters: map[string]adapter.Adapter{store.AdapterClaude: nopAdapter{}}, Actor: "hub:tester"})
		if err == nil {
			c, _ := svc.Add(context.Background(), store.NewCard{Title: ws.Name + " card"}, nil)
			st.AskHub(c.ID, "", "", "Raise the budget for "+ws.Name+"?", []string{"raise-and-resume", "stop"}, nil)
		}
		return svc, err
	}
	b := NewMultiBackend(context.Background(), entries, factory)
	defer b.Close()
	var evil WorkspaceInfo
	for _, i := range b.Workspaces() {
		if i.Name == "evil" {
			evil = i
		}
	}
	if evil.Available || !strings.Contains(evil.Reason, "tq allow evil") {
		t.Fatalf("%+v", evil)
	}
	if atomic.LoadInt32(&opened) != 0 {
		t.Fatal("databases open lazily, on first request")
	}
	ts := httptestServer(t, Options{Backend: b, Mode: "all"})
	code, body, _ := get(t, ts, "/waiting", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, ws := range []string{"acme", "globex", "initech"} {
		if !strings.Contains(body, `class="ws-label">`+ws) || !strings.Contains(body, "Raise the budget for "+ws) {
			t.Fatalf("waiting lacks %s:\n%s", ws, body)
		}
	}
	if strings.Contains(body, `class="ws-label">evil`) || !strings.Contains(body, "evil (unavailable: untrusted (run: tq allow evil))") {
		t.Fatal("evil is listed as unavailable in the switcher and never in the waiting list")
	}
	for _, path := range []string{"/?ws=evil", "/cards/c-aaaaaaaaaaaa?ws=evil", "/api/cards?ws=evil"} {
		code, body, _ := get(t, ts, path, nil)
		if code != http.StatusForbidden || !strings.Contains(body, "tq allow evil") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if _, err := os.Stat(store.Dir("evil")); !os.IsNotExist(err) {
		t.Fatal("no hub folder was created for the untrusted workspace")
	}
	if got := b.Opened(); len(got) != 3 {
		t.Fatalf("opened %v", got)
	}
}

// httptestServer serves opts on a loopback port with the loopback host allow-list (the guard
// fails closed without one).
func httptestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	opts.AllowedHosts = loopbackHosts(ts.Listener.Addr().String())
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = s.Handler()
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

func TestDiscoverCwd(t *testing.T) {
	cfg, base := fourWorkspaces(t)
	es, err := DiscoverCwd(filepath.Join(base, "acme"), cfg)
	if err != nil || len(es) != 1 || es[0].Info.Name != "acme" || !es[0].Info.Available {
		t.Fatalf("%+v %v", es, err)
	}
	if _, err := DiscoverCwd(filepath.Join(base, "evil"), cfg); err == nil || !strings.Contains(err.Error(), "tq allow evil") {
		t.Fatalf("untrusted cwd: %v", err)
	}
	if _, err := DiscoverCwd(base, cfg); err == nil {
		t.Fatal("the base folder itself is no workspace")
	}
}

// quickService opens a real store and runner for ws (no adapter does anything).
func quickService(t *testing.T, ws *resolve.Workspace) (*runner.Service, error) {
	st, err := store.Open(ws.Name, []string{"all"})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { st.Close() })
	return runner.New(runner.Config{Workspace: ws.Name, Root: ws.Root, Store: st, Gate: &webGate{states: map[string]*gate.State{}},
		Adapters: map[string]adapter.Adapter{store.AdapterClaude: nopAdapter{}}, Actor: "hub:tester"})
}

// slowAcme blocks the first open of acme until release is closed; entered closes when it starts.
func slowAcme(t *testing.T, calls *int32, entered, release chan struct{}) Factory {
	var once sync.Once
	return func(ws *resolve.Workspace) (*runner.Service, error) {
		if ws.Name == "acme" {
			atomic.AddInt32(calls, 1)
			once.Do(func() { close(entered) })
			<-release
		}
		return quickService(t, ws)
	}
}

type svcErr struct {
	svc *runner.Service
	err error
}

func TestMultiBackendSlowOpenBlocksNoOtherWorkspace(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	var calls int32
	entered, release := make(chan struct{}), make(chan struct{})
	b := NewMultiBackend(context.Background(), DiscoverAll(cfg), slowAcme(t, &calls, entered, release))
	defer b.Close()
	first, second := make(chan svcErr, 1), make(chan svcErr, 1)
	go func() { s, err := b.Service("acme"); first <- svcErr{s, err} }()
	<-entered
	go func() { s, err := b.Service("acme"); second <- svcErr{s, err} }()
	got := make(chan svcErr, 1)
	go func() { s, err := b.Service("globex"); got <- svcErr{s, err} }()
	select {
	case r := <-got:
		if r.err != nil || r.svc == nil {
			t.Fatalf("globex: %v", r.err)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("a slow open of acme blocked globex")
	}
	close(release)
	a1, a2 := <-first, <-second
	if a1.err != nil || a2.err != nil || a1.svc == nil || a1.svc != a2.svc {
		t.Fatalf("both callers get the one acme service: %+v %+v", a1, a2)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("acme was opened %d times", n)
	}
	if got := b.Opened(); len(got) != 2 {
		t.Fatalf("opened %v", got)
	}
}

func TestMultiBackendCloseDuringOpen(t *testing.T) {
	cfg, _ := fourWorkspaces(t)
	var calls int32
	entered, release := make(chan struct{}), make(chan struct{})
	b := NewMultiBackend(context.Background(), DiscoverAll(cfg), slowAcme(t, &calls, entered, release))
	opened := make(chan svcErr, 1)
	go func() { s, err := b.Service("acme"); opened <- svcErr{s, err} }()
	<-entered
	closed := make(chan struct{})
	go func() { b.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned while an open was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	r := <-opened
	if !errors.Is(r.err, ErrUnavailable) || r.svc != nil {
		t.Fatalf("an open that finishes after Close is refused: %+v", r)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	if got := b.Opened(); len(got) != 0 {
		t.Fatalf("opened %v", got)
	}
	if _, err := b.Service("globex"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nothing opens after Close: %v", err)
	}
}
