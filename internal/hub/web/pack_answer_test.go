package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// N1(b): a pack approval is answered only from a browser session (its
// cookie, a same-origin form post and the page's CSRF token), never by the
// local key, a token header or the API.
func TestPackApprovalAnswerNeedsTheBrowserSession(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), ".tentaqles"))
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Promote churn"}, nil)
	d, err := svc.Store().AskHub(c.ID, "", "", "ml approval", []string{"approve", "reject"}, map[string]any{
		"pack": "ml", "action": "promote", "subject": "churn@7", "target": "production", "expires_at": "2099-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	ts := e.server(Options{LocalKey: "lk"})
	formPath := "/cards/" + c.ID + "/decisions/" + d.ID + "/answer?ws=acme"
	pending := func() bool { got, _ := svc.Store().Decision(d.ID); return got.Status == store.DecisionPending }

	// The API (the CLI's client) refuses, with or without the local key.
	code, m, _ := apiDo(t, ts.URL, "POST", "/api/cards/"+c.ID+"/decisions/"+d.ID+"/answer?ws=acme", map[string]any{"choice": "approve"}, map[string]string{serveinfo.HeaderLocal: "lk"})
	if code != http.StatusForbidden || !pending() {
		t.Fatalf("api: %d %v", code, m)
	}
	// The local key on the form path refuses too.
	if code, _, _ := postForm(t, ts, formPath, url.Values{"choice": {"approve"}}, map[string]string{serveinfo.HeaderLocal: "lk"}); code != http.StatusForbidden || !pending() {
		t.Fatalf("local key form: %d", code)
	}
	// A same-origin form with no session or no CSRF refuses.
	if code, _, _ := postForm(t, ts, formPath, url.Values{"choice": {"approve"}}, origin(ts)); code != http.StatusForbidden || !pending() {
		t.Fatalf("no session: %d", code)
	}
	// The browser: the page sets the session and renders the token.
	_, body, hdr := get(t, ts, "/waiting?ws=acme", nil)
	var cookie string
	for _, sc := range hdr.Values("Set-Cookie") {
		if m := regexp.MustCompile(CookieSession + `=([0-9a-f]+)`).FindStringSubmatch(sc); m != nil {
			cookie = m[1]
		}
	}
	tok := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
	if cookie == "" || tok == nil {
		t.Fatalf("no session or token on the page (cookie %q)", cookie)
	}
	h := origin(ts)
	h["Cookie"] = CookieSession + "=" + cookie
	if code, _, _ := postForm(t, ts, formPath, url.Values{"choice": {"approve"}, "csrf": {"bad"}}, h); code != http.StatusForbidden || !pending() {
		t.Fatalf("wrong token: %d", code)
	}
	h2 := map[string]string{"Cookie": h["Cookie"]} // no Origin: cross-site
	if code, _, _ := postForm(t, ts, formPath, url.Values{"choice": {"approve"}, "csrf": {tok[1]}}, h2); code != http.StatusForbidden || !pending() {
		t.Fatalf("no origin: %d", code)
	}
	if code, _, _ := postForm(t, ts, formPath, url.Values{"choice": {"approve"}, "csrf": {tok[1]}}, h); code != http.StatusSeeOther || pending() {
		t.Fatalf("the browser form must answer: %d", code)
	}
}
