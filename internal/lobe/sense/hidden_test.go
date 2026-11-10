package sense

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
)

// shellResponse is a shell tool's result object with the given output.
func shellResponse(stdout, stderr string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"stdout": stdout, "stderr": stderr})
	return b
}

// commandInput is a shell tool's input with the given command.
func commandInput(command string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": command})
	return b
}

func TestLobeHiddenFixtures(t *testing.T) {
	cases := []struct {
		name, tool, command, stdout string
		want                        bool
	}{
		// hidden failures
		{"jest through tail", "Bash", "npm test | tail -5", "FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n", true},
		{"vitest summary", "Bash", "npx vitest run", " Test Files  1 failed | 12 passed (13)\n      Tests  2 failed | 40 passed (42)\n", true},
		{"pytest count", "Bash", "pytest -q", "=========== 1 failed, 4 passed in 0.12s ===========\n", true},
		{"go test output", "Bash", "go test ./...", "--- FAIL: TestCart (0.00s)\n    cart_test.go:12: want 3\nFAIL\nFAIL\tshop/cart\t0.01s\n", true},
		{"cargo result", "Bash", "cargo test", "test result: FAILED. 11 passed; 1 failed; 0 ignored\n", true},
		{"pester in PowerShell", "PowerShell", "Invoke-Pester", "Tests: 2 failed, 5 passed\n", true},
		{"TAP fail", "Bash", "npm test 2>&1 | tail -5", "# tests 6\n# fail 2\n", true},
		{"unittest summary", "Bash", "python -m unittest", "Ran 3 tests in 0.002s\n\nFAILED (failures=1)\n", true},
		{"bare FAIL line", "Bash", "make test", "FAIL\n", true},
		{"FAIL line beside a zero count", "Bash", "npx jest", "FAIL a.test.ts\n  Tests: 0 failed\n", true},
		{"git push through tail", "Bash", "git push 2>&1 | tail -3", "fatal: no remote\n", true},
		{"error line under || true", "Bash", "pytest || true", "error: collection failed\n", true},
		{"permission line", "Bash", "./deploy.sh | head -2", "bash: ./deploy.sh: Permission denied\n", true},
		{"npm error lines", "Bash", "npm run build | tail -3", "npm ERR! code ELIFECYCLE\nnpm ERR! errno 1\n", true},
		{"rustc error code", "Bash", "cargo build 2>&1 | head -40", "error[E0382]: borrow of moved value\n", true},
		// passes that mention failure words
		{"zero failed", "Bash", "npm test", "Tests 0 failed\n", false},
		{"cargo ok with zero failed", "Bash", "cargo test", "test result: ok. 12 passed; 0 failed\n", false},
		{"go ok line", "Bash", "go test ./...", "ok  \tshop/cart\t0.01s\n", false},
		{"pytest zero failed through tail", "Bash", "pytest | tail -3", "=== 4 passed, 0 failed in 0.1s ===\n", false},
		{"vitest all passed", "Bash", "npm test | tail -5", " Test Files  13 passed (13)\n      Tests  42 passed (42)\n", false},
		{"echo of a pipe", "Bash", `echo "a | tail"`, "fatal in name only\n", false},
		{"reader in front", "Bash", "cat README.md | tail -3", "error: example in docs\n", false},
		{"grep reader in front", "Bash", "grep -r FAIL src | head", "src/a.go:FAIL marker\n", false},
		{"plural words of zero", "Bash", "npm test | tail -5", "All good: 0 failures, 0 errors\n", false},
		{"git status", "Bash", "git status", "modified: failed.txt\n", false},
		{"PowerShell FAILED without a run line", "PowerShell", "Get-Content report.txt", "Failed: 0\nFAILED none\n", false},
		{"go pass", "Bash", "go test -run X", "PASS\nok  \tshop\t0.1s\n", false},
		{"tee with zero count", "Bash", "make test | tee log", "Tests: 0 failed, 9 passed\n", false},
		{"no pipe, no rule C", "Bash", `echo "npm ERR! sample"`, "npm ERR! sample\n", false},
		{"TAP zero", "Bash", "npm test", "# fail 0\n# pass 12\n", false},
	}
	hidden, passes := 0, 0
	for _, tc := range cases {
		_, got := Hidden(tc.tool, commandInput(tc.command), shellResponse(tc.stdout, ""))
		if got != tc.want {
			t.Errorf("%s: Hidden(%q) = %v, want %v", tc.name, tc.command, got, tc.want)
		}
		if tc.want {
			hidden++
		} else {
			passes++
		}
	}
	if hidden != 15 || passes != 15 {
		t.Fatalf("fixture counts: %d hidden, %d passes; want 15 and 15", hidden, passes)
	}
}

func TestLobeHiddenMcpFlag(t *testing.T) {
	errText, hidden := Hidden("mcp__acme__ping", json.RawMessage(`{"n":1}`),
		json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`))
	if !hidden || errText != "boom" {
		t.Fatalf("Hidden = (%q, %v), want (\"boom\", true)", errText, hidden)
	}
	// is_error on a string content is the same flag
	errText, hidden = Hidden("mcp__acme__ping", json.RawMessage(`{}`),
		json.RawMessage(`{"is_error":true,"content":"widget broke"}`))
	if !hidden || errText != "widget broke" {
		t.Fatalf("is_error: Hidden = (%q, %v), want (\"widget broke\", true)", errText, hidden)
	}
	// a flag with no text is hidden with the fixed text, not an empty error
	errText, hidden = Hidden("mcp__acme__ping", json.RawMessage(`{}`), json.RawMessage(`{"isError":true}`))
	if !hidden || errText != noErrorText {
		t.Fatalf("no text: Hidden = (%q, %v), want (%q, true)", errText, hidden, noErrorText)
	}
	// a decimal before the word is no count
	for _, out := range []string{`ratio 0.5 failed to converge`, `v1.5 failed upstream`} {
		resp, _ := json.Marshal(map[string]string{"stdout": out, "stderr": ""})
		if e, h := Hidden("Bash", json.RawMessage(`{"command":"python report.py"}`), resp); h {
			t.Fatalf("%q read as a count: (%q, true)", out, e)
		}
	}
	if _, h := Hidden("Bash", json.RawMessage(`{"command":"python report.py"}`), json.RawMessage(`{"stdout":"3 failed","stderr":""}`)); !h {
		t.Fatal(`"3 failed" at a line start is no longer a count`)
	}
	// a flag that is false, or a non-shell MCP text that reads like a summary, is a success
	if _, hidden := Hidden("mcp__acme__ping", json.RawMessage(`{}`),
		json.RawMessage(`{"isError":false,"content":[{"type":"text","text":"FAIL: 3 failed"}]}`)); hidden {
		t.Fatal("a false flag with failure words in an MCP text is hidden")
	}
}

func TestLobeHiddenWindow(t *testing.T) {
	// a FAIL line at character 2000 of a 10,000 character output is outside the window
	pre := strings.Repeat("a", 2000) + "\n"
	const line = "FAIL x.test.ts\n"
	out := pre + line + strings.Repeat("b", 10000-len(pre)-len(line))
	if _, hidden := Hidden("Bash", commandInput("npm test"), shellResponse(out, "")); hidden {
		t.Error("a FAIL line at character 2000 of 10,000 was read")
	}
	// the same line at character 9000 is in the window (the control)
	pre = strings.Repeat("a", 9000) + "\n"
	out = pre + line + strings.Repeat("b", 10000-len(pre)-len(line))
	if errText, hidden := Hidden("Bash", commandInput("npm test"), shellResponse(out, "")); !hidden || !strings.HasPrefix(errText, "FAIL x.test.ts") {
		t.Errorf("a FAIL line at character 9000 was not read: (%q, %v)", errText, hidden)
	}

	// a 1 MiB response: the best of several runs stays under 5 ms
	big := shellResponse(strings.Repeat("ordinary output line 0123456789\n", (1<<20)/31+1), "")
	input := commandInput("npm test")
	best := time.Duration(1 << 62)
	for i := 0; i < 7; i++ {
		start := time.Now()
		Hidden("Bash", input, big)
		if d := time.Since(start); d < best {
			best = d
		}
	}
	if best >= 5*time.Millisecond {
		t.Errorf("Hidden on a 1 MiB response took %v (best of 7), want under 5 ms", best)
	}
}

// encodeEscaped is a JSON string of the runes that uses escapes for some of them: \" \\ \n \t, \u for
// a few letters and a few characters, and surrogate pairs for some astral ones.
func encodeEscaped(rs []rune) string {
	var b strings.Builder
	b.WriteByte('"')
	for i, r := range rs {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == 'é' && i%2 == 0:
			b.WriteString(`\u00e9`)
		case r == 'a' && i%5 == 0:
			b.WriteString(`\u0061`)
		case r >= 0x10000 && i%3 == 0:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04X\u%04X`, hi, lo)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestLobeHiddenWindowMatchesDecode checks the window against a full decode of the same text, over
// escapes, surrogate pairs and a line break between stdout and stderr.
func TestLobeHiddenWindowMatchesDecode(t *testing.T) {
	pool := []rune("au04\n\"\\é😀\tb x")
	rng := rand.New(rand.NewSource(7))
	gen := func(n int) []rune {
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = pool[rng.Intn(len(pool))]
		}
		return rs
	}
	cases := []struct{ out, err int }{
		{3, 0}, {1500, 0}, {3999, 0}, {4000, 0}, {4001, 0}, {9000, 0}, {100, 9000}, {0, 9000}, {2600, 2600},
	}
	for _, tc := range cases {
		out, errRunes := gen(tc.out), gen(tc.err)
		resp := `{"stdout":` + encodeEscaped(out) + `,"stderr":` + encodeEscaped(errRunes) + `}`

		var full []rune
		full = append(full, out...)
		if len(out) > 0 && len(errRunes) > 0 {
			full = append(full, '\n')
		}
		full = append(full, errRunes...)
		want := string(full)
		if len(full) > 4000 {
			want = string(full[:1500]) + string(full[len(full)-2500:])
		}

		var decoded struct{ Stdout, Stderr string }
		if err := json.Unmarshal([]byte(resp), &decoded); err != nil {
			t.Fatalf("test response is not JSON: %v", err)
		}
		if got := responseText(json.RawMessage(resp)); got != want {
			t.Errorf("out %d, err %d: window has %d runes, want %d", tc.out, tc.err, len([]rune(got)), len([]rune(want)))
		}
	}
}

// TestLobeHiddenEvaluationChecks gives Hidden the inputs of checks 1, 4, 5, 6, 7 and 10 of feature 009's
// Evaluation: the hidden/ok answer and, for a hidden one, the start of the error.
func TestLobeHiddenEvaluationChecks(t *testing.T) {
	cartFail := "FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n"
	cases := []struct {
		name, tool, command, stdout, errStart string
		hidden                                bool
	}{
		{"check 1", "Bash", "npm test 2>&1 | tail -5", cartFail, "FAIL src/cart.test.ts", true},
		{"check 4", "Bash", "git push 2>&1 | tail -3", "fatal: no remote\n", "fatal: no remote", true},
		{"check 5", "Bash", "cat deploy.log | tail -3", "error: disk full\n", "", false},
		{"check 6", "Bash", "./deploy.sh 2>&1 | tail -3", "note: a Permission denied error would stop the run\nall good\n", "", false},
		{"check 7", "PowerShell", "git push 2>&1 | tail -3", "fatal: no remote\n", "", false},
		{"check 10", "Bash", "npm test 2>&1 | tail -5", "SECRET-ROW-7731 loaded\n" + cartFail, "FAIL src/cart.test.ts", true},
	}
	for _, tc := range cases {
		errText, hidden := Hidden(tc.tool, commandInput(tc.command), shellResponse(tc.stdout, ""))
		if hidden != tc.hidden {
			t.Errorf("%s: hidden = %v, want %v", tc.name, hidden, tc.hidden)
			continue
		}
		if tc.hidden && !strings.HasPrefix(errText, tc.errStart) {
			t.Errorf("%s: error %q does not start with %q", tc.name, errText, tc.errStart)
		}
		if strings.Contains(errText, "SECRET-ROW-7731") {
			t.Errorf("%s: the error keeps output before the deciding line", tc.name)
		}
	}
}

// TestLobeHiddenErrorCut checks the 20 line cut and the removed line breaks at the end of an error.
func TestLobeHiddenErrorCut(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "fatal: line %d\n", i)
	}
	errText, hidden := Hidden("Bash", commandInput("make | tail -3"), shellResponse(b.String(), ""))
	if !hidden {
		t.Fatal("not hidden")
	}
	if n := strings.Count(errText, "\n"); n != errorLines-1 {
		t.Errorf("error has %d line breaks, want %d", n, errorLines-1)
	}
	if strings.HasSuffix(errText, "\n") {
		t.Errorf("error ends in a line break: %q", errText)
	}
}

// TestLobeHiddenFingerprints: the fingerprint of the error of tester checks 1 and 3 of feature 009, worked
// out the way Record does (fingerprint.Of on the tool and the error).
func TestLobeHiddenFingerprints(t *testing.T) {
	errText, ok := Hidden("Bash", commandInput("npm test 2>&1 | tail -5"),
		shellResponse("FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n", ""))
	if !ok {
		t.Fatal("check 1 is not hidden")
	}
	if fp, _ := fingerprint.Of("Bash", errText); fp != "fp_90da432fb402b004" {
		t.Errorf("check 1 fp = %s, want fp_90da432fb402b004", fp)
	}
	errText, ok = Hidden("mcp__acme__ping", json.RawMessage(`{"n":4411}`),
		json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"widget 4411 is not frobnicated"}]}`))
	if !ok {
		t.Fatal("check 3 is not hidden")
	}
	if fp, _ := fingerprint.Of("mcp__acme__ping", errText); fp != "fp_48622a622f4928cc" {
		t.Errorf("check 3 fp = %s, want fp_48622a622f4928cc", fp)
	}
}
