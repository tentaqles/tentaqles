package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// TestEveryPageHasTheShellAndIsCSPSafe walks every GET page: the shell is there, exactly one nav
// entry is current, no inline script or handler slipped in, and ?fragment=main renders only #main.
func TestEveryPageHasTheShellAndIsCSPSafe(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Sweep", Tags: []string{store.TagAgentReady}}, nil)
	r, _ := e.pendingDecision("acme", c, store.KindResult)
	svc.Comment(context.Background(), c.ID, "a comment")
	ts := e.server(Options{Bind: "127.0.0.1:7470"})
	pages := []string{
		"/?ws=acme", "/cards/" + c.ID + "?ws=acme", "/cards/" + c.ID + "/review?ws=acme", "/waiting?ws=acme",
		"/runs?ws=acme", "/runs/" + r.ID + "?ws=acme", "/reviews?ws=acme", "/activity?ws=acme",
		"/activity?ws=acme&kind=runs&days=7", "/connections?ws=acme", "/settings?ws=acme",
	}
	for _, path := range pages {
		code, body, hdr := get(t, ts, path, nil)
		if code != 200 {
			t.Errorf("%s: %d", path, code)
			continue
		}
		for _, want := range []string{`<aside class="side">`, `id="themebtn"`, `<symbol id="i-sun"`, `id="new-card"`, `<script src="/static/hub.js"></script>`, `id="toast"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
		if n := strings.Count(body, `aria-current="page"`); n != 1 {
			t.Errorf("%s: %d current nav entries", path, n)
		}
		if !strings.Contains(hdr.Get("Content-Security-Policy"), "script-src 'self'") {
			t.Errorf("%s: CSP %q", path, hdr.Get("Content-Security-Policy"))
		}
		assertCSPSafe(t, path, body)
		sep := "&"
		if !strings.Contains(path, "?") {
			sep = "?"
		}
		fcode, frag, _ := get(t, ts, path+sep+"fragment=main", nil)
		// The fragment is #main's content (hub.js swaps it into the page's own <div id="main">).
		if fcode != 200 || strings.TrimSpace(frag) == "" || !strings.Contains(body, `<div id="main">`) {
			t.Errorf("%s: fragment=main: %d, want 200 and a non-empty body for the page's #main", path, fcode)
		}
		if strings.Contains(frag, `class="side"`) || strings.Contains(frag, "<html") || strings.Contains(frag, `id="new-card"`) {
			t.Errorf("%s: fragment=main carries more than #main", path)
		}
	}
	_, all, _ := get(t, ts, "/waiting", nil)
	assertCSPSafe(t, "/waiting", all)
}

// TestNewRoutesRefuseAnUnavailableWorkspace: every route this redesign added answers 403 for a
// listed but unavailable workspace and never opens it.
func TestNewRoutesRefuseAnUnavailableWorkspace(t *testing.T) {
	e := newEnv(t, "acme")
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	local := map[string]string{"X-TQ-Hub-Local": "lk"}
	for _, path := range []string{"/runs?ws=zeta", "/reviews?ws=zeta", "/connections?ws=zeta", "/settings?ws=zeta",
		"/cards/c-aaaaaaaaaaaa/tile?ws=zeta", "/api/settings?ws=zeta", "/activity?ws=zeta&days=7"} {
		if code, _, _ := get(t, ts, path, local); code != http.StatusForbidden {
			t.Errorf("GET %s: %d", path, code)
		}
	}
	for _, path := range []string{"/settings?ws=zeta", "/cards/c-aaaaaaaaaaaa/tags?ws=zeta"} {
		if code, _, _ := postForm(t, ts, path, url.Values{"concurrency": {"3"}, "tags": {"x"}}, origin(ts)); code != http.StatusForbidden {
			t.Errorf("POST %s: %d", path, code)
		}
	}
	for _, path := range []string{"/api/settings?ws=zeta", "/api/cards/c-aaaaaaaaaaaa?ws=zeta"} {
		if code, _, _ := apiDo(t, ts.URL, "POST", path, map[string]any{"concurrency": 3, "tags": []string{"x"}}, nil); code != http.StatusForbidden {
			t.Errorf("POST %s: %d", path, code)
		}
	}
}

// TestNewWritesNeedSameOrigin: the new form routes sit behind the same guard as the old ones.
func TestNewWritesNeedSameOrigin(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/settings?ws=acme", "/cards/" + c.ID + "/tags?ws=acme"} {
		if code, _, _ := postForm(t, ts, path, url.Values{"concurrency": {"3"}, "tags": {"x"}}, map[string]string{"Origin": "http://evil.example"}); code != http.StatusForbidden {
			t.Errorf("cross-origin %s: %d", path, code)
		}
	}
	if e.svcs["acme"].Store().Concurrency() != store.DefaultConcurrency {
		t.Fatal("a cross-origin post changed a setting")
	}
}
