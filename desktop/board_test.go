package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/pkg/setupapi"
)

// fakeTQEnv makes this test binary act as a fake `tq` when the board starts it as a child:
// "serve" behaves like `tq hub serve` (serve record + /api/ping), "fail" exits with an error,
// "hang" never answers.
const fakeTQEnv = "TQ_DESKTOP_FAKE_TQ"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeTQEnv); mode != "" {
		os.Exit(fakeTQ(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// agentMarkers are the env vars that mark an agent session; the child hub must not inherit them.
var agentMarkers = []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK"}

func fakeTQ(mode string, args []string) int {
	home := os.Getenv("TQ_HOME")
	_ = os.WriteFile(filepath.Join(home, "fake-tq-args.txt"), []byte(strings.Join(args, " ")), 0o600)
	var seen []string
	for _, k := range append([]string{"TQ_HUB_PARENT_STDIN"}, agentMarkers...) {
		if v, ok := os.LookupEnv(k); ok {
			seen = append(seen, k+"="+v)
		}
	}
	_ = os.WriteFile(filepath.Join(home, "fake-tq-env.txt"), []byte(strings.Join(seen, "\n")), 0o600)
	switch mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "boom: port 7470 is busy")
		return 3
	case "hang", "sleep":
		time.Sleep(time.Minute)
		return 0
	case "orphan": // starts a run-like grandchild, then dies on its own
		spawnGrandchild(home)
		return 5
	case "stubborn": // a hub with a live run child that ignores every polite stop
		signal.Ignore(os.Interrupt)
		spawnGrandchild(home)
	}
	var ws string
	port := 0
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--workspace":
			ws = args[i+1]
		case "--port":
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	writeRecord(port, "fake-key", ws)
	if mode == "serve" && os.Getenv("TQ_HUB_PARENT_STDIN") == "1" {
		// like tq hub serve: stdin EOF from the parent is a clean stop (the record goes away)
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			_ = os.Remove(filepath.Join(home, "hub", "serve-"+strconv.Itoa(port)+".json"))
			os.Exit(0)
		}()
	}
	_ = http.Serve(ln, pingHandler("fake-key"))
	return 0
}

// spawnGrandchild starts this binary again as a long sleeper (what a hub run's agent is) and
// writes its PID to $TQ_HOME/grandchild.pid.
func spawnGrandchild(home string) {
	self, _ := os.Executable()
	cmd := exec.Command(self, "-test.run=^$")
	cmd.Env = append(os.Environ(), fakeTQEnv+"=sleep")
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	_ = os.WriteFile(filepath.Join(home, "grandchild.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
}

func grandchildPID(t *testing.T, home string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(home, "grandchild.pid"))
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && perr == nil && pid > 0 {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake hub never started its grandchild")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitGone waits for pid to exit; the cleanup kills it so a failing test leaves nothing behind.
func waitGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
			t.Fatalf("pid %d (the hub's run child) outlived the hub", pid)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func pingHandler(key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" || r.Header.Get("X-TQ-Hub-Local") != key {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// writeRecord writes the serve record `tq hub serve` writes ($TQ_HOME/hub/serve-<port>.json).
func writeRecord(port int, key string, workspaces ...string) {
	dir := filepath.Join(os.Getenv("TQ_HOME"), "hub")
	_ = os.MkdirAll(dir, 0o700)
	raw, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "addr": "127.0.0.1:" + strconv.Itoa(port), "port": port,
		"mode": "single", "workspaces": workspaces, "local_key": key})
	_ = os.WriteFile(filepath.Join(dir, "serve-"+strconv.Itoa(port)+".json"), raw, 0o600)
}

func testHubs(t *testing.T, tq string) *boardHubs {
	t.Helper()
	b := newBoardHubs()
	b.tqPath = func() string { return tq }
	b.wait = 10 * time.Second
	b.poll = 20 * time.Millisecond
	b.grace = 2 * time.Second
	t.Cleanup(b.stopAll)
	return b
}

func selfPath(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenBoard_UsesARunningHub(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(pingHandler("k1"))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	writeRecord(port, "k1", "acme")

	b := testHubs(t, "") // starting a hub would fail: there is no tq
	got, err := b.open(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.Started || got.URL != fmt.Sprintf("http://127.0.0.1:%d/?ws=acme", port) {
		t.Fatalf("target: %+v", got)
	}
}

func TestOpenBoard_StartsTQHubServeAsAChild(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(fakeTQEnv, "serve")
	b := testHubs(t, selfPath(t))

	got, err := b.open(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || !strings.HasPrefix(got.URL, "http://127.0.0.1:") || !strings.HasSuffix(got.URL, "/?ws=acme") {
		t.Fatalf("target: %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(home, "fake-tq-args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	if !strings.HasPrefix(args, "hub serve --workspace acme --port ") {
		t.Fatalf("child args: %q", args)
	}
	if !strings.Contains(got.URL, ":"+strings.TrimPrefix(args, "hub serve --workspace acme --port ")+"/") {
		t.Fatalf("url %q is not on the port the child was given (%q)", got.URL, args)
	}

	// A second open finds the hub this app already started; nothing new is spawned.
	again, err := b.open(context.Background(), "acme")
	if err != nil || again.Started || again.URL != got.URL {
		t.Fatalf("second open: %+v %v", again, err)
	}
	if n := b.running(); n != 1 {
		t.Fatalf("want 1 child, got %d", n)
	}

	b.stopAll()
	if n := b.running(); n != 0 {
		t.Fatalf("children left after stopAll: %d", n)
	}
	if _, ok := setupapi.FindHub(context.Background(), "acme"); ok {
		t.Fatal("the stopped child still answers")
	}
	// The stop was the hub's own clean shutdown (stdin closed), not a kill: it removed its
	// serve record, on every OS.
	recs, _ := filepath.Glob(filepath.Join(home, "hub", "serve-*.json"))
	if len(recs) != 0 {
		t.Fatalf("the hub did not shut down cleanly; records left: %v", recs)
	}
}

// The child hub runs as a human-started hub would: it never inherits the agent markers of a
// terminal the app was launched from, and it is told its stdin is the parent's lifeline.
func TestOpenBoard_ChildEnvDropsAgentMarkers(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(fakeTQEnv, "serve")
	for _, k := range agentMarkers {
		t.Setenv(k, "1")
	}
	b := testHubs(t, selfPath(t))
	if _, err := b.open(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "fake-tq-env.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "TQ_HUB_PARENT_STDIN=1" {
		t.Fatalf("child env markers: %q", got)
	}
}

// A hub whose run child is alive and which ignores every polite stop: closing the app still
// ends the whole tree, so no agent keeps editing a worktree with nothing watching it.
func TestStopAll_EndsTheHubsProcessTree(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(fakeTQEnv, "stubborn")
	b := testHubs(t, selfPath(t))
	b.grace = 300 * time.Millisecond
	if _, err := b.open(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	pid := grandchildPID(t, home)
	if !processAlive(pid) {
		t.Fatal("the grandchild should be running before the stop")
	}
	b.stopAll()
	waitGone(t, pid, 5*time.Second)
}

// View > Setup reloads the app only after the window went to a board; on the wizard it does
// nothing, so unapplied wizard state survives a stray Ctrl+1.
func TestSetupMenu_ReloadsOnlyFromABoard(t *testing.T) {
	isolateHome(t)
	srv := httptest.NewServer(pingHandler("k1"))
	defer srv.Close()
	writeRecord(srv.Listener.Addr().(*net.TCPAddr).Port, "k1", "acme")
	a := NewApp()
	a.board = testHubs(t, "")
	if a.leaveBoard() {
		t.Fatal("on the wizard, View > Setup must not reload")
	}
	if _, err := a.OpenBoard("acme"); err != nil {
		t.Fatal(err)
	}
	if !a.leaveBoard() {
		t.Fatal("on a board, View > Setup reloads the wizard")
	}
	if a.leaveBoard() {
		t.Fatal("back on the wizard: a second press must not reload")
	}
	if _, err := a.OpenBoard("Bad Name"); err == nil || a.leaveBoard() {
		t.Fatal("a failed open does not arm the reload")
	}
}

// A hub that dies on its own leaves no run child behind either.
func TestOpenBoard_AHubThatDiesTakesItsTreeWithIt(t *testing.T) {
	home := isolateHome(t)
	t.Setenv(fakeTQEnv, "orphan")
	b := testHubs(t, selfPath(t))
	if _, err := b.open(context.Background(), "acme"); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("want an exited error, got %v", err)
	}
	waitGone(t, grandchildPID(t, home), 5*time.Second)
}

// The free port is picked, then released, then bound by the child: when something else takes
// it in between, the start is retried once on a fresh port.
func TestOpenBoard_RetriesWhenThePortIsTakenBeforeTheChildBinds(t *testing.T) {
	isolateHome(t)
	t.Setenv(fakeTQEnv, "serve")
	thief, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer thief.Close()
	stolen := thief.Addr().(*net.TCPAddr).Port
	b := testHubs(t, selfPath(t))
	calls := 0
	b.freePort = func() (int, error) {
		calls++
		if calls == 1 {
			return stolen, nil
		}
		return freeLoopbackPort()
	}
	got, err := b.open(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.Contains(got.URL, ":"+strconv.Itoa(stolen)+"/") {
		t.Fatalf("want one retry on a new port; calls=%d url=%s", calls, got.URL)
	}
}

func TestOpenBoard_ReportsAFailedStart(t *testing.T) {
	isolateHome(t)
	t.Setenv(fakeTQEnv, "fail")
	b := testHubs(t, selfPath(t))
	_, err := b.open(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "boom: port 7470 is busy") {
		t.Fatalf("want the child's output in the error, got %v", err)
	}
}

func TestOpenBoard_GivesUpOnAHubThatNeverAnswers(t *testing.T) {
	isolateHome(t)
	t.Setenv(fakeTQEnv, "hang")
	b := testHubs(t, selfPath(t))
	b.wait = 400 * time.Millisecond
	_, err := b.open(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("want a timeout error, got %v", err)
	}
	if n := b.running(); n != 0 {
		t.Fatalf("the hung child should be stopped, %d left", n)
	}
}

func TestOpenBoard_RefusesBadWorkspaceNamesAndMissingTQ(t *testing.T) {
	isolateHome(t)
	t.Setenv(fakeTQEnv, "serve")
	b := testHubs(t, selfPath(t))
	for _, ws := range []string{"", "-x", "--all", "../acme", "Acme", "a b"} {
		if _, err := b.open(context.Background(), ws); err == nil {
			t.Errorf("%q: expected an error", ws)
		}
	}
	if n := b.running(); n != 0 {
		t.Fatalf("a refused name started a child")
	}
	none := testHubs(t, "")
	if _, err := none.open(context.Background(), "acme"); err == nil || !strings.Contains(err.Error(), "tq is not installed") {
		t.Fatalf("want a missing-tq error, got %v", err)
	}
}
