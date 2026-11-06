package banner

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func envOf(goos string, vars map[string]string) Env {
	return Env{GOOS: goos, Getenv: func(k string) string { return vars[k] }}
}

func printed(e Env) string {
	var b bytes.Buffer
	Print(&b, Status{Version: "1.2.3", Identity: "acme", Dir: `C:\clients\acme`}, e)
	return b.String()
}

func TestPrint_UTF8(t *testing.T) {
	out := printed(envOf("windows", map[string]string{"NO_COLOR": "1"}))
	if !strings.Contains(out, "████████╗") {
		t.Fatalf("no art:\n%s", out)
	}
	for _, want := range []string{"  tentaqles 1.2.3  ·  identity: acme\n", "  C:\\clients\\acme\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if n := utf8.RuneCountInString(line); n > 77 {
			t.Fatalf("%d columns, 77 at most: %q", n, line)
		}
	}
}

func TestPrint_FallbackIsPureASCII(t *testing.T) {
	out := printed(envOf("linux", map[string]string{"LANG": "C", "NO_COLOR": "1"}))
	for i := 0; i < len(out); i++ {
		if out[i] >= 0x80 {
			t.Fatalf("non-ASCII byte at %d:\n%s", i, out)
		}
	}
	if !strings.Contains(out, "tentaqles 1.2.3 | identity: acme") {
		t.Fatalf("status line:\n%s", out)
	}
}

func TestUTF8(t *testing.T) {
	for _, c := range []struct {
		goos string
		vars map[string]string
		want bool
	}{
		{"windows", nil, true},
		{"linux", nil, false},
		{"darwin", map[string]string{"LANG": "en_US.UTF-8"}, true},
		{"linux", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, false},
		{"windows", map[string]string{"LANG": "C"}, false},
		{"linux", map[string]string{"LC_CTYPE": "pt_BR.utf8"}, true},
	} {
		if got := UTF8(envOf(c.goos, c.vars)); got != c.want {
			t.Errorf("%s %v: %v", c.goos, c.vars, got)
		}
	}
}

func TestColor(t *testing.T) {
	if out := printed(envOf("linux", map[string]string{"LANG": "en_US.UTF-8"})); !strings.Contains(out, "\x1b[36m") || !strings.Contains(out, "\x1b[0m") {
		t.Fatalf("the asset's colour is cyan:\n%q", out)
	}
	if out := printed(envOf("linux", map[string]string{"NO_COLOR": "1"})); strings.Contains(out, "\x1b") {
		t.Fatal("NO_COLOR must print no escape code")
	}
	if out := printed(envOf("windows", nil)); strings.Contains(out, "\x1b") {
		t.Fatal("an old Windows console must get no escape code")
	}
	if out := printed(envOf("windows", map[string]string{"WT_SESSION": "x"})); !strings.Contains(out, "\x1b[36m") {
		t.Fatal("Windows Terminal reads colour")
	}
}

func TestSilent(t *testing.T) {
	get := func(v string) func(string) string { return func(string) string { return v } }
	if Silent(false, get("")) {
		t.Fatal("no flag, no variable: the banner prints")
	}
	if !Silent(true, get("")) || !Silent(false, get("1")) {
		t.Fatal("--no-banner and TQ_NO_BANNER=1 silence it")
	}
}

func TestPrint_ControlCharactersRemoved(t *testing.T) {
	var b bytes.Buffer
	Print(&b, Status{Version: "1", Identity: "a\x1b]0;x\x07b", Dir: "c\nd"}, envOf("linux", map[string]string{"NO_COLOR": "1"}))
	if out := b.String(); strings.Contains(out, "\x07") || strings.Contains(out, "\x1b") || strings.Contains(out, "c\nd") {
		t.Fatalf("control characters reached the terminal: %q", out)
	}
}
