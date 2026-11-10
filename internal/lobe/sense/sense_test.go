package sense

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

func fixedNow() time.Time { return time.Date(2026, 11, 9, 14, 3, 11, 0, time.UTC) }

func intp(n int) *int { return &n }

// openClient opens the store of a client whose identity folder is created first.
func openClient(t *testing.T, name string) *store.Store {
	t.Helper()
	dir := filepath.Join(os.Getenv("TQ_HOME"), "identities", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(name, store.ReadWrite)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	return s
}

// setup isolates TQ_HOME and opens the store of client acme.
func setup(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	return openClient(t, "acme")
}

func firstCall() Call {
	return Call{
		Session: "s1",
		Cwd:     "/work/app",
		Tool:    "Bash",
		CallID:  "toolu_01",
		Input:   json.RawMessage(`{"command":"npm run buld"}`),
		Error:   "Exit code 1\nnpm error Missing script: buld",
	}
}

func firstPlace() Place { return Place{Repo: "r_a172cedc", Cwd: "."} }

func TestLobeSenseFromClaudeFailure(t *testing.T) {
	e := FromClaudeFailure(firstCall(), firstPlace(), fixedNow())
	if e.ID != "evt_9bde2e09" {
		t.Errorf("ID = %q, want evt_9bde2e09", e.ID)
	}
	if e.V != 1 {
		t.Errorf("V = %d, want 1", e.V)
	}
	if e.At != "2026-11-09T14:03:11Z" {
		t.Errorf("At = %q", e.At)
	}
	if e.Host != "claude" || e.Tool != "Bash" || e.Kind != "fail" || e.Session != "s1" {
		t.Errorf("Host/Tool/Kind/Session = %q/%q/%q/%q", e.Host, e.Tool, e.Kind, e.Session)
	}
	if e.Repo != "r_a172cedc" || e.Cwd != "." {
		t.Errorf("Repo/Cwd = %q/%q", e.Repo, e.Cwd)
	}
	if got := e.Input.String(); got != `{"command":"npm run buld"}` {
		t.Errorf("Input = %q", got)
	}
	if got := e.Error.String(); got != "npm error Missing script: buld" {
		t.Errorf("Error = %q", got)
	}
	if e.Exit == nil || *e.Exit != 1 {
		t.Errorf("Exit = %v, want 1", e.Exit)
	}
}

func TestLobeSenseExitCode(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		err      string
		wantExit *int
		wantErr  string
	}{
		{"exit 2 with a line", "Bash", "Exit code 2\nNo such file", intp(2), "No such file"},
		{"exit alone", "Bash", "Exit code 127", intp(127), ""},
		{"crlf", "Bash", "Exit code 1\r\nboom", intp(1), "boom"},
		{"no exit line", "Bash", "make: nothing to do", nil, "make: nothing to do"},
		{"not a number", "Bash", "Exit code abc\nx", nil, "Exit code abc\nx"},
		{"leading spaces", "Bash", "  Exit code 1\nx", nil, "  Exit code 1\nx"},
		{"exit zero", "Bash", "Exit code 0\nwarning", intp(0), "warning"},
		{"powershell", "PowerShell", "Exit code 1\ngti is not recognized", intp(1), "gti is not recognized"},
		{"edit is not a shell", "Edit", "Exit code 1\nString to replace not found", nil, "Exit code 1\nString to replace not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Call{Session: "s", Tool: tc.tool, CallID: "x", Input: json.RawMessage(`{"command":"ls"}`), Error: tc.err}
			e := FromClaudeFailure(c, Place{Cwd: "."}, fixedNow())
			if (e.Exit == nil) != (tc.wantExit == nil) {
				t.Fatalf("Exit = %v, want %v", e.Exit, tc.wantExit)
			}
			if e.Exit != nil && *e.Exit != *tc.wantExit {
				t.Errorf("Exit = %d, want %d", *e.Exit, *tc.wantExit)
			}
			if got := e.Error.String(); got != tc.wantErr {
				t.Errorf("Error = %q, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestLobeSenseSecretsNeverInTheEvent(t *testing.T) {
	cmd := `curl -H "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345" https://api.example.test/v1/items?api_key=Zx81kQpL0029aaBB`
	input, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		t.Fatal(err)
	}
	c := Call{
		Session: "s1",
		Tool:    "Bash",
		CallID:  "toolu_02",
		Input:   input,
		Error:   "password=hunter2 rejected for postgres://app:s3cretPW@db.example.test:5432/main",
	}
	e := FromClaudeFailure(c, Place{Cwd: "."}, fixedNow())
	texts := []string{e.Input.String(), e.Error.String(), CallText(c.Tool, c.Input)}
	secrets := []string{"abcdefghijklmnopqrstuvwxyz012345", "Zx81kQpL0029aaBB", "hunter2", "s3cretPW"}
	for _, text := range texts {
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Errorf("text %q holds the secret %q", text, secret)
			}
		}
	}
}

func TestLobeSenseInputForms(t *testing.T) {
	want := `{"command":"deploy","note":"hi"}`
	obj := Call{Session: "s1", Tool: "Bash", CallID: "a", Input: json.RawMessage(`{"note":"hi","command":"deploy"}`)}
	str := Call{Session: "s1", Tool: "Bash", CallID: "a", Input: json.RawMessage(`"{\"command\":\"deploy\",\"note\":\"hi\"}"`)}
	if got := FromClaudeFailure(obj, Place{Cwd: "."}, fixedNow()).Input.String(); got != want {
		t.Errorf("object input = %q, want %q", got, want)
	}
	if got := FromClaudeFailure(str, Place{Cwd: "."}, fixedNow()).Input.String(); got != want {
		t.Errorf("string input = %q, want %q", got, want)
	}

	empty := Call{Session: "s1", Tool: "Bash", CallID: "a"}
	if got := FromClaudeFailure(empty, Place{Cwd: "."}, fixedNow()).Input.String(); got != "{}" {
		t.Errorf("empty input = %q, want {}", got)
	}

	text := Call{Session: "s1", Tool: "Bash", CallID: "a", Input: json.RawMessage(`"just text"`)}
	if got := FromClaudeFailure(text, Place{Cwd: "."}, fixedNow()).Input.String(); got != `"just text"` {
		t.Errorf("text input = %q, want a JSON string", got)
	}
}

func TestLobeSenseMissingFields(t *testing.T) {
	now := fixedNow()

	noTool := Call{Session: "s1", CallID: "a", Input: json.RawMessage(`{}`)}
	if e := FromClaudeFailure(noTool, Place{Cwd: "."}, now); e.Tool != "unknown" {
		t.Errorf("Tool = %q, want unknown", e.Tool)
	}

	noSession := Call{Tool: "Bash", CallID: "a", Input: json.RawMessage(`{}`)}
	if e := FromClaudeFailure(noSession, Place{Cwd: "."}, now); e.Session != "" {
		t.Errorf("Session = %q, want empty", e.Session)
	}

	bare := Call{Session: "s1", Tool: "Bash", Input: json.RawMessage(`{"command":"ls"}`)}
	e1 := FromClaudeFailure(bare, Place{Cwd: "."}, now)
	e2 := FromClaudeFailure(bare, Place{Cwd: "."}, now.Add(time.Nanosecond))
	if e1.ID == e2.ID {
		t.Fatalf("two events 1ns apart share id %q", e1.ID)
	}
	noCall := eventlog.ID("claude", "s1", "")
	for _, id := range []string{e1.ID, e2.ID} {
		if len(id) != len("evt_")+8 || !strings.HasPrefix(id, "evt_") {
			t.Errorf("id %q is not evt_ plus 8 characters", id)
		}
		for _, r := range strings.TrimPrefix(id, "evt_") {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Errorf("id %q has a non-hex character", id)
			}
		}
		if id == noCall {
			t.Errorf("id %q equals the id of an empty call id", id)
		}
	}
}

func TestLobeSenseLocate(t *testing.T) {
	c := t.TempDir()
	for _, d := range []string{
		filepath.Join(c, "app", ".git"),
		filepath.Join(c, "app", "src", "etl"),
		filepath.Join(c, "docs"),
		filepath.Join(c, "wt"),
	} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(c, "wt", ".git"), []byte("gitdir: elsewhere\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const appKey = "r_a172cedc"

	checks := []struct {
		name string
		cwd  string
		want Place
	}{
		{"repo root", filepath.Join(c, "app"), Place{Repo: appKey, Cwd: "."}},
		{"nested in repo", filepath.Join(c, "app", "src", "etl"), Place{Repo: appKey, Cwd: "src/etl"}},
		{"client folder", c, Place{Cwd: "."}},
		{"folder without git", filepath.Join(c, "docs"), Place{Cwd: "docs"}},
		{"missing folder in repo", filepath.Join(c, "app", "not", "there", "yet"), Place{Repo: appKey, Cwd: "not/there/yet"}},
		{"outside the client", t.TempDir(), Place{Cwd: "."}},
	}
	for _, tc := range checks {
		if got := Locate(c, tc.cwd); got != tc.want {
			t.Errorf("%s: Locate = %+v, want %+v", tc.name, got, tc.want)
		}
	}

	wt := Locate(c, filepath.Join(c, "wt"))
	if wt.Repo == "" || wt.Repo != store.RepoKey(c, filepath.Join(c, "wt")) || wt.Cwd != "." {
		t.Errorf("git file: Locate = %+v", wt)
	}

	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(filepath.Join(c, "app"))
		if got := Locate(c, upper); got != (Place{Repo: appKey, Cwd: "."}) {
			t.Errorf("upper-case cwd: Locate = %+v", got)
		}
	}

	// A repository above the client folder is never found.
	parent := t.TempDir()
	client := filepath.Join(parent, "client")
	if err := os.MkdirAll(filepath.Join(client, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := Locate(client, filepath.Join(client, "docs")); got != (Place{Cwd: "docs"}) {
		t.Errorf("above the client: Locate = %+v, want {Cwd:docs}", got)
	}
}

// TestLobeSenseWorktreeKey: a linked worktree is keyed by its main repository, its cwd stays relative
// to the worktree, and a submodule keeps its own key. No process may be started, so PATH is empty.
func TestLobeSenseWorktreeKey(t *testing.T) {
	t.Setenv("PATH", "")
	c := t.TempDir()
	for _, f := range []struct{ path, content string }{
		{filepath.Join(c, "app", ".git", "worktrees", "fix", "commondir"), "../..\n"},
		{filepath.Join(c, "wt", "fix", ".git"), "gitdir: " + filepath.Join(c, "app", ".git", "worktrees", "fix") + "\n"},
		{filepath.Join(c, "sub", ".git"), "gitdir: " + filepath.Join(c, "subgit") + "\n"},
		{filepath.Join(c, "subgit", "placeholder"), ""},
	} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.path, []byte(f.content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(c, "wt", "fix", "src"), 0700); err != nil {
		t.Fatal(err)
	}
	const appKey = "r_a172cedc"

	if got := Locate(c, filepath.Join(c, "wt", "fix")); got != (Place{Repo: appKey, Cwd: "."}) {
		t.Errorf("worktree root: Locate = %+v, want {Repo:%s Cwd:.}", got, appKey)
	}
	if got := Locate(c, filepath.Join(c, "wt", "fix", "src")); got != (Place{Repo: appKey, Cwd: "src"}) {
		t.Errorf("worktree src: Locate = %+v, want {Repo:%s Cwd:src}", got, appKey)
	}
	if got := Locate(c, filepath.Join(c, "sub")); got != (Place{Repo: "r_ddc6e2b2", Cwd: "."}) {
		t.Errorf("submodule: Locate = %+v, want {Repo:r_ddc6e2b2 Cwd:.}", got)
	}
}

func TestLobeSenseLocateThroughALink(t *testing.T) {
	c := t.TempDir()
	for _, d := range []string{
		filepath.Join(c, "app", ".git"),
		filepath.Join(c, "app", "src"),
	} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, c).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v: %s", link, c, err, out)
		}
	} else if err := os.Symlink(c, link); err != nil {
		t.Fatal(err)
	}
	want := store.RepoKey(c, filepath.Join(c, "app"))

	// A symbolic link is resolved, so one side through the link and the other the real way still
	// meet (this is the macOS temp folder case, /var/folders to /private/var/folders). A Windows
	// junction is not resolved by Go's EvalSymlinks, and so not by the resolver either: a client
	// reached through a junction is a different folder for the whole program, consistently, which
	// the third case below checks.
	if runtime.GOOS != "windows" {
		// The client folder spelled the real way, the working folder through the link.
		if got := Locate(c, filepath.Join(link, "app", "src")); got != (Place{Repo: want, Cwd: "src"}) {
			t.Errorf("link cwd: Locate = %+v, want {Repo:%s Cwd:src}", got, want)
		}
		// The reverse: the client folder through the link, the working folder the real way.
		if got := Locate(link, filepath.Join(c, "app", "src")); got != (Place{Repo: want, Cwd: "src"}) {
			t.Errorf("link client: Locate = %+v, want {Repo:%s Cwd:src}", got, want)
		}
	}
	// Both through the link, at the repository root.
	if got := Locate(link, filepath.Join(link, "app")); got != (Place{Repo: want, Cwd: "."}) {
		t.Errorf("link client and cwd: Locate = %+v, want {Repo:%s Cwd:.}", got, want)
	}

	if runtime.GOOS == "windows" {
		short := shortName(t, c)
		if short == "" || strings.EqualFold(filepath.Clean(short), filepath.Clean(c)) {
			t.Logf("short names are off on this volume: %q for %q; skipping the short-name check", short, c)
		} else if got := Locate(c, filepath.Join(short, "app")); got != (Place{Repo: want, Cwd: "."}) {
			t.Errorf("short-name cwd: Locate = %+v, want {Repo:%s Cwd:.}", got, want)
		}
	}
}

// shortName is the 8.3 short name of the Windows folder dir, or "" when cmd gives none.
func shortName(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "for %I in ("+dir+") do @echo %~sI").Output()
	if err != nil {
		t.Fatalf("short name of %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestLobeSenseCallText(t *testing.T) {
	bash := json.RawMessage(`{"command":"npm test","description":"run tests"}`)
	if got := CallText("Bash", bash); got != "npm test" {
		t.Errorf("Bash = %q, want npm test", got)
	}
	asString, err := json.Marshal(string(bash))
	if err != nil {
		t.Fatal(err)
	}
	if got := CallText("Bash", asString); got != "npm test" {
		t.Errorf("Bash as a JSON string = %q, want npm test", got)
	}

	edit := json.RawMessage(`{"file_path":"a.go","old_string":"x"}`)
	if got := CallText("Edit", edit); got != `{"file_path":"a.go","old_string":"x"}` {
		t.Errorf("Edit = %q", got)
	}

	flag := json.RawMessage(`{"command":"deploy --token s3cr3tvalue99 now"}`)
	if got := CallText("Bash", flag); strings.Contains(got, "s3cr3tvalue99") {
		t.Errorf("token flag leaked: %q", got)
	}

	long, err := json.Marshal(map[string]string{"command": strings.Repeat("x", 6000)})
	if err != nil {
		t.Fatal(err)
	}
	if got := CallText("Bash", long); len([]rune(got)) > 4000 {
		t.Errorf("long command is %d runes, want at most 4000", len([]rune(got)))
	}
}

func TestLobeSenseOutcome(t *testing.T) {
	s := setup(t)
	call := firstCall()
	ev := FromClaudeFailure(call, firstPlace(), fixedNow())

	out, err := Record(context.Background(), s, ev, call)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if out.State != StateFull {
		t.Errorf("State = %q, want %q", out.State, StateFull)
	}
	if out.ID != ev.ID || out.Kind != ev.Kind {
		t.Errorf("ID/Kind = %q/%q, want %q/%q", out.ID, out.Kind, ev.ID, ev.Kind)
	}
	if out.CallID != "toolu_01" || out.Call != "npm run buld" {
		t.Errorf("CallID/Call = %q/%q", out.CallID, out.Call)
	}

	data, err := os.ReadFile(filepath.Join(s.Dir(), "events", "events-20261109.jsonl"))
	if err != nil {
		t.Fatalf("read day file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &line); err != nil {
		t.Fatalf("line does not parse: %v", err)
	}
	if line["id"] != out.ID {
		t.Errorf("line id = %v, want %q", line["id"], out.ID)
	}
	// The fingerprint of a failure is the one fingerprint.Of gives for its tool and stored error, in the
	// outcome and in the line alike.
	wantFP, _ := fingerprint.Of(ev.Tool, ev.Error.String())
	if out.FP != wantFP || line["fp"] != wantFP {
		t.Errorf("FP: outcome %q, line %v, want %q", out.FP, line["fp"], wantFP)
	}

	// A tool that is not a shell tool: its outcome carries the call text CallText gives it, and its tool name.
	mcpInput := json.RawMessage(`{"n":4411}`)
	mcpCall := Call{Session: "s1", Tool: "mcp__acme__ping", CallID: "toolu_mcp", Input: mcpInput, Error: "widget 4411 is not frobnicated"}
	mcpOut, err := Record(context.Background(), s, FromClaudeFailure(mcpCall, Place{Cwd: "."}, fixedNow()), mcpCall)
	if err != nil {
		t.Fatalf("Record of an MCP failure: %v", err)
	}
	if want := CallText("mcp__acme__ping", mcpInput); mcpOut.Call != want || mcpOut.Tool != "mcp__acme__ping" {
		t.Errorf("MCP outcome Call/Tool = %q/%q, want %q/mcp__acme__ping", mcpOut.Call, mcpOut.Tool, want)
	}
	if want, _ := fingerprint.Of("mcp__acme__ping", "widget 4411 is not frobnicated"); mcpOut.FP != want {
		t.Errorf("MCP outcome FP = %q, want %q", mcpOut.FP, want)
	}

	// A fresh store whose events path is a regular file cannot take the line.
	fresh := openClient(t, "beta")
	if err := os.WriteFile(filepath.Join(fresh.Dir(), "events"), []byte("not a folder\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = Record(context.Background(), fresh, ev, call)
	if out.State != StateDropped {
		t.Errorf("State = %q, want %q", out.State, StateDropped)
	}
	if out.Kind != eventlog.KindFail {
		t.Errorf("Kind = %q, want fail", out.Kind)
	}
	if out.FP != wantFP {
		t.Errorf("dropped FP = %q, want %q", out.FP, wantFP)
	}
	if err == nil {
		t.Error("Record returned no error for a failed write")
	}
}

// TestLobeSenseFingerprintInLine: one failure per tool class (shell, edit, read, web, mcp:<server>, other)
// writes a line whose fp is the one fingerprint.Of gives for that line's own tool and error. A shell failure
// whose error was only "Exit code 3" has exit 3, no error and the fp of "<empty>". An ok line has no fp.
func TestLobeSenseFingerprintInLine(t *testing.T) {
	s := setup(t)
	now := fixedNow()
	calls := []Call{
		{Session: "s1", Tool: "Bash", CallID: "c-shell", Input: json.RawMessage(`{"command":"npm run buld"}`), Error: "Exit code 1\nnpm error Missing script: buld"},
		{Session: "s1", Tool: "Edit", CallID: "c-edit", Input: json.RawMessage(`{"file_path":"a.txt"}`), Error: "String to replace not found in file"},
		{Session: "s1", Tool: "Read", CallID: "c-read", Input: json.RawMessage(`{"file_path":"missing.txt"}`), Error: "File does not exist: /work/missing.txt"},
		{Session: "s1", Tool: "WebFetch", CallID: "c-web", Input: json.RawMessage(`{"url":"https://example.test/x"}`), Error: "HTTP 404 Not Found"},
		{Session: "s1", Tool: "mcp__acme__ping", CallID: "c-mcp", Input: json.RawMessage(`{"n":4411}`), Error: "widget 4411 is not frobnicated"},
		{Session: "s1", Tool: "TodoWrite", CallID: "c-other", Input: json.RawMessage(`{}`), Error: "Unknown todo state: 7"},
		{Session: "s1", Tool: "Bash", CallID: "c-exit3", Input: json.RawMessage(`{"command":"tool --check"}`), Error: "Exit code 3"},
	}
	for _, c := range calls {
		if _, err := Record(context.Background(), s, FromClaudeFailure(c, Place{Cwd: "."}, now), c); err != nil {
			t.Fatalf("Record %s: %v", c.CallID, err)
		}
	}
	ok := Call{Session: "s1", Tool: "Bash", CallID: "c-grep", Input: json.RawMessage(`{"command":"grep -n zzz notes.txt"}`), Error: "Exit code 1"}
	if _, err := Record(context.Background(), s, FromClaudeFailure(ok, Place{Cwd: "."}, now), ok); err != nil {
		t.Fatalf("Record grep: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(s.Dir(), "events", "events-20261109.jsonl"))
	if err != nil {
		t.Fatalf("read day file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != len(calls)+1 {
		t.Fatalf("%d lines, want %d", len(lines), len(calls)+1)
	}
	parsed := make([]map[string]any, len(lines))
	for i, raw := range lines {
		if err := json.Unmarshal([]byte(raw), &parsed[i]); err != nil {
			t.Fatalf("line %d does not parse: %v", i+1, err)
		}
	}

	for i, c := range calls {
		line := parsed[i]
		if line["kind"] != "fail" {
			t.Errorf("line %d kind = %v, want fail", i+1, line["kind"])
		}
		tool, _ := line["tool"].(string)
		errText, _ := line["error"].(string)
		want, _ := fingerprint.Of(tool, errText)
		if tool != c.Tool || line["fp"] != want {
			t.Errorf("line %d (%s): tool %q fp %v, want tool %q fp %q", i+1, c.Tool, tool, line["fp"], c.Tool, want)
		}
	}

	// The shell failure with only "Exit code 3": exit 3, no error, the fp of an empty text.
	exitLine := parsed[len(calls)-1]
	wantEmpty, tpl := fingerprint.Of("Bash", "")
	if tpl != "<empty>" {
		t.Errorf("template of an empty text = %q, want <empty>", tpl)
	}
	if exitLine["exit"] != float64(3) {
		t.Errorf("exit = %v, want 3", exitLine["exit"])
	}
	if _, has := exitLine["error"]; has {
		t.Errorf("error = %v, want none", exitLine["error"])
	}
	if exitLine["fp"] != wantEmpty {
		t.Errorf("fp = %v, want %q", exitLine["fp"], wantEmpty)
	}

	// The ok line (grep found nothing) has no fp.
	okLine := parsed[len(parsed)-1]
	if okLine["kind"] != "ok" {
		t.Errorf("last line kind = %v, want ok", okLine["kind"])
	}
	if _, has := okLine["fp"]; has {
		t.Errorf("ok line has fp %v", okLine["fp"])
	}
}

func TestLobeSenseStartsNoProcess(t *testing.T) {
	s := setup(t)
	t.Setenv("PATH", "")
	call := firstCall()
	dir := t.TempDir()
	if got := Locate(dir, dir); got.Cwd != "." {
		t.Errorf("Locate with empty PATH = %+v", got)
	}
	out, err := Record(context.Background(), s, FromClaudeFailure(call, firstPlace(), fixedNow()), call)
	if err != nil || out.State != StateFull {
		t.Fatalf("Record with empty PATH: state %q, err %v", out.State, err)
	}

	for _, f := range nonTestFiles(t) {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if imp.Path.Value == `"os/exec"` {
				t.Errorf("%s imports os/exec", f)
			}
		}
	}
}

func TestLobeSenseNoInit(t *testing.T) {
	for _, f := range nonTestFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "func init(") {
			t.Errorf("%s declares an init function", f)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, v := range vs.Values {
					ast.Inspect(v, func(n ast.Node) bool {
						if _, isCall := n.(*ast.CallExpr); isCall {
							t.Errorf("%s: package-level var %s has a call in its initializer", f, vs.Names[0].Name)
						}
						return true
					})
				}
			}
		}
	}
}

func TestLobeSenseEveryToolEvent(t *testing.T) {
	now := fixedNow()

	edit := FromClaudeFailure(Call{
		Session: "s", Tool: "Edit", CallID: "e1",
		Input: json.RawMessage(`{"file_path":"a.txt"}`),
		Error: "Exit code 1\nString to replace not found",
	}, Place{Cwd: "."}, now)
	if edit.Exit != nil {
		t.Errorf("Edit Exit = %d, want nil: the exit rule is for shell tools only", *edit.Exit)
	}
	if got := edit.Error.String(); got != "Exit code 1\nString to replace not found" {
		t.Errorf("Edit Error = %q, want the whole error", got)
	}

	mcp := FromClaudeFailure(Call{
		Session: "s", Tool: "mcp__acme__ping", CallID: "m1",
		Input: json.RawMessage(`{"password":"hunter2","note":"hi"}`),
		Error: "widget is not frobnicated",
	}, Place{Cwd: "."}, now)
	if got := mcp.Input.String(); got != `{"note":"hi","password":"[redacted:key]"}` {
		t.Errorf("MCP Input = %q", got)
	}

	unnamed := FromClaudeFailure(Call{Session: "s", CallID: "u1", Input: json.RawMessage(`{}`), Error: "x"}, Place{Cwd: "."}, now)
	if unnamed.Tool != "unknown" {
		t.Errorf("empty tool name: Tool = %q, want unknown", unnamed.Tool)
	}

	const secret = "Zx81kQpL0029aaBB"
	web := Call{
		Session: "s", Tool: "WebFetch", CallID: "w1",
		Input: json.RawMessage(`{"url":"https://example.test/x?api_key=` + secret + `","prompt":"read"}`),
		Error: "fetch failed",
	}
	e := FromClaudeFailure(web, Place{Cwd: "."}, now)
	if strings.Contains(e.Input.String(), secret) {
		t.Errorf("WebFetch Input %q holds the secret", e.Input.String())
	}
	if strings.Contains(CallText(web.Tool, web.Input), secret) {
		t.Errorf("WebFetch CallText holds the secret")
	}
}

func TestLobeSenseCallTextOrder(t *testing.T) {
	a := CallText("mcp__acme__ping", json.RawMessage(`{"b":1,"a":2}`))
	b := CallText("mcp__acme__ping", json.RawMessage(`{"a":2,"b":1}`))
	if a != b {
		t.Errorf("key order changes the call text: %q and %q", a, b)
	}

	asString := json.RawMessage(`"{\"file_path\":\"a.go\",\"old_string\":\"x\"}"`)
	want := CallText("Edit", json.RawMessage(`{"file_path":"a.go","old_string":"x"}`))
	if got := CallText("Edit", asString); got != want {
		t.Errorf("Edit as a JSON string = %q, want %q", got, want)
	}
}

func TestLobeSenseSkip(t *testing.T) {
	bash := func(command, errText string) Call {
		return Call{Session: "s", Tool: "Bash", CallID: "b1",
			Input: json.RawMessage(`{"command":` + strconv.Quote(command) + `}`), Error: errText}
	}
	cases := []struct {
		name   string
		call   Call
		reason string
		skip   bool
	}{
		{"interrupted", Call{Tool: "Bash", IsInterrupt: true, Error: "Exit code 1\nfail"}, "interrupt", true},
		{"interrupted with nothing else", Call{IsInterrupt: true}, "interrupt", true},
		{"refused", bash("npm test", "The user doesn't want to proceed with this tool use."), "refused", true},
		{"refused with leading spaces", bash("npm test", "  \n The user doesn't want to proceed with this tool use."), "refused", true},
		{"own tentaqles", bash("tentaqles lobe graph find x", "Exit code 1\nfail"), "own", true},
		{"own tq", bash("tq lobe status", "Exit code 1\nfail"), "own", true},
		{"own after cd", bash("cd app && tentaqles.exe lobe graph map", "Exit code 1\nfail"), "own", true},
		{"own MCP tool", Call{Tool: "mcp__graph__lobe_find", Input: json.RawMessage(`{"q":"zzz"}`), Error: "nothing"}, "own", true},
		{"not own: tentaqles doctor", bash("tentaqles doctor", "Exit code 1\nfail"), "", false},
		{"not own: npm and tentaqles", bash("npm test && tentaqles lobe status", "Exit code 1\nfail"), "", false},
		{"not own: echo", bash("echo tentaqles lobe", "Exit code 1\nfail"), "", false},
		{"not own: MCP acme", Call{Tool: "mcp__acme__ping", Input: json.RawMessage(`{"n":1}`), Error: "nothing"}, "", false},
		{"not own: MCP lobe server", Call{Tool: "mcp__lobe__ping", Input: json.RawMessage(`{"n":1}`), Error: "nothing"}, "", false},
		{"not own: Edit", Call{Tool: "Edit", Input: json.RawMessage(`{"file_path":"a.txt"}`), Error: "String to replace not found"}, "", false},
		{"not own: empty command", bash("", "Exit code 1\nfail"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, skip := Skip(c.call)
			if reason != c.reason || skip != c.skip {
				t.Errorf("Skip = (%q, %v), want (%q, %v)", reason, skip, c.reason, c.skip)
			}
		})
	}
}

func TestLobeSenseGrepNoMatch(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		command  string
		errText  string
		kind     string
		errWant  string
		exitWant *int
	}{
		{"grep exit 1", "Bash", "grep -n zzz notes.txt", "Exit code 1", "ok", "", intp(1)},
		{"grep empty error", "Bash", "grep -n zzz notes.txt", "", "ok", "", nil},
		{"grep exit 2 keeps the message", "Bash", "grep -n zzz notes.txt", "Exit code 2\ngrep: notes.txt: No such file or directory", "fail", "grep: notes.txt: No such file or directory", intp(2)},
		{"rg", "Bash", "rg zzz", "Exit code 1", "ok", "", intp(1)},
		{"git grep", "Bash", "git grep -n x", "Exit code 1", "ok", "", intp(1)},
		{"grep after cd", "Bash", "cd app && grep x f", "Exit code 1", "ok", "", intp(1)},
		{"grep first of a pipe", "Bash", "grep x f | wc -l", "Exit code 1", "ok", "", intp(1)},
		{"grep with a redirect", "Bash", "grep x f 2>/dev/null", "Exit code 1", "ok", "", intp(1)},
		{"ls is not a grep", "Bash", "ls zzz", "Exit code 1", "fail", "", intp(1)},
		{"Edit is not a shell tool", "Edit", `{"file_path":"a.txt"}`, "Exit code 1", "fail", "Exit code 1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := json.RawMessage(`{"command":` + strconv.Quote(c.command) + `}`)
			if c.tool == "Edit" {
				input = json.RawMessage(c.command)
			}
			e := FromClaudeFailure(Call{Session: "s", Tool: c.tool, CallID: "g1", Input: input, Error: c.errText}, Place{Cwd: "."}, fixedNow())
			if e.Kind != c.kind {
				t.Errorf("Kind = %q, want %q", e.Kind, c.kind)
			}
			if got := e.Error.String(); got != c.errWant {
				t.Errorf("Error = %q, want %q", got, c.errWant)
			}
			switch {
			case (e.Exit == nil) != (c.exitWant == nil):
				t.Errorf("Exit = %v, want %v", e.Exit, c.exitWant)
			case e.Exit != nil && *e.Exit != *c.exitWant:
				t.Errorf("Exit = %d, want %d", *e.Exit, *c.exitWant)
			}
		})
	}
}

func TestLobeSenseOwnShellCommands(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{"tentaqles lobe graph find x", true},
		{"tq lobe status", true},
		{"cd app && tentaqles.exe lobe graph map", true},
		{"TQ_NO_BANNER=1 tq lobe status", true},
		{"/usr/local/bin/tq lobe status", true},
		{`C:\tools\tentaqles.exe lobe status`, true},
		{"tentaqles doctor", false},
		{"npm test && tentaqles lobe status", false},
		{"echo tentaqles lobe", false},
		{"tq", false},
		{"tq lobe status && rm -rf x", false},
		{"cd app", false},
		{"", false},
		{"tq lobe status 2>&1", true},
		{"cd app && tq lobe status 2>&1", true},
		{"tentaqles lobe graph find x > out.txt 2>/dev/null", true},
		{"tq lobe status &> all.log", true},
		{"tq lobe status 2>&1 | tee log", false},
		{"echo 1 2>&1", false},
	}
	for _, c := range cases {
		if got := ownShellCommand(c.command); got != c.want {
			t.Errorf("ownShellCommand(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

// nonTestFiles lists the non-test .go files of this package.
func nonTestFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		t.Fatal("no non-test files found")
	}
	return files
}
