package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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
