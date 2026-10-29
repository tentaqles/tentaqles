package statusline

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func isolate(t *testing.T) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
}

func TestRenderModeSegment(t *testing.T) {
	cases := []struct {
		f      Facts
		plain  string
		colour string
	}{
		{Facts{Client: "acme", Mode: "default"}, "acme · tq: default", dim + "tq: default"},
		{Facts{Client: "acme", Mode: "relaxed", Looser: true, Skipped: 4}, "acme · tq: relaxed · 4 skipped", yellow + "tq: relaxed · 4 skipped"},
		{Facts{Client: "acme", Mode: "custom", Looser: true, Skipped: 1}, "acme · tq: custom · 1 skipped", yellow + "tq: custom · 1 skipped"},
		{Facts{Client: "acme", Mode: "custom"}, "acme · tq: custom", dim + "tq: custom"},
		{Facts{Client: "acme", Mode: "strict"}, "acme · tq: strict", cyan + "tq: strict"},
	}
	for _, c := range cases {
		if got := Render(Input{}, c.f, false); got != c.plain {
			t.Errorf("%+v: %q, want %q", c.f, got, c.plain)
		}
		if got := Render(Input{}, c.f, true); !strings.Contains(got, c.colour+reset) {
			t.Errorf("%+v colour: %q", c.f, got)
		}
	}
}

func TestRenderModeSegmentAfterClient(t *testing.T) {
	got := Render(Input{}, Facts{Client: "acme", GitEmail: "tester@acme.test", Mode: "strict"}, false)
	if got != "acme · tq: strict · tester@acme.test" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderOff(t *testing.T) {
	in := Input{}
	in.Model.DisplayName = "Opus"
	got := Render(in, Facts{Client: "acme", GitEmail: "tester@acme.test", Mode: ModeOff}, false)
	if got != "tq: off · Opus" {
		t.Fatalf("off hides the identity tq is not applying: %q", got)
	}
	if c := Render(in, Facts{Mode: ModeOff}, true); !strings.HasPrefix(c, red+"tq: off"+reset) {
		t.Fatalf("off is red: %q", c)
	}
}

func TestRenderNoModeKeepsTodaysLine(t *testing.T) {
	if got := Render(Input{}, Facts{Client: "acme"}, false); got != "acme" {
		t.Fatalf("got %q", got)
	}
}

func TestSkippedCountsWorkspaceIn24h(t *testing.T) {
	isolate(t)
	now := time.Now().UTC()
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "deny", Rules: []string{"tq/a"}}, nil)
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "ask", Rules: []string{"tq/b"}}, nil)
	guardlog.Append(guardlog.Record{Time: now, WS: "other", Action: "ask", Rules: []string{"tq/b"}}, nil)
	guardlog.Append(guardlog.Record{Time: now.Add(-25 * time.Hour), WS: "acme", Action: "ask", Rules: []string{"tq/b"}}, nil)
	if n := Skipped("acme", now); n != 2 {
		t.Fatalf("skipped = %d", n)
	}
	if n := Skipped("", now); n != 0 {
		t.Fatalf("neutral = %d", n)
	}
}

// The status line runs on every prompt: a 5 MB log must not slow it down.
func TestSkippedStaysFastOnABigLog(t *testing.T) {
	isolate(t)
	now := time.Now().UTC()
	p := guardlog.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"ts":"` + now.Add(-time.Minute).Format(time.RFC3339) + `","ws":"acme","action":"ask","rule_ids":["tq/x"],"tool":"Bash","command":"` + strings.Repeat("x", 150) + `"}` + "\n"
	var b strings.Builder
	for b.Len() < 5<<20 {
		b.WriteString(line)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	n := Skipped("acme", now)
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Skipped took %s on a 5 MB log", d)
	}
	if n == 0 {
		t.Fatal("expected counts from the tail")
	}
}
