package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// codexPost is the event a Codex call's finished shell command or tool result arrives on.
const codexPost = evPostToolUse

// codexLogin sets the agent's login to acme's Codex folder (CODEX_HOME) and returns the folder.
func codexLogin(t *testing.T, client string) string {
	t.Helper()
	login := paths.IdentityDir(client, "codex")
	t.Setenv("CODEX_HOME", login)
	return login
}

// codexHookPayload is the JSON Codex sends on PostToolUse for one call: its session, folder, tool, call id,
// session file, input and response. input and response are raw JSON.
func codexHookPayload(t *testing.T, session, cwd, tool, callID, transcript, input, response string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"session_id":      session,
		"transcript_path": transcript,
		"cwd":             cwd,
		"hook_event_name": "PostToolUse",
		"model":           "gpt-test",
		"tool_name":       tool,
		"tool_use_id":     callID,
		"tool_input":      json.RawMessage(input),
		"tool_response":   json.RawMessage(response),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// runCodexHook runs Main as a codex lobe-hook of event with stdin, fails the test unless it is handled
// with code 0, and returns what it wrote to stdout.
func runCodexHook(t *testing.T, event, stdin string) string {
	t.Helper()
	var buf strings.Builder
	handled, code := Main([]string{"lobe-hook", "codex", event}, strings.NewReader(stdin), &buf)
	if !handled || code != 0 {
		t.Fatalf("Main codex %s = handled %v, code %d; want true, 0", event, handled, code)
	}
	return buf.String()
}

// dropLines returns the lines of every drops-*.jsonl file of client's store, in file order.
func dropLines(t *testing.T, client string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(paths.LobeDir(client), "events", "drops-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, splitLines(string(b))...)
	}
	return out
}

// codexEvents are the six events of Codex's hook contract.
func codexEvents() []string {
	return []string{"session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use", "stop", "session-end"}
}

// codexCase is one row of TestLobeCodexPostToolPaths: the session file's exit code for the call (nil: no
// line), the call's tool, input and response, and what must be written: "" for nothing, "drop" for one
// no-exit drop, or a kind (with exit, error and full or projected input).
type codexCase struct {
	name     string
	code     *int
	tool     string
	input    string
	response string
	want     string
	exit     *int
	errText  string
	inputOut string
}

func intPtr(n int) *int { return &n }

// TestLobeCodexPostToolPaths: one case per row of the Codex table (S-02 steps 14 and 15), the own-tool skip,
// and the no-exit drop. Each case checks the kind, exit, error and input of the one line, or that nothing
// was written, and that nothing reached stdout.
func TestLobeCodexPostToolPaths(t *testing.T) {
	cases := []codexCase{
		{name: "code 2 is a fail with the exit and the output", code: intPtr(2),
			tool: "Bash", input: `{"command":"make build","description":"build"}`, response: `"boom"`,
			want: eventlog.KindFail, exit: intPtr(2), errText: "boom", inputOut: `{"command":"make build","description":"build"}`},
		{name: "code 0 not hidden is an ok with exit 0 and the projected input", code: intPtr(0),
			tool: "Bash", input: `{"command":"git status","description":"x"}`, response: `"On branch main"`,
			want: eventlog.KindOK, exit: intPtr(0), inputOut: `{"command":"git status"}`},
		{name: "code 0 hidden by a pipe is a hidden-fail with exit 0", code: intPtr(0),
			tool: "Bash", input: `{"command":"npm test 2>&1 | tail -5"}`, response: `"Tests: 1 failed"`,
			want: eventlog.KindHiddenFail, exit: intPtr(0), errText: "Tests: 1 failed",
			inputOut: `{"command":"npm test 2>&1 | tail -5"}`},
		{name: "no code and Hidden fires is a hidden-fail with no exit",
			tool: "Bash", input: `{"command":"npm test"}`, response: `"Tests: 1 failed"`,
			want: eventlog.KindHiddenFail, errText: "Tests: 1 failed", inputOut: `{"command":"npm test"}`},
		{name: "no code and Hidden does not fire is no event and one drops line",
			tool: "Bash", input: `{"command":"echo hi"}`, response: `"hi"`, want: "drop"},
		{name: "an MCP tool with the error flag is a hidden-fail",
			tool: "mcp__acme__ping", input: `{"host":"db"}`,
			response: `{"content":[{"type":"text","text":"down"}],"isError":true}`,
			want:     eventlog.KindHiddenFail, errText: "down", inputOut: `{"host":"db"}`},
		{name: "apply_patch with Done is nothing and no drops line",
			tool: "apply_patch", input: `{"patch":"x"}`, response: `"Done"`},
		{name: "the memory's own command is nothing and no drops line", code: intPtr(2),
			tool: "Bash", input: `{"command":"tentaqles lobe status"}`, response: `"boom"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			login := codexLogin(t, "acme")
			transcript := codexSessionPath(login)
			callID := "call_p01"
			if c.code != nil {
				appendText(t, transcript, codexExitLine(t, callID, *c.code))
			}
			stdin := codexHookPayload(t, "s1", f.app("acme"), c.tool, callID, transcript, c.input, c.response)
			if out := runCodexHook(t, codexPost, stdin); out != "" {
				t.Fatalf("stdout %q, want nothing", out)
			}
			evs := lines(t, "acme")
			drops := dropLines(t, "acme")
			switch c.want {
			case "":
				if len(evs) != 0 || len(drops) != 0 {
					t.Fatalf("%d event lines and %d drops lines, want none", len(evs), len(drops))
				}
			case "drop":
				if len(evs) != 0 || len(drops) != 1 || !strings.Contains(drops[0], `"why":"no-exit"`) {
					t.Fatalf("%d event lines and drops %v, want none and one no-exit line", len(evs), drops)
				}
			default:
				if len(drops) != 0 {
					t.Fatalf("a drops line was written for a recorded call: %v", drops)
				}
				if len(evs) != 1 {
					t.Fatalf("%d event lines, want 1", len(evs))
				}
				got := decodeLogged(t, evs)[0]
				if got.Host != "codex" || got.Kind != c.want {
					t.Errorf("host, kind = %s, %s; want codex, %s", got.Host, got.Kind, c.want)
				}
				if (got.Exit == nil) != (c.exit == nil) || (got.Exit != nil && *got.Exit != *c.exit) {
					t.Errorf("exit = %v, want %v", got.Exit, c.exit)
				}
				if got.Error != c.errText {
					t.Errorf("error = %q, want %q", got.Error, c.errText)
				}
				if got.Input != c.inputOut {
					t.Errorf("input = %s, want %s", got.Input, c.inputOut)
				}
			}
		})
	}
}

// TestLobeCodexCaptureOff: with capture off, a failing shell call and a call with no code write nothing,
// no drops line and no store folder.
func TestLobeCodexCaptureOff(t *testing.T) {
	for _, name := range []string{"a failed call", "a call with no code"} {
		t.Run(name, func(t *testing.T) {
			f := newHome(t)
			login := codexLogin(t, "acme")
			appendManifestLobe(t, f, "acme", "capture: off")
			transcript := codexSessionPath(login)
			cmd := `{"command":"echo hi"}`
			if name == "a failed call" {
				cmd = `{"command":"make build"}`
				appendText(t, transcript, codexExitLine(t, "call_off", 2))
			}
			stdin := codexHookPayload(t, "s1", f.app("acme"), "Bash", "call_off", transcript, cmd, `"boom"`)
			if out := runCodexHook(t, codexPost, stdin); out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if n := len(lines(t, "acme")); n != 0 {
				t.Errorf("%d event lines with capture off, want 0", n)
			}
			if n := len(dropLines(t, "acme")); n != 0 {
				t.Errorf("%d drops lines with capture off, want 0", n)
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("a lobe folder was created with capture off")
			}
		})
	}
}

// TestLobeCodexNoExitCounted: a Bash call with no session line and plain output adds exactly one no-exit
// line to the drops file and no event line.
func TestLobeCodexNoExitCounted(t *testing.T) {
	f := newHome(t)
	login := codexLogin(t, "acme")
	stdin := codexHookPayload(t, "s1", f.app("acme"), "Bash", "call_none", codexSessionPath(login),
		`{"command":"echo hi"}`, `"hi"`)
	runCodexHook(t, codexPost, stdin)
	drops := dropLines(t, "acme")
	if len(drops) != 1 {
		t.Fatalf("%d drops lines, want exactly 1: %v", len(drops), drops)
	}
	var d struct {
		At  string `json:"at"`
		Why string `json:"why"`
	}
	if err := json.Unmarshal([]byte(drops[0]), &d); err != nil || d.Why != eventlog.DropNoExit || d.At == "" {
		t.Errorf("drops line = %s, want at and why no-exit", drops[0])
	}
	if n := len(lines(t, "acme")); n != 0 {
		t.Errorf("%d event lines, want 0", n)
	}
}

// TestLobeCodexSameFingerprintAsClaude: one error body through the Claude failure path and through the
// Codex path gives the same fingerprint. The two calls use two sessions, so neither is a repeat of the other.
func TestLobeCodexSameFingerprintAsClaude(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	runHook(t, evPost, payloadJSON("s-claude", f.app("acme"), "Bash", "toolu_fp1", "make build", "Exit code 2\nboom"))

	login := codexLogin(t, "acme")
	transcript := codexSessionPath(login)
	appendText(t, transcript, codexExitLine(t, "call_fp1", 2))
	runCodexHook(t, codexPost, codexHookPayload(t, "s-codex", f.app("acme"), "Bash", "call_fp1", transcript,
		`{"command":"make build"}`, `"boom"`))

	fps := map[string]string{}
	for _, l := range lines(t, "acme") {
		var e struct {
			Host string `json:"host"`
			FP   string `json:"fp"`
		}
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		fps[e.Host] = e.FP
	}
	if fps["claude"] == "" || fps["codex"] == "" {
		t.Fatalf("fingerprints %v, want one for claude and one for codex", fps)
	}
	if fps["claude"] != fps["codex"] {
		t.Errorf("claude fp %s, codex fp %s; want the same", fps["claude"], fps["codex"])
	}
}

// TestLobeCodexNeedsTheClientsLogin: with CODEX_HOME unset, or set to another client's Codex folder, a
// failed shell call writes no event line, no drops line and no store folder, even with a session file that
// holds its code. CLAUDE_CONFIG_DIR does not stand in for CODEX_HOME (TestLobeHookLoginUnsetDoesNothing).
func TestLobeCodexNeedsTheClientsLogin(t *testing.T) {
	cases := []struct {
		name  string
		login func(t *testing.T, f fixture) string
	}{
		{"CODEX_HOME unset", func(t *testing.T, f fixture) string { return "" }},
		{"CODEX_HOME is globex's Codex folder", func(t *testing.T, f fixture) string {
			return paths.IdentityDir("globex", "codex")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			login := c.login(t, f)
			t.Setenv("CODEX_HOME", login)
			// The session file sits under the folder the call would need, whichever login is set.
			transcript := codexSessionPath(paths.IdentityDir("acme", "codex"))
			appendText(t, transcript, codexExitLine(t, "call_nl", 2))
			stdin := codexHookPayload(t, "s1", f.app("acme"), "Bash", "call_nl", transcript,
				`{"command":"make build"}`, `"boom"`)
			if out := runCodexHook(t, codexPost, stdin); out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if n := len(lines(t, "acme")); n != 0 {
				t.Errorf("%d event lines, want 0", n)
			}
			if n := len(dropLines(t, "acme")); n != 0 {
				t.Errorf("%d drops lines, want 0", n)
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("a lobe folder was created")
			}
		})
	}
}

// TestLobeCodexStopIsNoStep: the Codex stop event has no step. A Claude transcript with a rejected call,
// read under the Claude login, is not read for Codex, so nothing is recorded and no store is created.
func TestLobeCodexStopIsNoStep(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	codexLogin(t, "acme")
	app := f.app("acme")
	T := acmeTranscript()
	appendText(t, T, rejectedEdit(t, "toolu_codex_stop", "ZZZ"))
	if out := runCodexHook(t, "stop", stopJSON(t, app, T)); out != "" {
		t.Fatalf("stdout %q, want nothing", out)
	}
	if n := len(lines(t, "acme")); n != 0 {
		t.Fatalf("%d event lines from a Codex stop, want 0", n)
	}
	if exists(paths.LobeDir("acme")) {
		t.Fatalf("a lobe folder was created by a Codex stop")
	}
}

// TestLobeCodexSessionEndSweeps: `tentaqles lobe-hook codex session-end` deletes an expired day file of the
// client whose CODEX_HOME it is, and nothing of another client. With globex's CODEX_HOME in acme's folder
// neither file goes; with acme's CODEX_HOME only acme's file goes. Nothing reaches stdout.
func TestLobeCodexSessionEndSweeps(t *testing.T) {
	f := newHome(t)
	mustMkdir(t, paths.IdentityDir("acme", "codex"))
	mustMkdir(t, paths.IdentityDir("globex", "codex"))
	makeStore(t, "acme")
	makeStore(t, "globex")
	acmeOld := filepath.Join(eventsDir("acme"), expiredDay)
	globexOld := filepath.Join(eventsDir("globex"), expiredDay)
	mustWrite(t, acmeOld, "")
	mustWrite(t, globexOld, "")
	end := sessionEndJSON(f.app("acme"))

	t.Setenv("CODEX_HOME", paths.IdentityDir("globex", "codex"))
	if out := runCodexHook(t, "session-end", end); out != "" {
		t.Fatalf("stdout %q, want nothing", out)
	}
	if !exists(acmeOld) || !exists(globexOld) {
		t.Fatalf("globex's CODEX_HOME in acme's folder deleted a day file")
	}

	codexLogin(t, "acme")
	if out := runCodexHook(t, "session-end", end); out != "" {
		t.Fatalf("stdout %q, want nothing", out)
	}
	if exists(acmeOld) {
		t.Errorf("acme's expired day file is still there after acme's session end")
	}
	if !exists(globexOld) {
		t.Errorf("globex's expired day file was deleted by acme's session end")
	}
}
