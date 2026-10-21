package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestBoardShowsLanesCardsAndBadges(t *testing.T) {
	e := newEnv(t, "acme")
	svc := e.svcs["acme"]
	c, _ := svc.Add(context.Background(), store.NewCard{Title: "Export invoices", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	code, body, _ := get(t, ts, "/?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	for _, want := range []string{"in progress", "Export invoices", "waiting on you", "st-waiting", `data-card="` + c.ID + `"`, "/static/hub.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("board lacks %q", want)
		}
	}
	_, frag, _ := get(t, ts, "/?ws=acme&fragment=main", nil)
	if strings.Contains(frag, "<html") || !strings.Contains(frag, "Export invoices") {
		t.Fatal("fragment=main renders the main section only")
	}
}

func TestTemplatesEscapeCardText(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "<script>alert(1)</script>", Body: "<b>bold</b>"}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/?ws=acme", "/cards/" + c.ID + "?ws=acme"} {
		_, body, _ := get(t, ts, path, nil)
		if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<b>bold</b>") || !strings.Contains(body, "&lt;script&gt;") {
			t.Fatalf("%s is not escaped", path)
		}
	}
}

func TestCardPageHasAnswerForms(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/cards/"+c.ID+"?ws=acme", nil)
	for _, want := range []string{"Waiting on you", `/cards/` + c.ID + `/decisions/d1a2b3c4/answer?ws=acme`, `value="approve"`, `value="stop"`, "Approve the plan?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("card page lacks %q", want)
		}
	}
	code, _, hdr := postForm(t, ts, "/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", url.Values{"choice": {"approve"}, "note": {"ok"}}, origin(ts))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), c.ID) || len(e.gate.answers) != 1 {
		t.Fatalf("%d %v %v", code, hdr, e.gate.answers)
	}
}

func TestWaitingCombinesWorkspaces(t *testing.T) {
	e := newEnv(t, "acme", "globex")
	a, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "acme card", Tags: []string{store.TagAgentReady}}, nil)
	g, _ := e.svcs["globex"].Add(context.Background(), store.NewCard{Title: "globex card", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", a, store.KindPlanApproval)
	e.svcs["globex"].Store().AskHub(g.ID, "", "", "Raise the budget?", []string{"raise-and-resume", "stop"}, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/waiting", nil)
	for _, want := range []string{"acme card", "globex card", `class="ws-label">acme`, `class="ws-label">globex`, "Raise the budget?"} {
		if !strings.Contains(body, want) {
			t.Fatalf("waiting lacks %q:\n%s", want, body)
		}
	}
}

func TestPostNeedsSameOrigin(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	form := url.Values{"title": {"From the page"}, "kind": {"feature"}}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, nil); code != http.StatusForbidden {
		t.Fatalf("no Origin: %d", code)
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, map[string]string{"Origin": "http://evil.example"}); code != http.StatusForbidden {
		t.Fatalf("cross origin: %d", code)
	}
	code, _, hdr := postForm(t, ts, "/cards?ws=acme", form, origin(ts))
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "/cards/c-") {
		t.Fatalf("same origin: %d %v", code, hdr)
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=acme", form, map[string]string{"Sec-Fetch-Site": "same-origin"}); code != http.StatusSeeOther {
		t.Fatalf("Sec-Fetch-Site same-origin: %d", code)
	}
}

func TestHostHeaderIsChecked(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{AllowedHosts: []string{"127.0.0.1:7470"}})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	req.Host = "evil.example:7470"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("DNS-rebinding Host must be refused: %d", resp.StatusCode)
	}
}

func TestTokenRequiredWhenConfigured(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{TokenHash: HashToken("s3cret"), LocalKey: "lk"})
	if code, _, _ := get(t, ts, "/?ws=acme", nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{HeaderToken: "s3cret"}); code != 200 {
		t.Fatalf("header token: %d", code)
	}
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{"X-TQ-Hub-Local": "lk"}); code != 200 {
		t.Fatalf("local key: %d", code)
	}
	if code, _, _ := get(t, ts, "/login?token=wrong", nil); code != http.StatusUnauthorized {
		t.Fatalf("bad login: %d", code)
	}
	code, _, hdr := get(t, ts, "/login?token=s3cret", nil)
	cookie := hdr.Get("Set-Cookie")
	if code != http.StatusSeeOther || !strings.Contains(cookie, CookieToken+"=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("login %d %q", code, cookie)
	}
	val := strings.SplitN(strings.SplitN(cookie, ";", 2)[0], "=", 2)[1]
	if code, _, _ := get(t, ts, "/?ws=acme", map[string]string{"Cookie": CookieToken + "=" + val}); code != 200 {
		t.Fatalf("cookie: %d", code)
	}
}

func TestUnknownWorkspaceIs404(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	if code, _, _ := get(t, ts, "/?ws=nope", nil); code != http.StatusNotFound {
		t.Fatalf("%d", code)
	}
}

func TestUnavailableWorkspaceIs403AndSkippedByWaiting(t *testing.T) {
	e := newEnv(t, "acme")
	a, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "acme card", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", a, store.KindPlanApproval)
	b := e.backend()
	b.Infos = append(b.Infos, WorkspaceInfo{Name: "zeta", Available: false, Reason: "untrusted"})
	ts := e.server(Options{Backend: b, LocalKey: "lk"})
	for _, path := range []string{"/?ws=zeta", "/cards/c-aaaaaaaaaaaa?ws=zeta", "/waiting?ws=zeta", "/events?ws=zeta", "/api/cards?ws=zeta", "/api/waiting?ws=zeta"} {
		if code, _, _ := get(t, ts, path, map[string]string{"X-TQ-Hub-Local": "lk"}); code != http.StatusForbidden {
			t.Fatalf("%s: %d", path, code)
		}
	}
	if code, _, _ := postForm(t, ts, "/cards?ws=zeta", url.Values{"title": {"x"}}, origin(ts)); code != http.StatusForbidden {
		t.Fatalf("form add: %d", code)
	}
	code, body, _ := get(t, ts, "/waiting", nil)
	if code != 200 || !strings.Contains(body, "acme card") || strings.Contains(body, `class="ws-label">zeta`) {
		t.Fatalf("%d %s", code, body)
	}
}

func TestPagesHaveNoInlineHandlersAndSendCSP(t *testing.T) {
	e := newEnv(t, "acme", "globex")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x"}, nil)
	ts := e.server(Options{})
	for _, path := range []string{"/?ws=acme", "/cards/" + c.ID + "?ws=acme", "/waiting"} {
		_, body, hdr := get(t, ts, path, nil)
		if strings.Contains(body, " onchange=") || strings.Contains(body, " onclick=") || strings.Contains(body, "<script>") {
			t.Fatalf("%s has inline script", path)
		}
		if csp := hdr.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || hdr.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s headers %v", path, hdr)
		}
	}
}

func TestFlashIsAFixedVocabulary(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme&flash=Your+session+expired,+visit+evil.example", nil)
	if strings.Contains(body, "evil.example") {
		t.Fatal("free text from the URL must not render as a flash")
	}
	_, body, _ = get(t, ts, "/?ws=acme&flash=answered", nil)
	if !strings.Contains(body, `class="flash"`) {
		t.Fatal("a known flash key renders")
	}
}

func TestAgentHeaderAppliesToFormRoutes(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "x", Tags: []string{store.TagAgentReady}}, nil)
	e.pendingDecision("acme", c, store.KindPlanApproval)
	ts := e.server(Options{})
	hdr := origin(ts)
	hdr[HeaderAgent] = "1"
	code, _, _ := postForm(t, ts, "/cards/"+c.ID+"/decisions/d1a2b3c4/answer?ws=acme", url.Values{"choice": {"approve"}}, hdr)
	if code != http.StatusForbidden || len(e.gate.answers) != 0 {
		t.Fatalf("%d %v", code, e.gate.answers)
	}
}

func TestFormRoutesRejectBadCardIDs(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	code, _, hdr := postForm(t, ts, "/cards/c-AAAA%0d%0aX/comment?ws=acme", url.Values{"body": {"x"}}, origin(ts))
	if code != http.StatusNotFound || hdr.Get("Location") != "" {
		t.Fatalf("%d %v", code, hdr)
	}
}
