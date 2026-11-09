package sense

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/eventlog"
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
	if err == nil {
		t.Error("Record returned no error for a failed write")
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
