package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestConnectionsIsAStaticNotYetAvailablePage(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/connections?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d", code)
	}
	main := between(body, `<div id="main">`, `<div class="toast"`)
	for _, want := range []string{"Board sync is not available yet", "release 2, part B", ">Linear<", ">Asana<", ">Azure DevOps<", ">Jira<",
		"Issues from a JQL filter become cards", `<span class="btn sm" aria-disabled="true">Connect</span>`, "<th>Hub lane</th>", "hub → tracker",
		`href="/connections?ws=acme" class="on" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("connections lacks %q", want)
		}
	}
	if n := strings.Count(main, "not connected</span>"); n != 4 {
		t.Errorf("%d trackers marked not connected, want 4", n)
	}
	own := between(main, `<div class="set">`, `<div class="scrim"`)
	if own == "" || strings.Contains(own, "<form") || strings.Contains(own, "<input") || strings.Contains(own, "<select") || strings.Contains(own, "<button") {
		t.Error("the connections page has no controls")
	}
	assertCSPSafe(t, "/connections", body)
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts2 := e.server(Options{Backend: b})
	if code, _, _ := get(t, ts2, "/connections?ws=zeta", nil); code != http.StatusForbidden {
		t.Fatalf("an unavailable workspace: %d", code)
	}
}
