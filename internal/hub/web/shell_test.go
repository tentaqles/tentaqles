package web

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

var (
	tagRe  = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9-]*)((?:\s+[^\s=>/]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?)*)\s*/?>`)
	attrRe = regexp.MustCompile(`([^\s=>/]+)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'>]+))?`)
)

// cspProblems lists inline script bodies, on* handler attributes and javascript: URLs. Under
// script-src 'self' none of them would run, so each is a bug that only shows in a browser. It
// parses tags and attribute names, so escaped text such as a card titled "onerror=" passes.
func cspProblems(body string) []string {
	var out []string
	for _, m := range tagRe.FindAllStringSubmatchIndex(body, -1) {
		name := strings.ToLower(body[m[2]:m[3]])
		hasSrc := false
		if m[4] >= 0 {
			for _, a := range attrRe.FindAllStringSubmatch(body[m[4]:m[5]], -1) {
				attr := strings.ToLower(a[1])
				if strings.HasPrefix(attr, "on") {
					out = append(out, "<"+name+"> carries the inline handler "+attr)
				}
				if attr == "src" {
					hasSrc = true
				}
			}
		}
		if name == "script" && (!hasSrc || !strings.HasPrefix(body[m[1]:], "</script>")) {
			out = append(out, "an inline <script> body")
		}
	}
	if strings.Contains(strings.ToLower(body), "javascript:") {
		out = append(out, "a javascript: URL")
	}
	return out
}

func assertCSPSafe(t *testing.T, path string, body string) {
	t.Helper()
	for _, p := range cspProblems(body) {
		t.Errorf("%s: %s", path, p)
	}
}

// between is s from the first from up to and including the next to ("" when from is missing).
// Later tasks use it to look inside one section of a page.
func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	if j := strings.Index(rest, to); j >= 0 {
		return rest[:j+len(to)]
	}
	return rest
}

func TestShellLandmarks(t *testing.T) {
	e := newEnv(t, "acme")
	e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Export invoices"}, nil)
	ts := e.server(Options{Bind: "127.0.0.1:7470"})
	code, body, _ := get(t, ts, "/?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{
		`<aside class="side">`, `<nav class="nav" aria-label="Pages">`,
		`href="/?ws=acme" class="on" aria-current="page"`, `href="/waiting?ws=acme"`, `href="/runs?ws=acme"`,
		`href="/reviews?ws=acme"`, `href="/activity?ws=acme"`, `href="/connections?ws=acme"`, `href="/settings?ws=acme"`,
		`<symbol id="i-board"`, `<symbol id="i-sun"`, `<symbol id="i-moon"`, `<symbol id="i-grip"`, `<svg class="mark"`,
		`id="themebtn"`, `id="search"`, `href="#new-card"`, `id="toast"`, `id="waiting-count"`, `<div id="main">`,
		`serving <b>127.0.0.1:7470</b>`, `0 of 2 running`, `href="/static/hub.css"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the shell lacks %q", want)
		}
	}
	head, _, _ := strings.Cut(body, "<body")
	if !strings.Contains(head, `<script src="/static/hub.js"></script>`) {
		t.Error("hub.js loads in <head> so the saved theme applies before the first paint")
	}
	assertCSPSafe(t, "/", body)
	_, frag, _ := get(t, ts, "/?ws=acme&fragment=main", nil)
	if strings.Contains(frag, `class="side"`) || strings.Contains(frag, "<html") {
		t.Fatal("fragment=main renders only what sits inside #main")
	}
}

func TestShellSearchOnlyOnTheBoard(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/activity?ws=acme", nil)
	if strings.Contains(body, `id="search"`) || !strings.Contains(body, `href="/activity?ws=acme" class="on" aria-current="page"`) {
		t.Fatal("the filter box belongs to the board; Activity is the current page")
	}
}

func TestShellSwitcherListsUnavailableWorkspaces(t *testing.T) {
	e := newEnv(t, "acme", "globex")
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts := e.server(Options{Backend: b})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range []string{`<details class="ws">`, "3 workspaces, 2 available", `href="/?ws=globex"`, "zeta (unavailable: untrusted)"} {
		if !strings.Contains(body, want) {
			t.Errorf("switcher lacks %q", want)
		}
	}
	if strings.Contains(body, `href="/?ws=zeta"`) {
		t.Error("an unavailable workspace is listed, never linked")
	}
}

func TestCSPHelperCatchesHandlersButNotEscapedText(t *testing.T) {
	if p := cspProblems(`<p data-title="&#34;&gt;&lt;img src=x onerror=alert(1)&gt;">ok</p><script src="/static/hub.js"></script>`); len(p) != 0 {
		t.Fatalf("escaped text is not a handler: %v", p)
	}
	for _, bad := range []string{`<img src=x onerror=alert(1)>`, `<script>alert(1)</script>`, `<a href="javascript:alert(1)">x</a>`, `<svg onload="x()">`} {
		if len(cspProblems(bad)) == 0 {
			t.Errorf("not caught: %s", bad)
		}
	}
}
