package sense

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
)

// TestLobeSenseBackgroundTexts: the result of a shell call that only says the command went to the
// background (the texts of background.go) is recognized; a result that quotes the text, another tool or
// any other result is not.
func TestLobeSenseBackgroundTexts(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		response string
		want     bool
	}{
		{"start, string", "Bash", `"Command running in background with ID: bash_1"`, true},
		{"start, leading white space", "Bash", `"  Command running in background with ID: bash_1"`, true},
		{"start, PowerShell", "PowerShell", `"Command running in background with ID: ps_9"`, true},
		{"start, object field", "Bash", `{"stdout":"Command running in background with ID: bash_1","stderr":""}`, true},
		{"timeout in seconds", "Bash", `"Command did not complete within its 300s timeout and was moved to the background (ID: b2)"`, true},
		{"timeout in milliseconds", "Bash", `"Command did not complete within its 120000ms timeout and was moved to the background (ID: b3)"`, true},
		{"timeout, object field", "PowerShell", `{"stdout":"","stderr":"Command did not complete within its 60s timeout and was moved to the background"}`, true},
		{"timeout without a number", "Bash", `"Command did not complete within its timeout and was moved to the background"`, false},
		{"timeout with another ending", "Bash", `"Command did not complete within its 60s timeout and was never run"`, false},
		{"quoted in the middle", "Bash", `"log: Command running in background with ID: bash_1"`, false},
		{"another tool", "Write", `"Command running in background with ID: bash_1"`, false},
		{"another tool, object", "mcp__acme__ping", `{"text":"Command running in background with ID: x"}`, false},
		{"start, task id (captured shape)", "Bash", `{"stdout":"","stderr":"","interrupted":false,"isImage":false,"noOutputExpected":false,"backgroundTaskId":"bmx1"}`, true},
		{"start, task id, PowerShell", "PowerShell", `{"stdout":"","stderr":"","backgroundTaskId":"p2"}`, true},
		{"task id empty", "Bash", `{"stdout":"done","stderr":"","backgroundTaskId":""}`, false},
		{"task id blank", "Bash", `{"stdout":"done","stderr":"","backgroundTaskId":"  "}`, false},
		{"task id not a string", "Bash", `{"stdout":"done","stderr":"","backgroundTaskId":null}`, false},
		{"task id, another tool", "mcp__acme__ping", `{"backgroundTaskId":"x"}`, false},
		{"ordinary output", "Bash", `{"stdout":"On branch main","stderr":"","interrupted":false}`, false},
		{"plain text", "Bash", `"pong"`, false},
		{"number", "Bash", `42`, false},
		{"empty", "Bash", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BackgroundStart(c.tool, json.RawMessage(c.response)); got != c.want {
				t.Errorf("BackgroundStart(%s, %s) = %v, want %v", c.tool, c.response, got, c.want)
			}
		})
	}
}

// TestLobeSenseOkInputForm: one object input, stored as a failure (its full input) and as a success with
// the full-input exception, gives byte-identical input values. For an MCP tool whose input has only
// top-level scalars, the projection gives the same bytes too, so the success form is the failure form.
func TestLobeSenseOkInputForm(t *testing.T) {
	inputs := []string{
		`{"zeta":"\u00e9t\u00e9","alpha":1.50,"mid":true,"n":null,"tag":"<b>&amp;</b>"}`,
		`{ "b" : "x" , "a" : [1, {"k": "v"}] , "c":{"deep":{"x":1}} }`,
		`{"command":"echo \"hi\"","description":"say it"}`,
	}
	now := fixedNow()
	for _, in := range inputs {
		raw := json.RawMessage(in)
		failure := FromClaudeFailure(Call{Session: "s1", Tool: "mcp__acme__ping", CallID: "f1", Input: raw, Error: "widget 4411 is not frobnicated"},
			Place{Cwd: "."}, now, nil, false)
		// A failure of the tool in the tail: the success keeps the full input.
		tail := []eventlog.TailEvent{{Session: "s1", Tool: "mcp__acme__ping", Kind: eventlog.KindFail}}
		success, ok := FromClaudePostTool(Call{Session: "s1", Tool: "mcp__acme__ping", CallID: "s1", Input: raw},
			json.RawMessage(`"pong"`), Place{Cwd: "."}, now, tail, true)
		if !ok {
			t.Fatalf("%s: the success was skipped", in)
		}
		if failure.Input.String() != success.Input.String() {
			t.Errorf("input %s:\nfailure %s\nsuccess %s", in, failure.Input.String(), success.Input.String())
		}
		// Without the exception the projection of a scalar-only input is the same object.
		plain, ok := FromClaudePostTool(Call{Session: "s1", Tool: "mcp__acme__ping", CallID: "s2", Input: raw},
			json.RawMessage(`"pong"`), Place{Cwd: "."}, now, nil, true)
		if !ok {
			t.Fatalf("%s: the success was skipped", in)
		}
		if got, want := plain.Input.String(), failure.Input.String(); got != want && isScalarObject(in) {
			t.Errorf("projection of a scalar input %s = %s, want %s", in, got, want)
		}
	}
}

// isScalarObject reports whether the input is an object whose values are all scalars and short strings,
// the inputs for which the projection keeps every value.
func isScalarObject(in string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		return false
	}
	for _, v := range m {
		switch x := v.(type) {
		case map[string]any, []any:
			return false
		case string:
			if len([]rune(x)) > projectCut {
				return false
			}
		}
	}
	return true
}

// TestLobeSenseOkEvent: a success is an ok line with no error, no fingerprint and no exit, and its id,
// time and place are those of a failure.
func TestLobeSenseOkEvent(t *testing.T) {
	now := fixedNow()
	e, ok := FromClaudePostTool(Call{Session: "s1", Tool: "Bash", CallID: "toolu_01", Input: json.RawMessage(`{"command":"git status","description":"x"}`)},
		json.RawMessage(`{"stdout":"On branch main","stderr":"","interrupted":false}`), Place{Repo: "r_a172cedc", Cwd: "."}, now, nil, true)
	if !ok {
		t.Fatal("the success was skipped")
	}
	if e.Kind != eventlog.KindOK || e.Host != "claude" || e.Error.String() != "" || e.FP != "" || e.Exit != nil {
		t.Errorf("event = %+v, want kind ok, host claude, no error, fp or exit", e)
	}
	if e.Input.String() != `{"command":"git status"}` {
		t.Errorf("input = %s, want the projection", e.Input.String())
	}
	if e.ID != eventlog.ID("claude", "s1", "toolu_01") || e.Repo != "r_a172cedc" || e.Cwd != "." || e.At != now.UTC().Format(time.RFC3339) {
		t.Errorf("id, repo, cwd or at = %s %s %s %s", e.ID, e.Repo, e.Cwd, e.At)
	}
	if _, ok := FromClaudePostTool(Call{Session: "s1", Tool: "Bash", Input: json.RawMessage(`{}`)},
		json.RawMessage(`"Command running in background with ID: b"`), Place{Cwd: "."}, now, nil, true); ok {
		t.Error("a background start built an event")
	}
}

// TestLobeSenseKeepsFullInput: the exception of S-03 step 5 in its cases.
func TestLobeSenseKeepsFullInput(t *testing.T) {
	fail := func(s, tool string) eventlog.TailEvent {
		return eventlog.TailEvent{Session: s, Tool: tool, Kind: eventlog.KindFail}
	}
	cases := []struct {
		name    string
		session string
		tool    string
		tail    []eventlog.TailEvent
		tailOK  bool
		want    bool
	}{
		{"same session and tool", "s1", "Bash", []eventlog.TailEvent{fail("s1", "Bash")}, true, true},
		{"another session", "s2", "Bash", []eventlog.TailEvent{fail("s1", "Bash")}, true, false},
		{"another tool", "s1", "Edit", []eventlog.TailEvent{fail("s1", "Bash")}, true, false},
		{"a success is not a failure", "s1", "Bash", []eventlog.TailEvent{{Session: "s1", Tool: "Bash", Kind: eventlog.KindOK}}, true, false},
		{"a reference line counts", "s1", "Bash", []eventlog.TailEvent{{Session: "s1", Tool: "Bash", Kind: eventlog.KindFail, Dup: "evt_1"}}, true, true},
		{"a hidden failure counts", "s1", "Bash", []eventlog.TailEvent{{Session: "s1", Tool: "Bash", Kind: eventlog.KindHiddenFail}}, true, true},
		{"a hidden failure of another tool does not", "s1", "Edit", []eventlog.TailEvent{{Session: "s1", Tool: "Bash", Kind: eventlog.KindHiddenFail}}, true, false},
		{"empty session never matches", "", "Bash", []eventlog.TailEvent{fail("", "Bash")}, true, false},
		{"tail unreadable keeps only the projection", "s1", "Bash", nil, false, false},
		{"empty tail", "s1", "Bash", nil, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := KeepsFullInput(c.session, c.tool, c.tail, c.tailOK); got != c.want {
				t.Errorf("KeepsFullInput = %v, want %v", got, c.want)
			}
		})
	}
}
