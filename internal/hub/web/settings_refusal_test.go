package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestSettingsFormRefusalRerendersTheFormWithTheTypedValue(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{})
	st := e.svcs["acme"].Store()
	for _, typed := range []string{"7,50", "abc", `<b>"x"</b>`} {
		code, body, _ := postForm(t, ts, "/settings?ws=acme", url.Values{"concurrency": {"5"}, "usd": {typed}}, origin(ts))
		if code != http.StatusBadRequest {
			t.Fatalf("%q: %d", typed, code)
		}
		for _, want := range []string{
			`<form class="set" method="post" action="/settings?ws=acme">`, // the form again, not the error page
			`role="alert"`, `Dollars: want a number, such as 15 or 7.50`,
			`id="err-usd"`, `aria-invalid="true" aria-describedby="err-usd"`,
			`name="concurrency" inputmode="numeric" value="5"`, // the other typed values are kept
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%q: page lacks %q", typed, want)
			}
		}
		if esc := template.HTMLEscapeString(typed); !strings.Contains(body, `inputmode="decimal" value="`+esc+`"`) {
			t.Errorf("%q: typed value not echoed escaped", typed)
		}
		if strings.Contains(body, "<b>") {
			t.Errorf("%q: raw markup reached the page", typed)
		}
		assertCSPSafe(t, "/settings", body)
	}
	if st.Concurrency() != store.DefaultConcurrency {
		t.Fatal("a refused form writes nothing")
	}
	// A valid submit is unchanged: redirect with the flash.
	code, _, hdr := postForm(t, ts, "/settings?ws=acme", url.Values{"usd": {"7.50"}}, origin(ts))
	if code != http.StatusSeeOther || hdr.Get("Location") != "/settings?ws=acme&flash=settings-saved" {
		t.Fatalf("%d %v", code, hdr)
	}
}
