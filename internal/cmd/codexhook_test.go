package cmd

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// codexPayload is a Codex PreToolUse payload for tool in cwd: a Bash command (a plain string) or
// any other tool, whose input the check ignores.
func codexPayload(t *testing.T, cwd, tool, command string) string {
	t.Helper()
	if tool == "Bash" {
		return hookPayloadFor(t, cwd, command)
	}
	return `{"session_id":"s","hook_event_name":"PreToolUse","tool_name":` + jsonPath(t, tool) + `,"cwd":` + jsonPath(t, cwd) + `,"tool_input":{"input":"*** Begin Patch"}}`
}

// parseCodexDeny checks a Codex answer: exactly one JSON object with the PreToolUse deny shape and
// a reason. It returns the reason.
func parseCodexDeny(t *testing.T, out string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var got map[string]map[string]any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("not a JSON object: %v (out=%q)", err, out)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("more than one JSON value on stdout: %q", out)
	}
	hso, ok := got["hookSpecificOutput"]
	if len(got) != 1 || !ok || len(hso) != 3 {
		t.Fatalf("shape: %q", out)
	}
	if hso["hookEventName"] != "PreToolUse" || hso["permissionDecision"] != "deny" {
		t.Fatalf("not a PreToolUse deny: %q", out)
	}
	reason, _ := hso["permissionDecisionReason"].(string)
	if reason == "" {
		t.Fatalf("a deny without a reason lets the call run: %q", out)
	}
	return reason
}

// requireGit skips a test that needs the git binary.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// gitInitWithEmail makes dir a git repository whose local user.email is email.
func gitInitWithEmail(t *testing.T, dir, email string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", email}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

// A commit under another git email is a deny: Codex gets the deny object on stdout with exit 0, and
// the same reason on stderr.
func TestCodexPreToolUse_WrongGitEmailDenies(t *testing.T) {
	requireGit(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  email: dev@acme.com\n")
	t.Setenv("TQ_WS", "acme") // the terminal of the acme folder: no env-drift finding (after isolateHome)
	gitInitWithEmail(t, ws, "other@example.com")
	code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, ws, "Bash", "git commit -m x"))
	if code != 0 {
		t.Fatalf("code=%d want 0 (err=%q)", code, errOut)
	}
	reason := parseCodexDeny(t, out)
	if !strings.HasPrefix(reason, "BLOCKED [identity/git-email-drift]: ") {
		t.Fatalf("reason=%q", reason)
	}
	if errOut != reason+"\n" {
		t.Fatalf("stderr=%q want the reason", errOut)
	}
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":` + jsonPath(t, reason) + `}}` + "\n"
	if out != want {
		t.Fatalf("stdout=%q\nwant   %q", out, want)
	}
}

// The same commit under the client's own email is an allow: nothing on stdout or stderr, exit 0.
func TestCodexPreToolUse_MatchingIdentityAllows(t *testing.T) {
	requireGit(t)
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  email: dev@acme.com\n")
	t.Setenv("TQ_WS", "acme")
	gitInitWithEmail(t, ws, "dev@acme.com")
	code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, ws, "Bash", "git commit -m x"))
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// A tool that is not a shell command is allowed, as the Claude Code hook allows it.
func TestCodexPreToolUse_NonShellToolAllowed(t *testing.T) {
	isolateHome(t)
	for _, tool := range []string{"apply_patch", "spawn_agent", "mcp__github__create_issue"} {
		code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, testutil.TempDir(t), tool, ""))
		if code != 0 || out != "" || errOut != "" {
			t.Fatalf("%s: code=%d out=%q err=%q", tool, code, out, errOut)
		}
	}
}

// A shell command in a folder with no client, and a remote git push there, is denied as an
// identity block, in the Codex form.
func TestCodexPreToolUse_NeutralFolderBlocksPush(t *testing.T) {
	isolateHome(t)
	code, out, _ := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, testutil.TempDir(t), "Bash", "git push"))
	if code != 0 || !strings.HasPrefix(parseCodexDeny(t, out), "BLOCKED [identity/") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// A payload tq cannot read is a deny the Codex way, not a crash and not an allow.
func TestCodexPreToolUse_UnreadablePayloadDenies(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, "not json")
	if code != 0 || !strings.Contains(parseCodexDeny(t, out), "BLOCKED [tq/guard-error]: the hook payload cannot be read") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// A panic in the check is a deny with exit 0 and no Go trace on stdout.
func TestCodexPreToolUse_PanicDenies(t *testing.T) {
	isolateHome(t)
	prev := hookGuardDecide
	hookGuardDecide = func(guard.Input) guard.Decision {
		var s []int
		_ = s[3] // index out of range
		return guard.Decision{}
	}
	t.Cleanup(func() { hookGuardDecide = prev })

	code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, testutil.TempDir(t), "Bash", "ls"))
	if code != 0 || !strings.Contains(parseCodexDeny(t, out), "BLOCKED [tq/guard-error]: tq guard error") || strings.Contains(errOut, "goroutine") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// A check that does not finish within the deadline is a deny with exit 0.
func TestCodexPreToolUse_DeadlineDenies(t *testing.T) {
	isolateHome(t)
	prevD, prevDecide := hookDeadline, hookGuardDecide
	release := make(chan struct{})
	hookDeadline = 100 * time.Millisecond
	hookGuardDecide = func(guard.Input) guard.Decision {
		<-release
		return guard.Decision{}
	}
	t.Cleanup(func() {
		close(release)
		hookDeadline, hookGuardDecide = prevD, prevDecide
	})

	code, out, errOut := runHook(t, []string{"codex-hook", "pre-tool-use"}, codexPayload(t, testutil.TempDir(t), "Bash", "ls"))
	if code != 0 || !strings.Contains(parseCodexDeny(t, out), "did not finish within") || !strings.Contains(errOut, "did not finish within") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// Flags tq does not know, and the Claude-only --json, fail the parse: RunMain answers the deny
// object with exit 0 (the command itself never runs).
func TestCodexPreToolUse_BadFlagDenies(t *testing.T) {
	isolateHome(t)
	for _, args := range [][]string{
		{"codex-hook", "pre-tool-use", "--no-such-flag"},
		{"codex-hook", "pre-tool-use", "--json"},
		{"codex-hook", "pre-tool-use", "--agent", "codex"},
	} {
		var out, errOut syncBuffer
		code := RunMain(args, &out, &errOut)
		if code != 0 || !strings.Contains(parseCodexDeny(t, out.String()), "BLOCKED [tq/guard-error]: tq could not run the hook") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out.String(), errOut.String())
		}
	}
}

// The process watchdog answers the Codex deny object, with exit 0, when the run is still going
// after hookWatchdog. It answers once: the decision that never returns cannot add a second object.
func TestCodexPreToolUse_WatchdogDenies(t *testing.T) {
	isolateHome(t)
	prevW, prevD, prevE, prevP := hookWatchdog, hookDeadline, hookExit, hookGuardDecide
	release := make(chan struct{})
	hookWatchdog, hookDeadline = 150*time.Millisecond, time.Hour
	hookGuardDecide = func(guard.Input) guard.Decision {
		<-release
		return guard.Decision{}
	}
	codes := make(chan int, 1)
	hookExit = func(c int) { codes <- c }
	t.Cleanup(func() {
		close(release)
		hookWatchdog, hookDeadline, hookExit, hookGuardDecide = prevW, prevD, prevE, prevP
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(codexPayload(t, testutil.TempDir(t), "Bash", "ls"))
	_ = w.Close()
	prevIn := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prevIn })

	var out, errOut syncBuffer
	go RunMain([]string{"codex-hook", "pre-tool-use"}, &out, &errOut)
	select {
	case code := <-codes:
		if code != 0 {
			t.Fatalf("exit %d, want 0", code)
		}
		if reason := parseCodexDeny(t, out.String()); !strings.Contains(reason, "tq did not answer within") {
			t.Fatalf("reason=%q", reason)
		}
		if !strings.Contains(errOut.String(), "tq did not answer within") {
			t.Fatalf("stderr=%q", errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watchdog did not fire")
	}
}

// The first answer on a Codex run's stdout is the only one: a second write is dropped.
func TestAnswerOnceKeepsTheFirstAnswer(t *testing.T) {
	var buf strings.Builder
	a := &answerOnce{w: &buf}
	_, _ = a.Write([]byte("first\n"))
	_, _ = a.Write([]byte("second\n"))
	if buf.String() != "first\n" {
		t.Fatalf("stdout=%q", buf.String())
	}
}

// hookInvocation names the Codex dialect for codex-hook pre-tool-use only.
func TestHookInvocationDialects(t *testing.T) {
	if d, ok := hookInvocation([]string{"codex-hook", "pre-tool-use"}); !ok || !d.codex {
		t.Fatalf("codex pre-tool-use: d=%+v ok=%v", d, ok)
	}
	if d, ok := hookInvocation([]string{"claude-hook", "pre-tool-use"}); !ok || d.codex {
		t.Fatalf("claude pre-tool-use: d=%+v ok=%v", d, ok)
	}
	for _, args := range [][]string{{"codex-hook", "session-start"}, {"claude-hook", "session-start"}, {"doctor"}} {
		if _, ok := hookInvocation(args); ok {
			t.Fatalf("%v is no pre-tool-use run", args)
		}
	}
}

// session-start prints the same preamble as the Claude Code hook for the same folder.
func TestCodexSessionStart_PrintsTheClientPreamble(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  provider: github\n  user: acme-bot\n  email: dev@acme.com\n")
	payload := `{"session_id":"s","hook_event_name":"SessionStart","source":"startup","cwd":` + jsonPath(t, ws) + `}`
	code, out, errOut := runHook(t, []string{"codex-hook", "session-start"}, payload)
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.HasPrefix(out, "Client: acme\n") || !strings.Contains(out, "Identity: acme") || !strings.Contains(out, "tentaqles doctor:") {
		t.Fatalf("out=%q", out)
	}
	_, claude, _ := runHook(t, []string{"claude-hook", "session-start"}, payload)
	if out != claude {
		t.Fatalf("codex and claude preambles differ:\ncodex  %q\nclaude %q", out, claude)
	}
}

// A neutral folder gets the neutral preamble, not an error.
func TestCodexSessionStart_NeutralFolder(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"codex-hook", "session-start"}, codexPayload(t, testutil.TempDir(t), "Bash", ""))
	if code != 0 || !strings.HasPrefix(out, "Client: none (neutral folder: outside any base)\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// session-start never blocks a session: a registry it cannot read still exits 0 with a note.
func TestCodexSessionStart_NeverFails(t *testing.T) {
	home := isolateHome(t)
	cfgPath := home + "/.tentaqles/config.yaml"
	if err := os.MkdirAll(cfgPath, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runHook(t, []string{"codex-hook", "session-start"}, codexPayload(t, testutil.TempDir(t), "Bash", ""))
	if code != 0 || !strings.HasPrefix(out, "Tentaqles: tq could not") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

// An unreadable payload on session-start still exits 0: the folder is the process cwd.
func TestCodexSessionStart_BadPayloadExitsZero(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"codex-hook", "session-start"}, "not json")
	if code != 0 || !strings.HasPrefix(out, "Client:") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}
