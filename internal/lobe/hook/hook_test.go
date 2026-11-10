package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// evPost is the event this version records.
const evPost = "post-tool-use-failure"

// shortenCutoffs makes the cutoffs and the slack short for one test, restored at its end.
func shortenCutoffs(t *testing.T, inject, capture, slack time.Duration) {
	t.Helper()
	oldInject, oldCapture, oldSlack := injectCutoff, captureCutoff, watchdogSlack
	injectCutoff, captureCutoff, watchdogSlack = inject, capture, slack
	t.Cleanup(func() {
		injectCutoff, captureCutoff, watchdogSlack = oldInject, oldCapture, oldSlack
	})
}

// setSteps installs the test seam's steps for one test, restored at its end.
func setSteps(t *testing.T, fn func(event string) []step) {
	t.Helper()
	old := extraSteps
	extraSteps = fn
	t.Cleanup(func() { extraSteps = old })
}

// TestHelperProcess is not a test: the test binary re-runs itself with LOBE_HELPER set, which makes
// this function do one job for the parent test. Without LOBE_HELPER it returns at once.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("LOBE_HELPER") {
	case "spin":
		extraSteps = func(string) []step {
			return []step{{id: "T-SPIN", run: func(context.Context, *run) error {
				for {
				}
			}}}
		}
		Main([]string{"lobe-hook", "claude", evPost}, os.Stdin, os.Stdout)
	case "idle":
	}
}

func TestLobeFixtureResolves(t *testing.T) {
	f := newHome(t)
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	res := resolve.Resolve(f.app("acme"), cfg)
	if res.Workspace == nil || res.Workspace.Name != "acme" {
		t.Fatalf("the fixture does not resolve to acme: %+v", res)
	}
}

func TestLobeHookNotOurs(t *testing.T) {
	for _, args := range [][]string{{"version"}, nil} {
		var buf bytes.Buffer
		handled, code := Main(args, strings.NewReader(""), &buf)
		if handled || code != 0 || buf.Len() != 0 {
			t.Errorf("Main(%q) = handled %v, code %d, stdout %q; want false, 0, nothing", args, handled, code, buf.String())
		}
	}
}

// rawPayloadJSON builds a PostToolUseFailure payload whose tool_input is the raw JSON object input,
// for a tool whose input is not a command (payloadJSON only builds a command).
func rawPayloadJSON(session, cwd, tool, callID, input, errText string) string {
	p := struct {
		SessionID      string          `json:"session_id"`
		TranscriptPath string          `json:"transcript_path"`
		Cwd            string          `json:"cwd"`
		PermissionMode string          `json:"permission_mode"`
		HookEventName  string          `json:"hook_event_name"`
		ToolName       string          `json:"tool_name"`
		ToolInput      json.RawMessage `json:"tool_input"`
		ToolUseID      string          `json:"tool_use_id"`
		Error          string          `json:"error"`
	}{session, "/x/t.jsonl", cwd, "default", "PostToolUseFailure", tool, json.RawMessage(input), callID, errText}
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestLobeSenseFailureEveryTool(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")

	calls := []struct {
		tool, callID, input, errText string
	}{
		{"Bash", "toolu_01", `{"command":"npm run buld"}`, "Exit code 1\nnpm error Missing script: buld"},
		{"PowerShell", "toolu_02", `{"command":"npm run buld"}`, "Exit code 1\nnpm error Missing script: buld"},
		{"Edit", "toolu_03", `{"file_path":"a.txt","old_string":"a","new_string":"b"}`, "String to replace not found"},
		{"Write", "toolu_04", `{"file_path":"b.txt","content":"hi"}`, "permission denied"},
		{"WebFetch", "toolu_05", `{"url":"https://example.test/x","prompt":"read"}`, "fetch failed: 503"},
		{"mcp__acme__ping", "toolu_06", `{"n":4411}`, "widget 4411 is not frobnicated"},
	}
	for _, c := range calls {
		out := runHook(t, evPost, rawPayloadJSON("s1", app, c.tool, c.callID, c.input, c.errText))
		if out != "" {
			t.Errorf("%s: stdout %q, want nothing", c.tool, out)
		}
	}

	ls := lines(t, "acme")
	if len(ls) != len(calls) {
		t.Fatalf("after %d failures of every tool: %d lines, want %d", len(calls), len(ls), len(calls))
	}
	got := decodeLogged(t, ls)
	for i, e := range got {
		if e.Kind != "fail" || e.Host != "claude" || e.Repo != "r_a172cedc" || e.Cwd != "." {
			t.Errorf("event %d %+v: want kind fail, host claude, repo r_a172cedc, cwd .", i, e)
		}
	}

	first := got[0]
	if first.ID != "evt_9bde2e09" || first.Exit == nil || *first.Exit != 1 ||
		first.Error != "npm error Missing script: buld" || first.Input != `{"command":"npm run buld"}` {
		t.Errorf("first event = %+v", first)
	}

	// The lines are decoded again as maps, for the tool name and for the keys a line has or lacks.
	maps := make([]map[string]any, len(ls))
	for i, l := range ls {
		if err := json.Unmarshal([]byte(l), &maps[i]); err != nil {
			t.Fatalf("line %d does not parse: %v", i, err)
		}
	}
	if maps[2]["tool"] != "Edit" {
		t.Errorf("line 2 tool = %v, want Edit", maps[2]["tool"])
	}
	if got[2].Error != "String to replace not found" {
		t.Errorf("Edit error = %q, want the whole error", got[2].Error)
	}
	if got[2].Input != `{"file_path":"a.txt","new_string":"b","old_string":"a"}` {
		t.Errorf("Edit input = %q", got[2].Input)
	}
	if _, has := maps[2]["exit"]; has {
		t.Errorf("Edit line has an exit key: %v", maps[2]["exit"])
	}

	if maps[5]["tool"] != "mcp__acme__ping" {
		t.Errorf("line 5 tool = %v, want mcp__acme__ping", maps[5]["tool"])
	}
	if got[5].Input != `{"n":4411}` {
		t.Errorf("MCP input = %q", got[5].Input)
	}
	if got[5].Error != "widget 4411 is not frobnicated" {
		t.Errorf("MCP error = %q", got[5].Error)
	}
	if _, has := maps[5]["exit"]; has {
		t.Errorf("MCP line has an exit key: %v", maps[5]["exit"])
	}
}

func TestLobeSenseFailureSkips(t *testing.T) {
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
			return payloadJSON("s1", f.tmp, "Bash", "toolu_01", "npm test", "Exit code 1\nfail")
		}},
		{"login names another client", func(t *testing.T, f fixture) string {
			t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir("globex", "claude"))
			return failureJSON(f)
		}},
		{"login empty", func(t *testing.T, f fixture) string {
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			return failureJSON(f)
		}},
		{"interrupted", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			return strings.TrimSuffix(rawPayloadJSON("s1", f.app("acme"), "Bash", "toolu_01",
				`{"command":"npm run buld"}`, "Exit code 1\nnpm error Missing script: buld"), "}") + `,"is_interrupt":true}`
		}},
		{"refused", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			return payloadJSON("s1", f.app("acme"), "Bash", "toolu_01", "npm run buld",
				"The user doesn't want to proceed with this tool use.")
		}},
		{"capture off", func(t *testing.T, f fixture) string {
			setLogin(t, f, "acme")
			appendManifestLobe(t, f, "acme", "capture: off")
			return failureJSON(f)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			stdin := c.setup(t, f)
			before := identityTrees(t, f)
			if out := runHook(t, evPost, stdin); out != "" {
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

func TestLobeSenseOwnTools(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")
	const errText = "Exit code 1\nfail"

	own := []string{
		rawPayloadJSON("s1", app, "Bash", "toolu_o1", `{"command":"tentaqles lobe graph find x"}`, errText),
		rawPayloadJSON("s1", app, "Bash", "toolu_o2", `{"command":"tq lobe status"}`, errText),
		rawPayloadJSON("s1", app, "Bash", "toolu_o3", `{"command":"cd app && tentaqles.exe lobe graph map"}`, errText),
		rawPayloadJSON("s1", app, "mcp__graph__lobe_find", "toolu_o4", `{"q":"zzz"}`, "nothing"),
	}
	for _, stdin := range own {
		runHook(t, evPost, stdin)
	}
	if n := len(lines(t, "acme")); n != 0 {
		t.Fatalf("the memory's own tools wrote %d lines, want 0", n)
	}
	if exists(paths.LobeDir("acme")) {
		t.Fatalf("the memory's own tools created a lobe folder for acme")
	}

	notOwn := []string{
		payloadJSON("s1", app, "Bash", "toolu_1", "tentaqles doctor", errText),
		payloadJSON("s1", app, "Bash", "toolu_2", "npm test && tentaqles lobe status", errText),
		payloadJSON("s1", app, "Bash", "toolu_3", "echo tentaqles lobe", errText),
		rawPayloadJSON("s1", app, "mcp__acme__ping", "toolu_4", `{"n":1}`, "nothing"),
	}
	for i, stdin := range notOwn {
		runHook(t, evPost, stdin)
		if n := len(lines(t, "acme")); n != i+1 {
			t.Fatalf("after %d tools that are not own: %d lines, want %d", i+1, n, i+1)
		}
	}
}

func TestLobeHookGrepNoMatch(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	app := f.app("acme")

	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_01", "grep -n zzz notes.txt", "Exit code 1"))
	ls := lines(t, "acme")
	if len(ls) != 1 {
		t.Fatalf("after a grep that found nothing: %d lines, want 1", len(ls))
	}
	var ok map[string]any
	if err := json.Unmarshal([]byte(ls[0]), &ok); err != nil {
		t.Fatalf("line does not parse: %v", err)
	}
	if ok["kind"] != "ok" || ok["exit"] != float64(1) {
		t.Errorf("line = %v, want kind ok and exit 1", ok)
	}
	if _, has := ok["error"]; has {
		t.Errorf("line has an error key: %v", ok["error"])
	}

	runHook(t, evPost, payloadJSON("s1", app, "Bash", "toolu_02", "grep -n zzz notes.txt",
		"Exit code 2\ngrep: notes.txt: No such file or directory"))
	ls = lines(t, "acme")
	if len(ls) != 2 {
		t.Fatalf("after a grep that failed: %d lines, want 2", len(ls))
	}
	var bad map[string]any
	if err := json.Unmarshal([]byte(ls[1]), &bad); err != nil {
		t.Fatalf("line does not parse: %v", err)
	}
	if bad["kind"] != "fail" || bad["exit"] != float64(2) {
		t.Errorf("line = %v, want kind fail and exit 2", bad)
	}
}

func TestLobeHookServesOnlyItsLogin(t *testing.T) {
	f := newHome(t)
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir("globex", "claude"))
	before := identityTrees(t, f)
	served := 0
	for _, ev := range allEvents() {
		if out := runHook(t, ev, failureJSON(f)); out != "" {
			t.Errorf("%s: stdout %q, want nothing", ev, out)
			continue
		}
		served++
	}
	t.Logf("%d of %d events answered without writing", served, len(allEvents()))
	if after := identityTrees(t, f); after != before {
		t.Errorf("an identity folder changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, name := range []string{"acme", "globex"} {
		if exists(paths.LobeDir(name)) {
			t.Errorf("a lobe folder exists for %s", name)
		}
	}
}

func TestLobeHookLoginUnsetDoesNothing(t *testing.T) {
	cases := []struct {
		name  string
		login func(t *testing.T, f fixture) string
	}{
		{"unset", func(t *testing.T, f fixture) string { return "" }},
		{"the home claude folder", func(t *testing.T, f fixture) string {
			p := filepath.Join(f.tmp, "home", ".claude")
			mustMkdir(t, p)
			return p
		}},
		{"a folder that does not exist", func(t *testing.T, f fixture) string {
			return filepath.Join(f.tmp, "nowhere")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newHome(t)
			t.Setenv("CLAUDE_CONFIG_DIR", c.login(t, f))
			before := identityTrees(t, f)
			if out := runHook(t, evPost, failureJSON(f)); out != "" {
				t.Errorf("stdout %q, want nothing", out)
			}
			if after := identityTrees(t, f); after != before {
				t.Errorf("an identity folder changed")
			}
			if exists(paths.LobeDir("acme")) {
				t.Errorf("a store was created")
			}
		})
	}
}

// loginForm is the login path spelled one of the ways the test names.
func loginForm(name, login string) string {
	switch name {
	case "trailing separator":
		return login + string(filepath.Separator)
	case "forward slashes":
		return filepath.ToSlash(login)
	case "mixed separators":
		i := strings.LastIndex(login, string(filepath.Separator))
		return login[:i] + "/" + login[i+1:]
	case "upper case":
		return strings.ToUpper(login)
	}
	return login
}

func TestLobeHookLoginPathForms(t *testing.T) {
	names := []string{"trailing separator", "forward slashes", "mixed separators", "upper case", "plain"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f := newHome(t)
			t.Setenv("CLAUDE_CONFIG_DIR", loginForm(name, filepath.Join(f.identity("acme"), "claude")))
			before := len(lines(t, "acme"))
			runHook(t, evPost, failureJSON(f))
			want := 1
			if name == "upper case" && runtime.GOOS != "windows" {
				want = 0
			}
			if got := len(lines(t, "acme")) - before; got != want {
				t.Errorf("%s wrote %d lines, want %d", name, got, want)
			}
		})
	}
}

func TestLobeNoHooksSwitch(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	t.Setenv("TQ_LOBE_NO_HOOKS", "1")
	for _, ev := range allEvents() {
		if out := runHook(t, ev, failureJSON(f)); out != "" {
			t.Errorf("%s: stdout %q, want nothing", ev, out)
		}
	}
	if exists(paths.LobeDir("acme")) {
		t.Errorf("a lobe folder was written with TQ_LOBE_NO_HOOKS=1")
	}
}

func TestLobeHookAlwaysExitsZero(t *testing.T) {
	// acmeFailure sets acme's login and returns a valid Bash failure as stdin.
	acmeFailure := func(t *testing.T, f fixture) io.Reader {
		setLogin(t, f, "acme")
		return strings.NewReader(failureJSON(f))
	}
	cases := []struct {
		name string
		arm  func(t *testing.T, f fixture) (stdin io.Reader, host, event string)
	}{
		{"empty stdin", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			return strings.NewReader(""), "claude", evPost
		}},
		{"not json", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			return strings.NewReader("not json"), "claude", evPost
		}},
		{"invalid UTF-8", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			return strings.NewReader(string([]byte{0xff, 0xfe, '{', '}'})), "claude", evPost
		}},
		{"object over 1 MiB", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			big := `{"` + strings.Repeat("a", 1100*1024) + `":1}`
			return strings.NewReader(big), "claude", evPost
		}},
		{"unknown host", func(t *testing.T, f fixture) (io.Reader, string, string) {
			return acmeFailure(t, f), "cursor", evPost
		}},
		{"unknown event", func(t *testing.T, f fixture) (io.Reader, string, string) {
			return acmeFailure(t, f), "claude", "bogus"
		}},
		{"neutral folder", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			return strings.NewReader(payloadJSON("s1", f.tmp, "Bash", "toolu_01", "npm test", "Exit code 1\nfail")), "claude", evPost
		}},
		{"unreadable manifest", func(t *testing.T, f fixture) (io.Reader, string, string) {
			mustWrite(t, filepath.Join(f.identity("acme"), "manifest.yaml"), "schema: [")
			return acmeFailure(t, f), "claude", evPost
		}},
		{"panicking step", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setSteps(t, func(string) []step {
				return []step{{id: "T-PANIC", run: func(context.Context, *run) error { panic("boom") }}}
			})
			return acmeFailure(t, f), "claude", evPost
		}},
		{"step that ignores its context", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setSteps(t, func(string) []step {
				return []step{{id: "T-SLEEP", run: func(context.Context, *run) error {
					time.Sleep(10 * time.Second)
					return nil
				}}}
			})
			return acmeFailure(t, f), "claude", evPost
		}},
		{"store lock held", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			st, err := store.Open("acme", store.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			unlock, err := st.Lock(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(unlock)
			return strings.NewReader(failureJSON(f)), "claude", evPost
		}},
		{"stdin that never closes", func(t *testing.T, f fixture) (io.Reader, string, string) {
			setLogin(t, f, "acme")
			pr, pw := io.Pipe()
			t.Cleanup(func() { pw.Close() })
			return pr, "claude", evPost
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shortenCutoffs(t, 300*time.Millisecond, 300*time.Millisecond, 100*time.Millisecond)
			f := newHome(t)
			stdin, host, event := c.arm(t, f)
			var buf bytes.Buffer
			start := time.Now()
			handled, code := Main([]string{"lobe-hook", host, event}, stdin, &buf)
			elapsed := time.Since(start)
			t.Logf("%s: returned after %v", c.name, elapsed)
			if !handled || code != 0 || buf.Len() != 0 {
				t.Fatalf("Main = handled %v, code %d, stdout %q; want true, 0, nothing", handled, code, buf.String())
			}
			if limit := cutoffFor(event) + 250*time.Millisecond; elapsed > limit {
				t.Errorf("returned after %v, limit %v", elapsed, limit)
			}
			if c.name == "store lock held" {
				if n := len(lines(t, "acme")); n != 1 {
					t.Errorf("with the lock held: %d event lines, want 1", n)
				}
			}
		})
	}
}

func TestLobeCaptureSurvivesALateStep(t *testing.T) {
	shortenCutoffs(t, 300*time.Millisecond, 300*time.Millisecond, 100*time.Millisecond)
	f := newHome(t)
	setLogin(t, f, "acme")
	setSteps(t, func(string) []step {
		return []step{{id: "T-LATE", run: func(context.Context, *run) error {
			time.Sleep(time.Second)
			return nil
		}}}
	})
	if out := runHook(t, evPost, failureJSON(f)); out != "" {
		t.Errorf("stdout %q, want nothing", out)
	}
	if n := len(lines(t, "acme")); n != 1 {
		t.Errorf("%d event lines, want 1", n)
	}
	errs := statsLines(t, "acme")
	if len(errs) != 1 {
		t.Fatalf("%d error lines, want 1: %q", len(errs), errs)
	}
	var rec errRecord
	if err := json.Unmarshal([]byte(errs[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Step != "T-LATE" || rec.Class != "deadline" {
		t.Errorf("error line = %+v, want step T-LATE and class deadline", rec)
	}
}

func TestLobeHookErrorsHoldNoContent(t *testing.T) {
	cases := []struct {
		class string
		run   func(ctx context.Context, r *run) error
	}{
		{"deadline", func(context.Context, *run) error { return context.DeadlineExceeded }},
		{"panic", func(context.Context, *run) error { panic("MARKER-PANIC") }},
		{"busy", func(context.Context, *run) error { return store.ErrBusy }},
		{"oversize", func(context.Context, *run) error { return ErrOversize }},
		{"detach", func(context.Context, *run) error { return ErrDetach }},
		{"io", func(context.Context, *run) error {
			return &fs.PathError{Op: "open", Path: "MARKER-PATH/x", Err: fs.ErrPermission}
		}},
		{"error", func(context.Context, *run) error { return errors.New("MARKER-GOERR") }},
	}
	for _, c := range cases {
		t.Run(c.class, func(t *testing.T) {
			f := newHome(t)
			setLogin(t, f, "acme")
			setSteps(t, func(string) []step { return []step{{id: "T-ERR", run: c.run}} })
			stdin := payloadJSON("MARKER-SESSION", f.app("acme"), "Bash", "MARKER-CALL", "MARKER-CMD", "Exit code 1\nMARKER-ERR")
			runHook(t, evPost, stdin)

			errs := statsLines(t, "acme")
			if len(errs) != 1 {
				t.Fatalf("%d error lines, want 1", len(errs))
			}
			line := errs[0]
			var keys map[string]any
			if err := json.Unmarshal([]byte(line), &keys); err != nil {
				t.Fatal(err)
			}
			if len(keys) != 5 {
				t.Errorf("error line has %d keys, want 5: %s", len(keys), line)
			}
			prefix := `{"at":"`
			suffix := `","host":"claude","event":"post-tool-use-failure","step":"T-ERR","class":"` + c.class + `"}`
			if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
				t.Errorf("error line %s, want the five keys in order ending %s", line, suffix)
			}
			stats := filepath.Join(paths.LobeDir("acme"), "stats")
			_ = filepath.WalkDir(stats, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				b, rerr := os.ReadFile(p)
				if rerr == nil && bytes.Contains(b, []byte("MARKER")) {
					t.Errorf("%s holds payload or error text", p)
				}
				return nil
			})
		})
	}
}

func TestLobeLogDiskFull(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	if _, err := store.Open("acme", store.ReadWrite); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(paths.LobeDir("acme"), "events")
	if err := os.RemoveAll(events); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, events, "")
	if out := runHook(t, evPost, failureJSON(f)); out != "" {
		t.Errorf("stdout %q, want nothing", out)
	}
	fi, err := os.Stat(events)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Errorf("events changed: %v, %v", fi, err)
	}
	errs := statsLines(t, "acme")
	if len(errs) != 1 {
		t.Fatalf("%d error lines, want 1", len(errs))
	}
	var rec errRecord
	if err := json.Unmarshal([]byte(errs[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Step != "S-01" || rec.Class != "io" {
		t.Errorf("error line = %+v, want step S-01 and class io", rec)
	}
}

func TestLobeSenseStartsNoProcess(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	t.Setenv("PATH", "")
	if out := runHook(t, evPost, failureJSON(f)); out != "" {
		t.Errorf("stdout %q, want nothing", out)
	}
	if n := len(lines(t, "acme")); n != 1 {
		t.Errorf("%d event lines with an empty PATH, want 1", n)
	}
}

func TestLobeStall_WatchdogEndsAStuckMain(t *testing.T) {
	shortenCutoffs(t, 300*time.Millisecond, 300*time.Millisecond, 100*time.Millisecond)
	f := newHome(t)
	setLogin(t, f, "acme")
	stdin := failureJSON(f)

	codes := make(chan int, 4)
	oldExit, oldArm := exitProcess, afterArm
	exitProcess = func(c int) { codes <- c }
	release := make(chan struct{})
	var once sync.Once
	releaseFn := func() { once.Do(func() { close(release) }) }
	afterArm = func() { <-release }
	t.Cleanup(func() {
		exitProcess, afterArm = oldExit, oldArm
		releaseFn()
	})

	returned := make(chan struct{})
	go func() {
		Main([]string{"lobe-hook", "claude", evPost}, strings.NewReader(stdin), io.Discard)
		close(returned)
	}()
	select {
	case c := <-codes:
		if c != 0 {
			t.Errorf("watchdog exited with %d, want 0", c)
		}
	case <-time.After(injectCutoff + watchdogSlack + 300*time.Millisecond):
		t.Fatal("the watchdog did not end the stuck Main")
	}
	releaseFn()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Main did not return after release")
	}
}

// childRun runs the test binary as TestHelperProcess in mode with stdin, and returns its stdout and
// how long it took to end.
func childRun(t *testing.T, mode, stdin string) (string, time.Duration) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "LOBE_HELPER="+mode)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s child: %v", mode, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("%s child: %v; stderr: %s", mode, err, errOut.String())
		}
		return out.String(), elapsed
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("%s child did not end", mode)
	}
	return "", 0
}

func TestLobeStall_SpinningHandler(t *testing.T) {
	f := newHome(t)
	setLogin(t, f, "acme")
	stdin := failureJSON(f)

	idle, _ := childRun(t, "idle", stdin)
	spun, elapsed := childRun(t, "spin", stdin)
	t.Logf("spinning child ended after %v", elapsed)
	if limit := injectCutoff + watchdogSlack + 500*time.Millisecond; elapsed > limit {
		t.Errorf("spinning child took %v, limit %v", elapsed, limit)
	}
	if spun != idle {
		t.Errorf("spinning child wrote %q, idle child wrote %q", spun, idle)
	}
	if n := len(lines(t, "acme")); n != 1 {
		t.Errorf("%d event lines, want 1", n)
	}
}

func TestLobeHookCutoffsUnderRegisteredTimeout(t *testing.T) {
	for _, ev := range allEvents() {
		total := cutoffFor(ev) + watchdogSlack
		t.Logf("%s: cutoff plus slack %v", ev, total)
		if total >= 5*time.Second {
			t.Errorf("%s: %v, want under 5s", ev, total)
		}
		if ev == "session-end" && total >= 3*time.Second {
			t.Errorf("session-end: %v, want under 3s", total)
		}
	}
}

func TestLobeHookNoInit(t *testing.T) {
	const module = "github.com/tentaqles/tentaqles/"
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources: %v", err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		isTest := strings.HasSuffix(name, "_test.go")
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, "\"")
			if !strings.HasPrefix(p, module) {
				continue
			}
			rest := strings.TrimPrefix(p, module)
			switch rest {
			case "internal/resolve", "internal/paths", "internal/registry", "internal/manifest":
				continue
			}
			if !strings.HasPrefix(rest, "internal/lobe/") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		if !isTest {
			for _, d := range file.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "init" {
					t.Errorf("%s declares func init", name)
				}
			}
		}
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, v := range vs.Values {
					ast.Inspect(v, func(n ast.Node) bool {
						if _, isCall := n.(*ast.CallExpr); isCall {
							t.Errorf("%s: a package-level var is initialized by a call", name)
						}
						return true
					})
				}
			}
		}
	}
}
