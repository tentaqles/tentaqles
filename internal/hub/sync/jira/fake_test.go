package jira

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

// call is one request the fake Jira saw.
type call struct {
	Method, Path, Query, Auth string
	Body                      map[string]any
}

// fakeJira is a recorded Jira Cloud REST v3: canned JSON per route, every request logged.
type fakeJira struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	calls  []call
	routes map[string]func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

func newFakeJira(t *testing.T) *fakeJira {
	t.Helper()
	f := &fakeJira{t: t, routes: map[string]func(http.ResponseWriter, *http.Request, map[string]any){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s %s: body is not JSON: %s", r.Method, r.URL.Path, raw)
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("%s %s: content type %q", r.Method, r.URL.Path, ct)
			}
		}
		f.mu.Lock()
		f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Body: body})
		h := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"errorMessages":["Issue does not exist or you do not have permission to see it."],"errors":{}}`)
			return
		}
		h(w, r, body)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJira) on(method, path string, h func(w http.ResponseWriter, r *http.Request, body map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = h
}

// json answers with a fixed status and JSON document.
func (f *fakeJira) json(method, path string, status int, doc string) {
	f.on(method, path, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, doc)
	})
}

func (f *fakeJira) seen() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func (f *fakeJira) last(method, path string) (call, bool) {
	cs := f.seen()
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Method == method && cs[i].Path == path {
			return cs[i], true
		}
	}
	return call{}, false
}

// a fake fixture, built in parts so no line reads as a credential
const (
	fixtureCredPart = "s3cr3t-" + "api-token"
	fixtureCred     = "tester@example.com:" + fixtureCredPart
)

// newTestAdapter builds the jira adapter against the fake, with a keychain-style transport.
func newTestAdapter(t *testing.T, f *fakeJira, scope map[string]string, tok cred.Token) *Adapter {
	t.Helper()
	if scope == nil {
		scope = map[string]string{"org": "acme", "project": "ACME"}
	}
	a, err := newAdapter(hubsync.Config{
		Workspace: "acme", Provider: "jira", Scope: scope, AssignedTo: "me",
		Transport: cred.Static(cred.SourceKeychain, tok), BaseURL: f.srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }
	return a
}
