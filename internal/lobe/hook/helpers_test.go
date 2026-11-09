package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// fixture is the test home: a temp folder, TQ_HOME inside it, and the clients folder that holds acme
// and globex.
type fixture struct {
	tmp     string
	tqHome  string
	clients string
}

// newHome builds the two test clients by hand and points the environment at them. Every test that runs
// a hook calls it, so no test touches the developer's ~/.tentaqles.
func newHome(t *testing.T) fixture {
	t.Helper()
	tmp := t.TempDir()
	f := fixture{
		tmp:     tmp,
		tqHome:  filepath.Join(tmp, "home", ".tentaqles"),
		clients: filepath.Join(tmp, "clients"),
	}
	t.Setenv("TQ_HOME", f.tqHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	for _, v := range []string{"TQ_LOBE_NO_HOOKS", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "TQ_WS", "TQ_WS_ROOT"} {
		t.Setenv(v, "")
	}
	for _, name := range []string{"acme", "globex"} {
		mustWrite(t, filepath.Join(f.identity(name), "manifest.yaml"),
			"schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: dev@"+name+".test }\nidentities: { claude: {} }\n")
		mustMkdir(t, filepath.Join(f.identity(name), "claude"))
		mustWrite(t, filepath.Join(f.client(name), ".tentaqles.yaml"), "schema: tentaqles-marker-v1\nclient: "+name+"\n")
		mustMkdir(t, filepath.Join(f.app(name), ".git"))
	}
	mustWrite(t, filepath.Join(f.tqHome, "config.yaml"), fmt.Sprintf(
		"bases:\n  - %s\nclients:\n  acme: %s\n  globex: %s\n",
		filepath.ToSlash(f.clients), filepath.ToSlash(f.client("acme")), filepath.ToSlash(f.client("globex"))))
	return f
}

// identity is the identity folder of a client, $TQ_HOME/identities/<name>.
func (f fixture) identity(name string) string { return filepath.Join(f.tqHome, "identities", name) }

// client is the client folder of a client under the clients folder.
func (f fixture) client(name string) string { return filepath.Join(f.clients, name) }

// app is the app folder of a client, a git repository inside the client folder.
func (f fixture) app(name string) string { return filepath.Join(f.clients, name, "app") }

// setLogin makes the agent's login the client's own login folder.
func setLogin(t *testing.T, f fixture, client string) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir(client, "claude"))
}

// failureJSON is a valid failed Bash call in acme's app folder.
func failureJSON(f fixture) string {
	return payloadJSON("s1", f.app("acme"), "Bash", "toolu_01", "npm run buld", "Exit code 1\nnpm error Missing script: buld")
}

// claudePayload is the JSON Claude Code sends for a failed tool call, in its key order.
type claudePayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
	ToolName       string `json:"tool_name"`
	ToolInput      struct {
		Command string `json:"command"`
	} `json:"tool_input"`
	ToolUseID   string `json:"tool_use_id"`
	Error       string `json:"error"`
	IsInterrupt bool   `json:"is_interrupt"`
	DurationMS  int    `json:"duration_ms"`
}

// payloadJSON builds a PostToolUseFailure payload as Claude Code sends it.
func payloadJSON(session, cwd, tool, callID, command, errText string) string {
	p := claudePayload{
		SessionID:      session,
		TranscriptPath: "/x/t.jsonl",
		Cwd:            cwd,
		PermissionMode: "default",
		HookEventName:  "PostToolUseFailure",
		ToolName:       tool,
		ToolUseID:      callID,
		Error:          errText,
		DurationMS:     12,
	}
	p.ToolInput.Command = command
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// runHook runs Main as a claude lobe-hook of event with stdin, fails the test unless it is handled with
// code 0, and returns what it wrote to stdout.
func runHook(t *testing.T, event, stdin string) string {
	t.Helper()
	var buf bytes.Buffer
	handled, code := Main([]string{"lobe-hook", "claude", event}, strings.NewReader(stdin), &buf)
	if !handled || code != 0 {
		t.Fatalf("Main(%s) = handled %v, code %d; want true, 0", event, handled, code)
	}
	return buf.String()
}

// tree lists every folder and file under dir, sorted, with a hash of each file's bytes. A missing
// folder lists as nothing.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var rows []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			rows = append(rows, "dir "+filepath.ToSlash(rel))
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			rows = append(rows, "unreadable "+filepath.ToSlash(rel))
			return nil
		}
		rows = append(rows, fmt.Sprintf("file %s %x", filepath.ToSlash(rel), sha256.Sum256(b)))
		return nil
	})
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

// identityTrees is the tree of both test clients' identity folders, for "nothing changed" checks.
func identityTrees(t *testing.T, f fixture) string {
	t.Helper()
	return tree(t, f.identity("acme")) + "\n--\n" + tree(t, f.identity("globex"))
}

// mustWrite writes text to p, creating the folders above it.
func mustWrite(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mustMkdir creates the folder p and the folders above it.
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}

// exists reports whether p exists, without following a link.
func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// splitLines returns the non-empty lines of s.
func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// lines returns the lines of every events-*.jsonl file of client's store, in file order.
func lines(t *testing.T, client string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(paths.LobeDir(client), "events", "events-*.jsonl"))
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

// statsLines returns the lines of client's error file, or nil when there is none.
func statsLines(t *testing.T, client string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(paths.LobeDir(client), "stats", "hook-errors.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return splitLines(string(b))
}

// logged is the part of an event line the tests read.
type logged struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Host  string `json:"host"`
	Repo  string `json:"repo"`
	Cwd   string `json:"cwd"`
	Exit  *int   `json:"exit"`
	Error string `json:"error"`
	Input string `json:"input"`
}

// decodeLogged decodes event lines.
func decodeLogged(t *testing.T, ls []string) []logged {
	t.Helper()
	var out []logged
	for _, l := range ls {
		var e logged
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("bad event line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

// errRecord is an error line of the error file.
type errRecord struct {
	At    string `json:"at"`
	Host  string `json:"host"`
	Event string `json:"event"`
	Step  string `json:"step"`
	Class string `json:"class"`
}

// allEvents are the seven events of the hook contract.
func allEvents() []string {
	return []string{
		"session-start", "user-prompt-submit", "pre-tool-use", "post-tool-use",
		"post-tool-use-failure", "stop", "session-end",
	}
}
