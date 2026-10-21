package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rawDo sends one request with an explicit Host header (what a DNS-rebinding page produces).
func rawDo(t *testing.T, ts *httptest.Server, method, path, host string, hdr map[string]string, form url.Values) int {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, ts.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if host != "" {
		req.Host = host
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func port(ts *httptest.Server) string {
	u, _ := url.Parse(ts.URL)
	return u.Port()
}

func TestRebindingHostAndOriginAreRefused(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{}) // the harness allow-list: 127.0.0.1, localhost, [::1] on the test port
	p := port(ts)
	evil := "evil.example:" + p
	evilOrigin := map[string]string{"Origin": "http://" + evil}
	form := url.Values{"title": {"rebound"}}
	for _, path := range []string{"/?ws=acme", "/api/cards?ws=acme", "/api/waiting", "/events?ws=acme", "/static/hub.js", "/login"} {
		if code := rawDo(t, ts, http.MethodGet, path, evil, evilOrigin, nil); code != http.StatusMisdirectedRequest {
			t.Fatalf("GET %s with Host %s: %d", path, evil, code)
		}
	}
	if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", evil, evilOrigin, form); code != http.StatusMisdirectedRequest {
		t.Fatalf("POST with rebound Host: %d", code)
	}
	for _, host := range []string{"127.0.0.1:" + p, "LOCALHOST:" + p, "[::1]:" + p} {
		ok := map[string]string{"Origin": "http://" + strings.ToLower(host)}
		if code := rawDo(t, ts, http.MethodGet, "/?ws=acme", host, ok, nil); code != 200 {
			t.Fatalf("GET with Host %s: %d", host, code)
		}
		if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", host, ok, form); code != http.StatusSeeOther {
			t.Fatalf("POST with Host %s: %d", host, code)
		}
	}
}

func TestOriginIsJudgedAgainstTheAllowList(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	p := port(ts)
	host := "127.0.0.1:" + p
	form := url.Values{"title": {"x"}}
	for name, hdr := range map[string]map[string]string{
		"evil origin":          {"Origin": "http://evil.example:" + p},
		"null origin":          {"Origin": "null"},
		"https origin":         {"Origin": "https://" + host},
		"evil referer":         {"Referer": "http://evil.example:" + p + "/"},
		"nothing":              {},
		"sec-fetch cross-site": {"Sec-Fetch-Site": "cross-site"},
	} {
		if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", host, hdr, form); code != http.StatusForbidden {
			t.Fatalf("%s: %d", name, code)
		}
	}
	for name, hdr := range map[string]map[string]string{
		"origin":   {"Origin": "http://localhost:" + p}, // another allowed name for the same hub
		"referer":  {"Referer": "http://" + host + "/cards/x"},
		"sec-site": {"Sec-Fetch-Site": "same-origin"},
	} {
		if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", host, hdr, form); code != http.StatusSeeOther {
			t.Fatalf("%s: %d", name, code)
		}
	}
}

func TestNoAllowListAndNoTokenFailsClosed(t *testing.T) {
	e := newEnv(t, "acme")
	s, err := New(Options{Backend: e.backend(), LocalKey: "lk"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	host := "127.0.0.1:" + port(ts)
	for _, path := range []string{"/", "/api/ping", "/events", "/static/hub.css", "/login"} {
		if code := rawDo(t, ts, http.MethodGet, path, "", map[string]string{"X-TQ-Hub-Local": "lk"}, nil); code != http.StatusMisdirectedRequest {
			t.Fatalf("GET %s: %d", path, code)
		}
	}
	if code := rawDo(t, ts, http.MethodPost, "/cards", host, map[string]string{"Origin": "http://" + host}, url.Values{"title": {"x"}}); code != http.StatusMisdirectedRequest {
		t.Fatalf("POST: %d", code)
	}
}

func TestNoAllowListWithTokenStillNeedsTheToken(t *testing.T) {
	e := newEnv(t, "acme")
	s, err := New(Options{Backend: e.backend(), TokenHash: HashToken("s3cret")})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	evil := "evil.example:" + port(ts)
	// a rebound page reaches the server but cannot carry the cookie scoped to the real host
	if code := rawDo(t, ts, http.MethodGet, "/?ws=acme", evil, nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", evil, map[string]string{"Origin": "http://" + evil}, url.Values{"title": {"x"}}); code != http.StatusUnauthorized {
		t.Fatalf("POST without token: %d", code)
	}
	if code := rawDo(t, ts, http.MethodGet, "/?ws=acme", "", map[string]string{HeaderToken: "s3cret"}, nil); code != 200 {
		t.Fatalf("header token: %d", code)
	}
	// with no allow-list the Origin can never be verified: a cookie-carrying form post is refused,
	// a request carrying the token header (never sent cross-site without a preflight) is not
	if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", "", map[string]string{"Cookie": CookieToken + "=s3cret", "Origin": ts.URL}, url.Values{"title": {"x"}}); code != http.StatusForbidden {
		t.Fatalf("cookie POST without allow-list: %d", code)
	}
	if code := rawDo(t, ts, http.MethodPost, "/cards?ws=acme", "", map[string]string{HeaderToken: "s3cret"}, url.Values{"title": {"x"}}); code != http.StatusSeeOther {
		t.Fatalf("header-token POST: %d", code)
	}
}
