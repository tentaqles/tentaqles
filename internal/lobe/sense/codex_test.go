package sense

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
)

// TestLobeCodexText checks codexText in its three forms (a string, a list of parts, an object's content),
// and the text an object without content gives.
func TestLobeCodexText(t *testing.T) {
	cases := []struct {
		name     string
		response string
		want     string
	}{
		{"a JSON string is the text itself", `"boom"`, "boom"},
		{"a JSON string keeps its escapes decoded", `"line one\nline two"`, "line one\nline two"},
		{"a list of parts joins the text values by line breaks",
			`[{"type":"text","text":"first"},{"type":"image","url":"x"},{"type":"text","text":"second"}]`,
			"first\nsecond"},
		{"an object gives its content, read the same way",
			`{"content":[{"type":"text","text":"down"}],"isError":true}`, "down"},
		{"an object with no content gives nothing", `{"other":"x"}`, ""},
		{"no response gives nothing", ``, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := codexText(json.RawMessage(c.response)); got != c.want {
				t.Errorf("codexText(%s) = %q, want %q", c.response, got, c.want)
			}
		})
	}
}

// codexCall is a Codex call of tool with input and a call id.
func codexCall(tool, input string) Call {
	return Call{Session: "s1", Tool: tool, CallID: "call_01", Input: json.RawMessage(input)}
}

// TestLobeCodexPostToolCases checks each case of FromCodexPostTool: its kind, exit, input, error, host and
// whether it writes or counts a no-exit drop.
func TestLobeCodexPostToolCases(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		name     string
		call     Call
		response string
		code     int
		haveCode bool
		write    bool
		noExit   bool
		kind     string
		exit     *int
		input    string
		errText  string
	}{
		{name: "Bash code not 0 is a fail with the code and the output",
			call: codexCall("Bash", `{"command":"make build","description":"build"}`), response: `"boom"`,
			code: 2, haveCode: true, write: true, kind: eventlog.KindFail, exit: intp(2),
			input: `{"command":"make build","description":"build"}`, errText: "boom"},
		{name: "Bash code 0 not hidden is an ok with exit 0 and the projected input",
			call: codexCall("Bash", `{"command":"git status","description":"x"}`), response: `"On branch main"`,
			code: 0, haveCode: true, write: true, kind: eventlog.KindOK, exit: intp(0),
			input: `{"command":"git status"}`},
		{name: "Bash code 0 hidden by a pipe is a hidden-fail with exit 0",
			call: codexCall("Bash", `{"command":"npm test 2>&1 | tail -5"}`), response: `"Tests: 1 failed"`,
			code: 0, haveCode: true, write: true, kind: eventlog.KindHiddenFail, exit: intp(0),
			input: `{"command":"npm test 2>&1 | tail -5"}`, errText: "Tests: 1 failed"},
		{name: "Bash with no code and Hidden firing is a hidden-fail with no exit",
			call: codexCall("Bash", `{"command":"npm test"}`), response: `"Tests: 1 failed"`,
			write: true, kind: eventlog.KindHiddenFail, input: `{"command":"npm test"}`, errText: "Tests: 1 failed"},
		{name: "Bash with no code and Hidden not firing writes nothing and counts a no-exit drop",
			call: codexCall("Bash", `{"command":"echo hi"}`), response: `"hi"`,
			noExit: true},
		{name: "an MCP tool with the error flag is a hidden-fail with no exit and the full input",
			call: codexCall("mcp__acme__ping", `{"host":"db"}`), response: `{"content":[{"type":"text","text":"down"}],"isError":true}`,
			write: true, kind: eventlog.KindHiddenFail, input: `{"host":"db"}`, errText: "down"},
		{name: "a tool with no error flag writes nothing and counts no drop",
			call: codexCall("apply_patch", `{"patch":"x"}`), response: `"Done"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, write, noExit := FromCodexPostTool(c.call, json.RawMessage(c.response), c.code, c.haveCode,
				firstPlace(), now, nil, true)
			if write != c.write || noExit != c.noExit {
				t.Fatalf("write, noExit = %v, %v; want %v, %v", write, noExit, c.write, c.noExit)
			}
			if !write {
				return
			}
			if ev.Host != "codex" || ev.Kind != c.kind {
				t.Errorf("host, kind = %s, %s; want codex, %s", ev.Host, ev.Kind, c.kind)
			}
			if (ev.Exit == nil) != (c.exit == nil) || (ev.Exit != nil && *ev.Exit != *c.exit) {
				t.Errorf("exit = %v, want %v", ev.Exit, c.exit)
			}
			if ev.Input.String() != c.input {
				t.Errorf("input = %s, want %s", ev.Input.String(), c.input)
			}
			if ev.Error.String() != c.errText {
				t.Errorf("error = %q, want %q", ev.Error.String(), c.errText)
			}
			if ev.Repo != "r_a172cedc" || ev.Cwd != "." || ev.At != now.UTC().Format(time.RFC3339) {
				t.Errorf("repo, cwd or at = %s %s %s", ev.Repo, ev.Cwd, ev.At)
			}
		})
	}
}

// TestLobeCodexOkInputIsTheSuccessForm: an ok event's input is the same bytes a Claude success of the same
// call would give, so the two hosts store one call the same way.
func TestLobeCodexOkInputIsTheSuccessForm(t *testing.T) {
	input := `{"command":"git status","description":"x"}`
	ev, write, _ := FromCodexPostTool(codexCall("Bash", input), json.RawMessage(`"ok"`), 0, true,
		firstPlace(), fixedNow(), nil, true)
	if !write {
		t.Fatal("a code-0 call wrote nothing")
	}
	claude, ok := FromClaudePostTool(Call{Session: "s1", Tool: "Bash", CallID: "call_01", Input: json.RawMessage(input)},
		json.RawMessage(`{"stdout":"ok","stderr":"","interrupted":false}`), firstPlace(), fixedNow(), nil, true)
	if !ok {
		t.Fatal("the Claude success was skipped")
	}
	if ev.Input.String() != claude.Input.String() {
		t.Errorf("codex input %s, claude input %s", ev.Input.String(), claude.Input.String())
	}
}

// TestLobeCodexIDs: a call with an id gets eventlog.ID under host codex; a call without one gets the id
// without a call id, so two calls with the same content at different times differ.
func TestLobeCodexIDs(t *testing.T) {
	now := fixedNow()
	ev, _, _ := FromCodexPostTool(codexCall("Bash", `{"command":"make"}`), json.RawMessage(`"boom"`), 2, true,
		firstPlace(), now, nil, true)
	if ev.ID != eventlog.ID("codex", "s1", "call_01") {
		t.Errorf("id = %s, want the codex id of call_01", ev.ID)
	}
	noID := Call{Session: "s1", Tool: "Bash", Input: json.RawMessage(`{"command":"make"}`)}
	a, _, _ := FromCodexPostTool(noID, json.RawMessage(`"boom"`), 2, true, firstPlace(), now, nil, true)
	b, _, _ := FromCodexPostTool(noID, json.RawMessage(`"boom"`), 2, true, firstPlace(), now.Add(1), nil, true)
	if a.ID != eventlog.IDWithoutCall("codex", "s1", "Bash", eventlog.KindFail, a.Input.String(), now.UnixNano()) {
		t.Errorf("id without a call id = %s", a.ID)
	}
	if a.ID == b.ID {
		t.Error("two calls without ids at different times share an id")
	}
}

// TestLobeCodexFingerprintMatchesClaude: one error body gives the same fingerprint through the Claude
// failure path and the Codex path, so the repeat check and the fingerprint read both hosts alike.
func TestLobeCodexFingerprintMatchesClaude(t *testing.T) {
	codexEv, _, _ := FromCodexPostTool(codexCall("Bash", `{"command":"make build"}`), json.RawMessage(`"boom"`),
		2, true, firstPlace(), fixedNow(), nil, true)
	claudeEv := FromClaudeFailure(Call{Session: "s1", Tool: "Bash", CallID: "toolu_01",
		Input: json.RawMessage(`{"command":"make build"}`), Error: "Exit code 2\nboom"}, firstPlace(), fixedNow(), nil, true)
	want, _ := fingerprint.Of("Bash", claudeEv.Error.String())
	got, _ := fingerprint.Of("Bash", codexEv.Error.String())
	if got == "" || got != want {
		t.Errorf("codex fp %q, claude fp %q", got, want)
	}
}

// TestLobeCodexGrepFoundNothingIsOK: a grep that exits 1 with no output is an ok line with exit 1, as
// S-01 writes it for Claude Code; the same exit with output is still a fail.
func TestLobeCodexGrepFoundNothingIsOK(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	ev, write, _ := FromCodexPostTool(codexCall("Bash", `{"command":"grep -q foo app.go"}`), json.RawMessage(`""`),
		1, true, firstPlace(), now, nil, true)
	if !write || ev.Kind != eventlog.KindOK || ev.Exit == nil || *ev.Exit != 1 || ev.Error.String() != "" {
		t.Errorf("empty grep: write %v kind %q exit %v error %q; want ok, exit 1, no error", write, ev.Kind, ev.Exit, ev.Error.String())
	}
	ev, write, _ = FromCodexPostTool(codexCall("Bash", `{"command":"grep foo missing.go"}`),
		json.RawMessage(`"grep: missing.go: No such file or directory"`), 1, true, firstPlace(), now, nil, true)
	if !write || ev.Kind != eventlog.KindFail {
		t.Errorf("grep with an error: write %v kind %q; want fail", write, ev.Kind)
	}
}
