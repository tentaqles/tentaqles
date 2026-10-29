package model

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

// Review r2 Major 1: the YAML path decoded into Go values, whose duplicate
// key check is quadratic, so a long "Note: v" text stalled for minutes.
func TestScrubEncodedYAMLIsLinear(t *testing.T) {
	cases := map[string]string{
		"repeated keys, 1 MiB":   strings.Repeat("Note: v\n", 1<<17),
		"repeated keys, 900 KiB": strings.Repeat("Note: v\n", 115000),
		"distinct keys, 900 KiB": distinctKeys(60000),
		"list items, 900 KiB":    strings.Repeat("- Note: v\n", 92000),
		"over the YAML cap":      strings.Repeat("Note: v\n", 1<<18),
		"near maxEncodedLen":     strings.Repeat("Note: v\n", (maxEncodedLen-16)/8),
	}
	for name, s := range cases {
		start := time.Now()
		got := Scrub(map[string]any{"content": s})
		// The YAML work is about 0.15 s per MiB; secrets.Redact over the
		// whole string adds about 0.5 s per MiB. Round 2 took minutes: a
		// generous wall-clock cap still catches that on a loaded CI runner,
		// and the scaling test below catches a superlinear one.
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("%s: Scrub took %v", name, d)
		}
		if _, ok := got.(map[string]any)["content"].(string); !ok {
			t.Errorf("%s: content is not a string: %T", name, got)
		}
	}
}

// Scrub scales linearly: four times the input takes well under sixteen
// times as long (a quadratic scan would). The median of 5 runs per size,
// and a ratio rather than a wall clock, so CI load cancels out.
func TestScrubScalesLinearly(t *testing.T) {
	median := func(s string) time.Duration {
		var ds []time.Duration
		for i := 0; i < 5; i++ {
			start := time.Now()
			Scrub(map[string]any{"content": s})
			ds = append(ds, time.Since(start))
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		return ds[len(ds)/2]
	}
	for name, mk := range map[string]func(n int) string{
		"repeated keys": func(n int) string { return strings.Repeat("Note: v\n", n) },
		"distinct keys": distinctKeys,
	} {
		small, big := median(mk(8000)), median(mk(32000))
		if small < time.Millisecond {
			small = time.Millisecond
		}
		if r := float64(big) / float64(small); r > 10 {
			t.Errorf("%s: 4x the input took %.1fx as long (%v vs %v)", name, r, big, small)
		}
	}
}

func distinctKeys(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("key")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(itoa(i))
		b.WriteString(": v\n")
	}
	return b.String()
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// Past the YAML size cap or the node budget, a YAML-looking string fails
// closed instead of passing through with only secrets.Redact.
func TestScrubEncodedYAMLCapsFailClosed(t *testing.T) {
	big := strings.Repeat("Note: v\n", 1<<18) + "password: tester-cap1\n" // tq:allow-secret (fake fixture)
	if got := Scrub(big); got != RedactedKey {
		t.Fatalf("over the YAML cap: got %d bytes, want %q", len(got.(string)), RedactedKey)
	}
	// An alias bomb: each level doubles the values the conversion expands.
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x]\n")
	for i := 1; i < 30; i++ {
		b.WriteString("a" + itoa(i) + ": &a" + itoa(i) + " [*a" + itoa(i-1) + ", *a" + itoa(i-1) + "]\n")
	}
	b.WriteString("password: tester-bomb1\n") // tq:allow-secret (fake fixture)
	start := time.Now()
	got := Scrub(b.String())
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("alias bomb took %v", d)
	}
	if got != RedactedKey {
		t.Fatalf("alias bomb: got %q, want %q", got, RedactedKey)
	}
	// Under the caps, YAML still re-encodes with the secret scrubbed.
	if got := Scrub("Note: v\npassword: tester-cap2\n").(string); strings.Contains(got, "tester-cap2") || !strings.Contains(got, "Note") { // tq:allow-secret (fake fixture)
		t.Fatalf("small YAML: %q", got)
	}
}

// Review r2 Minor 1: shapes that still leaked.
func TestScrubEncodedRound3Shapes(t *testing.T) {
	cases := map[string]string{
		"list after doc marker": "---\n- password: tester-r3a\n",                                    // tq:allow-secret (fake fixture)
		"slash key":             "app/password: tester-r3b\n",                                       // tq:allow-secret (fake fixture)
		"slash key no nl":       "acme/storagekey: tester-r3c",                                      // tq:allow-secret (fake fixture)
		"space key":             "acme storage key: tester-r3i",                                     // tq:allow-secret (fake fixture)
		"scalar between json":   `{"a":1} 5 {"password":"tester-r3d"}`,                              // tq:allow-secret (fake fixture)
		"string between json":   `{"a":1} "s" {"password":"tester-r3e"}`,                            // tq:allow-secret (fake fixture)
		"yaml parse error":      "password: tester-r3f\nfoo: [\n",                                   // tq:allow-secret (fake fixture)
		"list item parse error": "- token: tester-r3g\n  bad: [\n",                                  // tq:allow-secret (fake fixture)
		"merge key":             "base: &b\n  mode: prod\nuse:\n  <<: *b\n  password: tester-r3j\n", // tq:allow-secret (fake fixture)
		"merged secret":         "base: &b {api_token: tester-r3k}\nuse:\n  <<: *b\n",               // tq:allow-secret (fake fixture)
	}
	for name, s := range cases {
		got := Scrub(map[string]any{"v": s})
		b, _ := json.Marshal(got)
		if strings.Contains(string(b), "tester-r3") {
			t.Errorf("%s: leak in %s", name, b)
		}
	}
	// The non-secret parts survive where the string still parses.
	if got := Scrub("app/password: tester-r3h\nmode: prod\n").(string); !strings.Contains(got, "prod") || strings.Contains(got, "tester-r3h") { // tq:allow-secret (fake fixture)
		t.Errorf("slash key lost a value: %q", got)
	}
	if got := Scrub(cases["scalar between json"]).(string); !strings.HasPrefix(got, `{"a":1} 5 {`) {
		t.Errorf("scalar between json: %q", got)
	}
	// A parse error without a secret-looking key line is kept as it is.
	for _, keep := range []string{"foo: [\n", "{not yaml: [", "note: x\nbad: [\n", `{"a":1} 5 6`} {
		if got := Scrub(keep); got != keep {
			t.Errorf("Scrub(%q) = %q, want it unchanged", keep, got)
		}
	}
}
