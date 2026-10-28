package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

func fakeTQ(mode string, args []string) int {
	_ = os.WriteFile(filepath.Join(os.Getenv("TQ_HOME"), "fake-tq-args.txt"), []byte(strings.Join(args, " ")), 0o600)
	switch mode {
	case "fail":
		fmt.Fprintln(os.Stderr, "boom: port 7470 is busy")
		return 3
	case "hang":
		time.Sleep(time.Minute)
		return 0
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
	_ = http.Serve(ln, pingHandler("fake-key"))
	return 0
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
