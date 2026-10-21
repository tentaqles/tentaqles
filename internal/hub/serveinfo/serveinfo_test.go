package serveinfo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWriteFindRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ping" && r.Header.Get(HeaderLocal) == "k1" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if err := Write(Info{PID: 1, Addr: "127.0.0.1:" + u.Port(), Port: port, Mode: "single", Workspaces: []string{"acme"}, LocalKey: "k1"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := Find(context.Background(), "acme"); !ok || got.Port != port {
		t.Fatalf("%+v %v", got, ok)
	}
	if _, ok := Find(context.Background(), "globex"); ok {
		t.Fatal("globex is not served")
	}
	if BaseURL(Info{Addr: "0.0.0.0:7470", Port: 7470}) != "http://127.0.0.1:7470" {
		t.Fatal("wildcard binds dial loopback")
	}
	Remove(port)
	if len(ReadAll()) != 0 {
		t.Fatal("removed")
	}
}
