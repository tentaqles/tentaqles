package hook

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// successPayload is the JSON Claude Code sends for a successful tool call (PostToolUse).
type successPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolResponse   json.RawMessage `json:"tool_response"`
}

// successJSON builds a PostToolUse payload: input and response are raw JSON.
func successJSON(session, cwd, tool, callID, input, response string) string {
	p := successPayload{
		SessionID:      session,
		TranscriptPath: "/x/t.jsonl",
		Cwd:            cwd,
		PermissionMode: "default",
		HookEventName:  "PostToolUse",
		ToolName:       tool,
		ToolInput:      json.RawMessage(input),
		ToolUseID:      callID,
		ToolResponse:   json.RawMessage(response),
	}
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// bashOK is a successful Bash result in the owner's capture shape.
const bashOK = `{"stdout":"OUTPUT-MARKER-7731 on branch main","stderr":"","interrupted":false}`

// lobeFiles lists every file under client's lobe folder, relative to it, with its bytes. A missing folder
// lists as nothing.
func lobeFiles(t *testing.T, client string) map[string]string {
	t.Helper()
	root := paths.LobeDir(client)
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(b)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// TestLobeSenseOkDropsOutput: a success's response is processed, and its output appears in no file under
// the lobe folder.
func TestLobeSenseOkDropsOutput(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")

	stdout := runHook(t, evPostToolUse, successJSON("s1", app, "Bash", "toolu_01",
		`{"command":"git status"}`, bashOK))
	stdout += runHook(t, evPostToolUse, successJSON("s1", app, "Write", "toolu_02",
		`{"file_path":"notes.txt","content":"CONTENT-MARKER-1"}`,
		`{"filePath":"notes.txt","note":"OUTPUT-MARKER-7731"}`))
	if stdout != "" {
		t.Errorf("stdout %q, want nothing", stdout)
	}

	ls := lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("after two successes: %d lines, want 2", len(ls))
	}
	for _, e := range decodeLogged(t, ls) {
		if e.Kind != "ok" || e.Error != "" || e.Exit != nil {
			t.Errorf("line %+v: want kind ok, no error and no exit", e)
		}
	}
	hits := 0
	for name, body := range lobeFiles(t, "acme") {
		if n := strings.Count(body, "OUTPUT-MARKER-7731"); n > 0 {
			t.Errorf("%s holds the output marker %d times", name, n)
			hits += n
		}
	}
	if hits != 0 {
		t.Fatalf("the output marker appears %d times under the lobe folder, want 0", hits)
	}
}

// TestLobeSenseOkFullAfterFailure: after a failure of a tool in a session, a success of the same tool in
// that session keeps its full input; the same success in another session keeps the projection.
func TestLobeSenseOkFullAfterFailure(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	a250 := strings.Repeat("a", 250)
	a200 := strings.Repeat("a", 200)
	ok := func(session, callID string) string {
		return successJSON(session, app, "mcp__acme__ping", callID,
			fmt.Sprintf(`{"q":%q}`, a250), `"pong"`)
	}

	// Another session first: its success has no failure of its own in the tail.
	runHook(t, evPostToolUse, ok("s2", "toolu_01"))
	runHook(t, evPost, failureFor("s1", app, "mcp__acme__ping", "toolu_02"))
	runHook(t, evPostToolUse, ok("s1", "toolu_03"))
	runHook(t, evPostToolUse, ok("s3", "toolu_04"))

	got := decodeLogged(t, lines(t, "acme"))
	if len(got) != 4 {
		t.Fatalf("%d lines, want 4", len(got))
	}
	if got[0].Input != `{"q":"`+a200+`"}` {
		t.Errorf("success in s2 (no failure): input %q, want the projection", got[0].Input)
	}
	if got[2].Kind != "ok" || got[2].Input != `{"q":"`+a250+`"}` {
		t.Errorf("success in s1 after its failure: %+v, want the full input", got[2])
	}
	if got[3].Input != `{"q":"`+a200+`"}` {
		t.Errorf("success in s3 (no failure in s3): input %q, want the projection", got[3].Input)
	}
}

// failureFor is a failed MCP call of the given session and tool, as PostToolUseFailure sends it.
func failureFor(session, app, tool, callID string) string {
	return rawPayloadJSON(session, app, tool, callID, `{"q":"x"}`, "widget 4411 is not frobnicated")
}

// TestLobeSenseBackgroundStartWritesNothing: a shell command that only went to the background writes no
// line and creates no store folder.
func TestLobeSenseBackgroundStartWritesNothing(t *testing.T) {
	cases := []struct {
		name, tool, response string
	}{
		{"run in background", "Bash", `"Command running in background with ID: bash_1"`},
		{"moved to the background", "Bash", `"Command did not complete within its 300s timeout and was moved to the background (ID: b1)"`},
		{"PowerShell object result", "PowerShell", `{"stdout":"Command running in background with ID: ps_1","stderr":""}`},
		{"captured shape: task id", "Bash", `{"stdout":"","stderr":"","interrupted":false,"isImage":false,"noOutputExpected":false,"backgroundTaskId":"bmx1"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			setLogin(t, f, "acme")
			stdin := successJSON("s1", f.app("acme"), c.tool, "toolu_05", `{"command":"npm run dev"}`, c.response)
			if out := runHook(t, evPostToolUse, stdin); out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if n := len(lines(t, "acme")); n != 0 {
				t.Errorf("%d lines, want 0", n)
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("a lobe folder was created for a background start")
			}
		})
	}
}

// TestLobeSenseOkEveryCall: 100 successful calls with no failure write 100 lines, and the median line of a
// mixed fixture of 100 calls is under 300 bytes.
func TestLobeSenseOkEveryCall(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	const n = 100
	for i := 0; i < n; i++ {
		var stdin string
		// Distinct inputs throughout, so no call is a repeat of another (S-06).
		switch i % 5 {
		case 0:
			stdin = successJSON("s1", app, "Bash", fmt.Sprintf("toolu_%03d", i),
				fmt.Sprintf(`{"command":"npm run check-%03d","description":"check"}`, i), bashOK)
		case 1:
			stdin = successJSON("s1", app, "Write", fmt.Sprintf("toolu_%03d", i),
				fmt.Sprintf(`{"file_path":"notes-%03d.txt","content":"text %03d"}`, i, i),
				fmt.Sprintf(`{"filePath":"notes-%03d.txt","note":"done"}`, i))
		case 2:
			stdin = successJSON("s1", app, "Read", fmt.Sprintf("toolu_%03d", i),
				fmt.Sprintf(`{"file_path":"src/file-%03d.go","offset":%d}`, i, i), `"contents"`)
		case 3:
			stdin = successJSON("s1", app, "Grep", fmt.Sprintf("toolu_%03d", i),
				fmt.Sprintf(`{"pattern":"symbol%03d","path":"src"}`, i), `"src/a.go:3:symbol"`)
		default:
			stdin = successJSON("s1", app, "mcp__acme__ping", fmt.Sprintf("toolu_%03d", i),
				fmt.Sprintf(`{"n":%d}`, i), `"pong"`)
		}
		runHook(t, evPostToolUse, stdin)
	}
	ls := lines(t, "acme")
	if len(ls) != n {
		t.Fatalf("%d successful calls wrote %d lines, want %d", n, len(ls), n)
	}
	sizes := make([]int, len(ls))
	for i, l := range ls {
		sizes[i] = len(l)
	}
	sort.Ints(sizes)
	if median := sizes[len(sizes)/2]; median >= 300 {
		t.Errorf("median ok line is %d bytes, want under 300", median)
	}
}

// TestLobeSenseOkSkips: a success that the hook must not record writes nothing in any store, exits 0 and
// prints nothing, in five cases: bad JSON, no client, another client's login, no login variable, capture
// off.
func TestLobeSenseOkSkips(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f fixture) string
	}{
		{"not json", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			return "not json"
		}},
		{"no client", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			return successJSON("s1", f.tmp, "Bash", "toolu_01", `{"command":"npm test"}`, bashOK)
		}},
		{"login names another client", func(t *testing.T, f fixture) string {
			t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir("globex", "claude"))
			return successJSON("s1", f.app("acme"), "Bash", "toolu_01", `{"command":"npm test"}`, bashOK)
		}},
		{"login empty", func(t *testing.T, f fixture) string {
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			return successJSON("s1", f.app("acme"), "Bash", "toolu_01", `{"command":"npm test"}`, bashOK)
		}},
		{"capture off", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			appendManifestLobe(t, f, "acme", "capture: off")
			return successJSON("s1", f.app("acme"), "Bash", "toolu_01", `{"command":"npm test"}`, bashOK)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			stdin := c.setup(t, f)
			before := identityTrees(t, f)
			if out := runHook(t, evPostToolUse, stdin); out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if after := identityTrees(t, f); after != before {
				t.Errorf("an identity folder changed:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("a lobe folder was created for acme")
			}
		})
	}
}
