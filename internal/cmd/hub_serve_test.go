package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/hub/web"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// fakeHub answers /api/ping on a loopback port and records itself as a hub serving workspaces.
func fakeHub(t *testing.T, pid int, workspaces ...string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	t.Cleanup(func() { ln.Close() })
	serveinfo.Write(serveinfo.Info{PID: pid, Addr: ln.Addr().String(), Port: port, Mode: "single", Workspaces: workspaces, LocalKey: "k"})
	t.Cleanup(func() { serveinfo.Remove(port) })
	return port
}

func TestServePortBusyNamesTheRunningHub(t *testing.T) {
	hubWS(t)
	port := fakeHub(t, 4242, "globex")
	_, _, _, err := runTQ(t, "hub", "serve", "--port", itoa(port))
	if err == nil {
		t.Fatal("a second hub on the same port must not start")
	}
	for _, want := range []string{"running tq hub", "pid 4242", "globex", "--all", "--port"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message lacks %q: %v", want, err)
		}
	}
	serveinfo.Remove(port)
	_, _, _, err = runTQ(t, "hub", "serve", "--port", itoa(port))
	if err == nil || !strings.Contains(err.Error(), "another program") {
		t.Fatalf("plain port conflict: %v", err)
	}
	if _, err := os.Stat(store.DBPath("acme")); !os.IsNotExist(err) {
		t.Fatal("a hub that could not bind opened no store")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestServeWritesItsRecordAndStops(t *testing.T) {
	hubWS(t)
	port := freePort(t)
	var out lockedBuf
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"hub", "serve", "--port", itoa(port), "--no-notify"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	deadline := time.Now().Add(10 * time.Second)
	var info serveinfo.Info
	var ok bool
	for time.Now().Before(deadline) {
		if info, ok = serveinfo.FindByPort(context.Background(), port); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok || info.Mode != "single" || !info.Serves("acme") {
		cancel()
		t.Fatalf("no live serve record: %+v\n%s", info, out.String())
	}
	if !strings.Contains(out.String(), "serving acme on http://127.0.0.1:") {
		t.Fatal(out.String())
	}
	_, status, _, err := runTQ(t, "hub", "status")
	if err != nil || !strings.Contains(status, "mode single") || !strings.Contains(status, "acme: 0 cards") ||
		!strings.Contains(status, "acme is served by the hub on port "+itoa(port)) {
		cancel()
		t.Fatalf("%v\n%s", err, status)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
	if _, err := os.Stat(serveinfo.Path(port)); !os.IsNotExist(err) {
		t.Fatal("the serve record is removed on exit")
	}
	if strings.Contains(out.String(), "token") {
		t.Fatalf("a loopback bind prints no token:\n%s", out.String())
	}
	_, status, _, _ = runTQ(t, "hub", "status")
	if !strings.Contains(status, "no hub is running") || !strings.Contains(status, "tq hub serve") {
		t.Fatal(status)
	}
}

func TestServeDrainRunsQueuedWorkAndHonoursHubMode(t *testing.T) {
	py := testutil.Python(t)
	fake, _ := filepath.Abs(filepath.Join("..", "hub", "adapter", "testdata", "fake_claude.py")) // before hubWS changes the cwd
	root := hubWS(t)
	dir := t.TempDir()
	script, _ := json.Marshal(map[string]any{"counter": filepath.Join(dir, "n"), "argv_log": filepath.Join(dir, "argv.jsonl"),
		"rounds": []map[string]any{{"result": "I looked around.", "cost": 0.1}}})
	os.WriteFile(filepath.Join(dir, "script.json"), script, 0o600)
	t.Setenv("FAKE_CLAUDE_SCRIPT", filepath.Join(dir, "script.json"))
	argv, _ := json.Marshal(append(py, fake))
	t.Setenv("TQ_HUB_CLAUDE_CMD", string(argv))
	c := addCard(t, "x", "--tag", "agent-ready", "--project", "apps/billing")
	if _, _, _, err := runTQ(t, "hub", "move", c.ID, "in progress"); err != nil {
		t.Fatal(err)
	}
	_, out, errOut, err := runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err != nil || !strings.Contains(out, "drained 1 workspace(s) (mode single)") {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
	st, _ := store.OpenExisting("acme", []string{"all"})
	runs, _ := st.RunsForCard(c.ID)
	st.Close()
	if len(runs) != 1 || runs[0].Status != store.StatusFailed || runs[0].StopReason != "ended without a stop" || runs[0].CostUSD != 0.1 {
		t.Fatalf("%+v", runs)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "argv.jsonl"))
	var call map[string]any
	if err := json.Unmarshal(bytes.SplitN(raw, []byte("\n"), 2)[0], &call); err != nil {
		t.Fatalf("argv log %q: %v", raw, err)
	}
	if cwd, _ := call["cwd"].(string); !strings.EqualFold(filepath.Clean(cwd), filepath.Join(root, "apps", "billing")) {
		t.Fatalf("the round ran in %q, want the card's project folder", cwd)
	}
	if call["headless"] != "1" || call["build_token"] != false {
		t.Fatalf("launch environment %v", call)
	}
	cfg, _ := registry.Load()
	cfg.Hub.Mode = "all"
	cfg.Save()
	_, out, _, err = runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err != nil || !strings.Contains(out, "(mode all)") {
		t.Fatalf("hub.mode all in config.yaml: %v %s", err, out)
	}
}

func TestASecondHubForTheSameWorkspaceIsRefused(t *testing.T) {
	hubWS(t)
	port := fakeHub(t, 4243, "acme")
	_, _, _, err := runTQ(t, "hub", "serve", "--drain", "--no-notify")
	if err == nil || !strings.Contains(err.Error(), "already served by the hub on port "+itoa(port)) {
		t.Fatalf("two hubs must never run one queue: %v", err)
	}
	if _, err := os.Stat(store.DBPath("acme")); !os.IsNotExist(err) {
		t.Fatal("the refused hub opened the store")
	}
}

func TestServedElsewhereAllModeListsUnavailable(t *testing.T) {
	hubWS(t)
	port := fakeHub(t, 4244, "acme")
	in := []web.Entry{{Info: web.WorkspaceInfo{Name: "acme", Available: true}}, {Info: web.WorkspaceInfo{Name: "globex", Available: true}},
		{Info: web.WorkspaceInfo{Name: "evil", Reason: "untrusted (run: tq allow evil)"}}}
	out, err := servedElsewhere(context.Background(), in, "all")
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Info.Available || !strings.Contains(out[0].Info.Reason, "served by the hub on port "+itoa(port)) {
		t.Fatalf("%+v", out[0])
	}
	if !out[1].Info.Available || out[2].Info.Available || out[2].Info.Reason != "untrusted (run: tq allow evil)" {
		t.Fatalf("%+v", out)
	}
	if !in[0].Info.Available {
		t.Fatal("servedElsewhere copies its input")
	}
}

func TestServeTokenAndHosts(t *testing.T) {
	if tok, gen := serveToken("127.0.0.1", ""); tok != "" || gen {
		t.Fatal("loopback needs no token")
	}
	if tok, gen := serveToken("192.168.1.20", ""); len(tok) < 24 || !gen {
		t.Fatalf("a LAN bind generates a token: %q %v", tok, gen)
	}
	if tok, gen := serveToken("0.0.0.0", "mine"); tok != "mine" || gen {
		t.Fatal("--token is kept and not reprinted")
	}
	if !isLoopback("localhost") || !isLoopback("::1") || isLoopback("10.0.0.1") {
		t.Fatal("isLoopback")
	}
	hosts := allowedHosts("127.0.0.1", 7470)
	if len(hosts) == 0 || hosts[0] != "127.0.0.1:7470" {
		t.Fatal(hosts)
	}
	for _, want := range []string{"127.0.0.1:7470", "localhost:7470", "[::1]:7470"} {
		if !slices.Contains(hosts, want) {
			t.Fatalf("a loopback bind allows %s: %v", want, hosts)
		}
	}
	if len(hosts) != 3 {
		t.Fatalf("a loopback bind allows only the loopback aliases: %v", hosts)
	}
	if h80 := allowedHosts("localhost", 80); !slices.Contains(h80, "localhost") || !slices.Contains(h80, "127.0.0.1:80") {
		t.Fatalf("port 80 adds the portless forms: %v", h80)
	}
	// The guard fails closed with an empty list, so a wildcard bind lists every name it answers to.
	wild := allowedHosts("0.0.0.0", 7470)
	if !slices.Contains(wild, "127.0.0.1:7470") || !slices.Contains(wild, "localhost:7470") {
		t.Fatalf("a wildcard bind keeps the loopback aliases: %v", wild)
	}
	if name, err := os.Hostname(); err == nil && name != "" && !slices.Contains(wild, strings.ToLower(name)+":7470") {
		t.Fatalf("a wildcard bind allows the host name: %v", wild)
	}
	if got := allowedHosts("192.168.1.20", 7470); !slices.Contains(got, "192.168.1.20:7470") || !slices.Contains(got, "127.0.0.1:7470") {
		t.Fatal(got)
	}
}

func TestServeAllStoresTheTokenAndListsUntrusted(t *testing.T) {
	root := hubWS(t)
	addCard(t, "existing board") // acme has a hub.db, so --all opens it at start
	evil := filepath.Join(filepath.Dir(root), "evil")
	os.MkdirAll(evil, 0o755)
	os.WriteFile(filepath.Join(evil, manifest.FileName), []byte("schema: tentaqles-client-v2\nclient: evil\nidentities: { gh: {} }\n"), 0o600)
	port := freePort(t)
	var out lockedBuf
	cmd := NewRoot()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"hub", "serve", "--all", "--port", itoa(port), "--token", "test-token-not-secret", "--no-notify"}) // tq:allow-secret
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	}()
	var info serveinfo.Info
	var ok bool
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if info, ok = serveinfo.FindByPort(context.Background(), port); ok {
			break
		}
	}
	if !ok || info.Mode != "all" || !info.Serves("acme") || info.Serves("evil") || len(info.Unavailable) != 1 ||
		!strings.Contains(info.Unavailable[0], "tq allow evil") {
		t.Fatalf("%+v\n%s", info, out.String())
	}
	if strings.Contains(out.String(), "test-token-not-secret") {
		t.Fatal("a token given with --token is never printed")
	}
	st, err := store.OpenExisting("acme", []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	h, _, _ := st.Setting(store.SettingWebTokenHash)
	st.Close()
	if h != web.HashToken("test-token-not-secret") { // tq:allow-secret
		t.Fatalf("web_token_hash %q", h)
	}
	if _, err := os.Stat(store.Dir("evil")); !os.IsNotExist(err) {
		t.Fatal("the untrusted workspace got a hub folder")
	}
}

// stopLog records, in order, when each shutdown step happened.
type stopLog struct {
	mu     sync.Mutex
	events []string
}

func (l *stopLog) add(e string) { l.mu.Lock(); defer l.mu.Unlock(); l.events = append(l.events, e) }
func (l *stopLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func TestRunHTTPStopsInOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(l *stopLog, started chan struct{}) http.HandlerFunc
		want    string
	}{
		{"a request in flight finishes first", func(l *stopLog, started chan struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				close(started)
				time.Sleep(300 * time.Millisecond)
				w.Write([]byte("ok"))
				l.add("request done")
			}
		}, "request done"},
		{"a stream past its grace is closed first", func(l *stopLog, started chan struct{}) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
				l.add("stream done")
			}
		}, "stream done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			l := &stopLog{}
			started := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- runHTTP(ctx, ln, tc.handler(l, started), 200*time.Millisecond,
					func() { l.add("backend closed") }, func() { l.add("record removed") })
			}()
			go func() {
				resp, err := http.Get("http://" + ln.Addr().String() + "/")
				if err == nil { // read until the server ends the response or closes the stream
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("the request never reached the handler")
			}
			stopAt := time.Now()
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "stream done" && time.Since(stopAt) < 200*time.Millisecond {
					t.Fatal("the open stream was not given its grace")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("runHTTP did not return")
			}
			if got := l.get(); !slices.Equal(got, []string{tc.want, "backend closed", "record removed"}) {
				t.Fatalf("stop order %v", got)
			}
		})
	}
}
