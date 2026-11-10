package fingerprint

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// tplRows are rows 1 to 5 of the tester table of S-08 (docs/prd/lobe-sense.md), as signature lines. Rows 1 to
// 3 each have a second variant with the change the table names; both give the same template.
var tplRows = []struct {
	name, signature, want string
}{
	{"row 1 as written", "/usr/bin/bash: line 1: Get-ChildItem: command not found",
		"<path>: line <n>: Get-ChildItem: command not found"},
	{"row 1 with /bin/bash and line 12", "/bin/bash: line 12: Get-ChildItem: command not found",
		"<path>: line <n>: Get-ChildItem: command not found"},
	{"row 2 as written", "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)",
		"[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
	{"row 2 with 'SELECT' and pos 41", "[PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)",
		"[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
	{"row 3 as written", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.",
		"HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
	{"row 3 with 91cc and 3 seconds", "HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending 91cc. Retry after 3 seconds.",
		"HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
	{"row 4 is the text of row 2", "[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)",
		"[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)"},
	{"row 5 with 404 Not Found", "HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.",
		"HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds."},
}

func TestLobeFingerprintTemplateRows(t *testing.T) {
	for _, r := range tplRows {
		if got := Template(r.signature); got != r.want {
			t.Errorf("%s:\n got  %q\n want %q", r.name, got, r.want)
		}
	}
}

// tplRandWord returns n random lowercase letters.
func tplRandWord(rng *rand.Rand, n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rng.IntN(len(letters))]
	}
	return string(b)
}

// tplRandID returns four random hex digits that hold at least one digit and one letter, so that mask (6)
// reads them as an id.
func tplRandID(rng *rand.Rand) string {
	const hex = "0123456789abcdef"
	for {
		b := make([]byte, 4)
		hasDigit, hasLetter := false, false
		for i := range b {
			b[i] = hex[rng.IntN(len(hex))]
			hasDigit = hasDigit || b[i] <= '9'
			hasLetter = hasLetter || b[i] >= 'a'
		}
		if hasDigit && hasLetter {
			return string(b)
		}
	}
}

func TestLobeFingerprintStable(t *testing.T) {
	const variants = 50
	rng := rand.New(rand.NewPCG(20261110, 6))
	gen := map[string]func() string{
		"row 1": func() string {
			path := fmt.Sprintf("/%s/%s/%s.sh", tplRandWord(rng, 3+rng.IntN(5)), tplRandWord(rng, 3+rng.IntN(5)), tplRandWord(rng, 3+rng.IntN(5)))
			if rng.IntN(2) == 0 {
				path = fmt.Sprintf(`C:\%s\%s\run.ps1`, tplRandWord(rng, 3+rng.IntN(5)), tplRandWord(rng, 3+rng.IntN(5)))
			}
			return fmt.Sprintf("%s: line %d: Get-ChildItem: command not found", path, 1+rng.IntN(99999))
		},
		"row 2": func() string {
			quote := []string{"'", `"`, "`"}[rng.IntN(3)]
			content := tplRandWord(rng, 1+rng.IntN(12))
			if rng.IntN(2) == 0 {
				content += " " + tplRandWord(rng, 1+rng.IntN(8))
			}
			return fmt.Sprintf("[PARSE_SYNTAX_ERROR] Syntax error at or near %s%s%s. SQLSTATE: 42601 (line %d, pos %d)",
				quote, content, quote, 1+rng.IntN(999), 1+rng.IntN(999))
		},
		"row 3": func() string {
			return fmt.Sprintf("HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending %s. Retry after %d seconds.",
				tplRandID(rng), 1+rng.IntN(999))
		},
	}
	want := map[string]string{
		"row 1": tplRows[0].want,
		"row 2": tplRows[2].want,
		"row 3": tplRows[4].want,
	}
	for _, row := range []string{"row 1", "row 2", "row 3"} {
		seen := map[string]bool{}
		for i := 0; i < variants; i++ {
			sig := gen[row]()
			got := Template(sig)
			seen[got] = true
			if got != want[row] {
				t.Fatalf("%s variant %d: signature %q\n got  %q\n want %q", row, i, sig, got, want[row])
			}
		}
		if len(seen) != 1 {
			t.Errorf("%s: %d templates over %d variants, want 1", row, len(seen), variants)
		}
	}
}

func TestLobeFingerprintTemplateMasks(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// mask 1
		{"marker", "password=[redacted:named] then [redacted:api-key]", "password=<secret> then <secret>"},
		// mask 2
		{"url keeps last two labels", "GET https://api.acme.io/v2/widgets?id=4 failed", "GET <url:acme.io> failed"},
		{"url address and port", "dial http://10.0.0.1:8080/x now", "dial <url:ip> now"},
		{"url trailing dot stays out", "see http://localhost:3000.", "see <url:localhost>."},
		// mask 3: the apostrophe after a letter opens nothing; the quotes after a space do
		{"quotes", "can't find 'widget' or \"it's\"", "can't find <q> or <q>"},
		{"backticks", "run `make` now", "run <q> now"},
		// mask 4 and mask 7
		{"keyword number kept", "failed with exit code 127 after 5 tries", "failed with exit code 127 after <n> tries"},
		{"http status kept", "HTTP 404 and HTTP 429", "HTTP 404 and HTTP 429"},
		{"error code kept", "ORA-00942: table missing", "ORA-00942: table missing"},
		{"error code TS2345 kept", "TS2345 in build", "TS2345 in build"},
		{"number without keyword masked", "room 42 and abc123", "room <n> and <id>"},
		// mask 5
		{"path with line and column", "open /var/log/app.log:12:5: failed", "open <path>: failed"},
		{"relative path with extension", "edit src/main.go now", "edit <path> now"},
		{"slash without extension stays", "read/write mode", "read/write mode"},
		// mask 6
		{"numbers", "pid 4411 at 1.5, x", "pid <n> at <n>, x"},
		// spaces and the cut
		{"spaces collapse and trim", "  alpha \t beta   gamma  ", "alpha beta gamma"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := Template(c.in); got != c.want {
			t.Errorf("%s: Template(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLobeFingerprintTemplateMarkerIsSecret(t *testing.T) {
	got := Template("login failed for token [redacted:key] at host")
	if got != "login failed for token <secret> at host" {
		t.Errorf("marker: got %q", got)
	}
}

func TestLobeFingerprintTemplateQuoteLimit(t *testing.T) {
	within := "'" + strings.Repeat("a", 200) + "'"
	if got := Template("x " + within); got != "x <q>" {
		t.Errorf("200 characters inside quotes: got %q, want %q", got, "x <q>")
	}
	over := "'" + strings.Repeat("a", 201) + "'"
	if got := Template("x " + over); strings.Contains(got, "<q>") {
		t.Errorf("201 characters inside quotes were masked: %q", got)
	}
}

func TestLobeFingerprintTemplateCut(t *testing.T) {
	if got := Template(strings.Repeat("a", 300)); got != strings.Repeat("a", 200) {
		t.Errorf("ASCII cut: got %d characters", len(got))
	}
	// 250 two-byte characters: the cut counts characters, so 200 of them remain.
	if got := Template(strings.Repeat("é", 250)); got != strings.Repeat("é", 200) {
		t.Errorf("rune cut: got %d characters, want 200", len([]rune(got)))
	}
}
