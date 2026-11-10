package hook

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/cli"
	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
)

// hiddenLine is the part of a stored line the hidden-fail tests read: the fingerprint too, which
// logged does not carry.
type hiddenLine struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Error string `json:"error"`
	Exit  *int   `json:"exit"`
	FP    string `json:"fp"`
}

// decodeHidden decodes event lines into hiddenLine values.
func decodeHidden(t *testing.T, ls []string) []hiddenLine {
	t.Helper()
	var out []hiddenLine
	for _, l := range ls {
		var e hiddenLine
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("bad event line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

// TestLobeSenseHiddenFailEvent: the payloads of tester checks 1, 3 and 4 of feature 009 each write one
// hidden-fail line with the full input, an error of at most 20 lines and the fingerprint of that error,
// and the status counts the three.
func TestLobeSenseHiddenFailEvent(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	payloads := []struct {
		name, stdin, id, tool, input, errStart string
	}{
		{"piped test run", successJSON("s1", app, "Bash", "toolu_01", `{"command":"npm test 2>&1 | tail -5"}`,
			`{"stdout":"FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n","stderr":""}`),
			"evt_9bde2e09", "Bash", `{"command":"npm test 2>&1 | tail -5"}`, "FAIL src/cart.test.ts"},
		{"MCP result with the error flag", successJSON("s1", app, "mcp__acme__ping", "toolu_03", `{"n":4411}`,
			`{"isError":true,"content":[{"type":"text","text":"widget 4411 is not frobnicated"}]}`),
			"evt_7fe44cf7", "mcp__acme__ping", `{"n":4411}`, "widget 4411 is not frobnicated"},
		{"hidden exit code, anchored line", successJSON("s1", app, "Bash", "toolu_04", `{"command":"git push 2>&1 | tail -3"}`,
			`{"stdout":"fatal: no remote\n","stderr":""}`),
			"evt_20ed2862", "Bash", `{"command":"git push 2>&1 | tail -3"}`, "fatal: no remote"},
	}
	for _, p := range payloads {
		before := len(lines(t, "acme"))
		if out := runHook(t, evPostToolUse, p.stdin); out != "" {
			t.Fatalf("%s: stdout %q, want nothing", p.name, out)
		}
		ls := lines(t, "acme")
		if len(ls) != before+1 {
			t.Fatalf("%s: %d lines, want %d", p.name, len(ls), before+1)
		}
		e := decodeHidden(t, ls[before:])[0]
		if e.ID != p.id || e.Kind != "hidden-fail" || e.Tool != p.tool || e.Exit != nil {
			t.Errorf("%s: id %s kind %s tool %s exit %v; want %s hidden-fail %s nil", p.name, e.ID, e.Kind, e.Tool, e.Exit, p.id, p.tool)
		}
		if e.Input != p.input {
			t.Errorf("%s: input %q, want the full input %q", p.name, e.Input, p.input)
		}
		if !strings.HasPrefix(e.Error, p.errStart) {
			t.Errorf("%s: error %q, want it to start with %q", p.name, e.Error, p.errStart)
		}
		if n := len(strings.Split(e.Error, "\n")); n > 20 {
			t.Errorf("%s: error has %d lines, want at most 20", p.name, n)
		}
		want, _ := fingerprint.Of(p.tool, e.Error)
		if e.FP != want {
			t.Errorf("%s: fp %q, want %q", p.name, e.FP, want)
		}
	}

	// The status counts the three hidden failures.
	var buf strings.Builder
	cwd := f.client("acme")
	d := cli.Deps{
		InAgent: func() bool { return false },
		Getenv:  os.Getenv,
		Getwd:   func() (string, error) { return cwd, nil },
	}
	if err := cli.Status(d, "", true, nil, false, &buf); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Events struct {
			HiddenFail int `json:"hidden_fail"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &doc); err != nil {
		t.Fatalf("status --json: %v in %q", err, buf.String())
	}
	if doc.Events.HiddenFail != 3 {
		t.Errorf("status hidden_fail = %d, want 3", doc.Events.HiddenFail)
	}
}

// TestLobeSenseHiddenFailKeepsOnlyTheError: the response text outside the error (a line before the FAIL
// line) is in no file under the client's lobe folder.
func TestLobeSenseHiddenFailKeepsOnlyTheError(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	stdin := successJSON("s2", f.app("acme"), "Bash", "toolu_10", `{"command":"npm test 2>&1 | tail -5"}`,
		`{"stdout":"SECRET-ROW-7731 loaded\nFAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n","stderr":""}`)
	runHook(t, evPostToolUse, stdin)
	ls := decodeHidden(t, lines(t, "acme"))
	if len(ls) != 1 || ls[0].Kind != "hidden-fail" || !strings.HasPrefix(ls[0].Error, "FAIL src/cart.test.ts") {
		t.Fatalf("lines %+v, want one hidden-fail whose error starts at the FAIL line", ls)
	}
	for name, body := range lobeFiles(t, "acme") {
		if strings.Contains(body, "SECRET-ROW-7731") {
			t.Errorf("%s holds the response text outside the error", name)
		}
	}
}
