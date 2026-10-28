package setupapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeHub serves /api/ping for requests that carry key, and writes the serve record a real
// `tq hub serve` would write under $TQ_HOME/hub.
func fakeHub(t *testing.T, key string, workspaces ...string) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" || r.Header.Get("X-TQ-Hub-Local") != key {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	_, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	writeServeRecord(t, port, key, workspaces...)
	return port
}

func writeServeRecord(t *testing.T, port int, key string, workspaces ...string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("TQ_HOME"), "hub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"pid": 1, "addr": "127.0.0.1:" + strconv.Itoa(port), "port": port, "mode": "single",
		"workspaces": workspaces, "local_key": key})
	if err := os.WriteFile(filepath.Join(dir, "serve-"+strconv.Itoa(port)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindHub_DiscoversALiveHubForTheWorkspace(t *testing.T) {
	isolateHome(t)
	port := fakeHub(t, "k1", "acme")
	h, ok := FindHub(context.Background(), "acme")
	if !ok {
		t.Fatal("hub not found")
	}
	if h.Port != port || h.URL != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("hub: %+v", h)
	}
	if got := BoardURL(h, "acme"); got != h.URL+"/?ws=acme" {
		t.Fatalf("board url %q", got)
	}
	if _, ok := FindHub(context.Background(), "other"); ok {
		t.Fatal("a hub that does not serve the workspace must not match")
	}
}

func TestFindHub_IgnoresStaleRecordsAndWrongKeys(t *testing.T) {
	isolateHome(t)
	// A record whose process is gone: reserve a port, then free it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stale := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	writeServeRecord(t, stale, "k1", "acme")
	if _, ok := FindHub(context.Background(), "acme"); ok {
		t.Fatal("stale record matched")
	}
	// A live server that does not answer to the recorded key is not a tq hub.
	port := fakeHub(t, "real", "acme")
	writeServeRecord(t, port, "forged", "acme")
	if _, ok := FindHub(context.Background(), "acme"); ok {
		t.Fatal("a ping refused with the recorded key must not match")
	}
	if hubs := LiveHubs(context.Background()); len(hubs) != 0 {
		t.Fatalf("live hubs: %+v", hubs)
	}
}

func TestLiveHubs_ListsEveryAnsweringHub(t *testing.T) {
	isolateHome(t)
	fakeHub(t, "a", "acme")
	fakeHub(t, "b", "beta", "gamma")
	hubs := LiveHubs(context.Background())
	if len(hubs) != 2 {
		t.Fatalf("want 2 hubs, got %+v", hubs)
	}
	for _, h := range hubs {
		if h.Mode != "single" || len(h.Workspaces) == 0 {
			t.Fatalf("hub fields: %+v", h)
		}
	}
}
