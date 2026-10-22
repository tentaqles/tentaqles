package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestSettingsPageShowsValuesAndRules(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{Bind: "127.0.0.1:7470", Mode: "single"})
	code, body, _ := get(t, ts, "/settings?ws=acme", nil)
	if code != 200 {
		t.Fatalf("%d", code)
	}
	for _, want := range []string{`<form class="set" method="post" action="/settings?ws=acme">`, `name="concurrency" inputmode="numeric" value="2"`,
		`name="usd" inputmode="decimal" value="10"`, `name="tokens" inputmode="numeric" value="2000000"`, `name="minutes" inputmode="numeric" value="120"`,
		`name="rounds" inputmode="numeric" value="12"`, `name="notify" value="on" checked`, `<code class="m">127.0.0.1:7470</code>`, "this workspace",
		"<th>An agent session</th>", "Set a budget, change these settings", "Ctrl+C", `href="/settings?ws=acme" class="on" aria-current="page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("settings lacks %q", want)
		}
	}
	if strings.Contains(body, "/shutdown") || strings.Contains(body, "/stop") || strings.Contains(between(body, `<section class="panel danger-zone">`, "</section>"), "<form") {
		t.Error("there is no remote stop")
	}
	assertCSPSafe(t, "/settings", body)
}

func TestSettingsFormParsesHumanNumbers(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	form := url.Values{"concurrency": {"3"}, "usd": {"$15"}, "tokens": {"3,000,000"}, "minutes": {""}, "rounds": {"8"}, "notify": {"off"}}
	code, _, hdr := postForm(t, ts, "/settings?ws=acme", form, origin(ts))
	if code != http.StatusSeeOther || hdr.Get("Location") != "/settings?ws=acme&flash=settings-saved" {
		t.Fatalf("%d %v", code, hdr)
	}
	st := e.svcs["acme"].Store()
	if st.Concurrency() != 3 || st.DefaultBudget() != (store.Budget{Tokens: 3_000_000, USD: 15, Minutes: 120, Rounds: 8}) || st.Notify() {
		t.Fatalf("%d %+v %v", st.Concurrency(), st.DefaultBudget(), st.Notify())
	}
}

func TestSettingsFormRejectsWithoutPartialWrites(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	st := e.svcs["acme"].Store()
	for _, form := range []url.Values{
		{"concurrency": {"5"}, "usd": {"0"}},
		{"concurrency": {"5"}, "tokens": {"lots"}},
		{"concurrency": {"0"}},
		{"concurrency": {"17"}},
		{"notify": {"maybe"}},
	} {
		if code, _, _ := postForm(t, ts, "/settings?ws=acme", form, origin(ts)); code != http.StatusBadRequest {
			t.Errorf("%v: %d", form, code)
		}
	}
	if st.Concurrency() != store.DefaultConcurrency || st.DefaultBudget() != store.DefaultBudget || !st.Notify() {
		t.Fatal("a refused form writes nothing")
	}
}

func TestSettingsAreHumanOnly(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	hdr := origin(ts)
	hdr[HeaderAgent] = "1"
	if code, _, _ := postForm(t, ts, "/settings?ws=acme", url.Values{"concurrency": {"4"}}, hdr); code != http.StatusForbidden {
		t.Fatalf("form as agent: %d", code)
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/settings?ws=acme", map[string]any{"concurrency": 4}, map[string]string{HeaderAgent: "1"}); code != http.StatusForbidden || m["code"] != "human_only" {
		t.Fatalf("API as agent: %d %v", code, m)
	}
	if e.svcs["acme"].Store().Concurrency() != store.DefaultConcurrency {
		t.Fatal("an agent changed the concurrency")
	}
	code, m, raw := apiDo(t, ts.URL, "POST", "/api/settings?ws=acme", map[string]any{"concurrency": 4, "notify": false}, nil)
	if code != 200 || m["concurrency"] != float64(4) || m["notify"] != false {
		t.Fatalf("%d %s", code, raw)
	}
	if code, m, _ := apiDo(t, ts.URL, "GET", "/api/settings?ws=acme", nil, nil); code != 200 || m["concurrency"] != float64(4) {
		t.Fatalf("%d %v", code, m)
	}
}
