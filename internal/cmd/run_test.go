package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// isolateHome points TQ_HOME and the OS home at temp dirs so tests never touch
// the real developer home (in particular ~/.gitconfig-tentaqles).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	// The test binary may itself run inside a tq-managed shell (a Claude Code
	// session, `tq run`), which carries the state of the last switch in
	// __TQ_STATE. `tq env` would then "restore" that shell's variables and
	// emit unsets for a neutral cwd, so start every test from a clean slate.
	unsetenv(t, envplan.StateVar)
	// Nor from an off window or a per-launch mode (spec E §8).
	for _, k := range []string{"TQ_MODE", "TQ_ENABLED", envplan.HeadlessModeVar} {
		unsetenv(t, k)
	}
	return home
}

// unsetenv removes k from the process environment for the duration of the
// test (t.Setenv can only set, and an empty value is still "present" to
// os.LookupEnv).
func unsetenv(t *testing.T, k string) {
	t.Helper()
	v, ok := os.LookupEnv(k)
	if !ok {
		return
	}
	if err := os.Unsetenv(k); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv(k, v) })
}

// mkUntrusted registers a base and writes a workspace manifest by hand, without
// ever calling tq allow.
func mkUntrusted(t *testing.T, name string) string {
	t.Helper()
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.FileName),
		[]byte("schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\nidentities: { gh: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRun_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"run", "evil", "--", "cmd"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func TestLogin_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "evil", "gh"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func ws(t *testing.T, mode string) *resolve.Workspace {
	t.Helper()
	return &resolve.Workspace{
		Name: "acme",
		Hash: "hash-" + mode,
		Manifest: &manifest.Manifest{
			Schema: "tentaqles-client-v2", Client: "acme",
			Claude: manifest.Claude{PermissionMode: mode},
		},
	}
}

func TestClaudeArgs(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := []string{"-p", "hi"}

	t.Run("empty mode passes args through", func(t *testing.T) {
		var warned []string
		got := claudeArgs(ws(t, ""), base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "-p hi" || len(warned) != 0 {
			t.Fatalf("got %v warned %v", got, warned)
		}
	})

	t.Run("bypass without allow downgrades to acceptEdits and warns", func(t *testing.T) {
		var warned []string
		got := claudeArgs(ws(t, "bypass"), base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "--permission-mode acceptEdits -p hi" {
			t.Fatalf("got %v", got)
		}
		if len(warned) != 1 || !strings.Contains(warned[0], "bypass") {
			t.Fatalf("expected a bypass warning, got %v", warned)
		}
	})

	t.Run("bypass with allow passes the skip flag", func(t *testing.T) {
		w := ws(t, "bypass")
		if err := trust.AllowBypass(w.Hash); err != nil {
			t.Fatal(err)
		}
		var warned []string
		got := claudeArgs(w, base, func(s string) { warned = append(warned, s) })
		if strings.Join(got, " ") != "--dangerously-skip-permissions -p hi" {
			t.Fatalf("got %v", got)
		}
		if len(warned) != 0 {
			t.Fatalf("no warning expected, got %v", warned)
		}
	})

	t.Run("allowlisted mode is forwarded", func(t *testing.T) {
		got := claudeArgs(ws(t, "plan"), base, func(string) {})
		if strings.Join(got, " ") != "--permission-mode plan -p hi" {
			t.Fatalf("got %v", got)
		}
	})
}

// --- spec E §8.1: the launcher -------------------------------------------

// clearAgentMarkers unsets every agent marker (the test binary may run under Claude Code).
func clearAgentMarkers(t *testing.T) {
	t.Helper()
	for _, k := range agentMarkers {
		unsetenv(t, k)
	}
}

// launched is one call of the execCommand seam.
type launched struct {
	name string
	args []string
	env  map[string]string
}

// String names the call without its env: a failure message must never print
// the environment (it carries the developer's tokens).
func (l launched) String() string {
	return l.name + " " + strings.Join(l.args, " ") + " (TQ_MODE=" + l.env["TQ_MODE"] + ")"
}

// fakeExec replaces execCommand for the test and returns the recorded calls.
func fakeExec(t *testing.T) *[]launched {
	t.Helper()
	var calls []launched
	prev := execCommand
	execCommand = func(name string, args []string, env []string) error {
		m := map[string]string{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			m[strings.ToUpper(k)] = v
		}
		calls = append(calls, launched{name, append([]string(nil), args...), m})
		return nil
	}
	t.Cleanup(func() { execCommand = prev })
	return &calls
}

// mkLaunchWS writes and trusts a workspace with claude and codex identities
// and the given claude.permission_mode; it returns the root.
func mkLaunchWS(t *testing.T, name, permMode string) string {
	t.Helper()
	root := mkUntrusted(t, name)
	body := "schema: tentaqles-client-v2\nclient: " + name + "\ngit: { email: tester@example.com }\nidentities: { claude: {}, codex: {} }\n"
	if permMode != "" {
		body += "claude: { permission_mode: " + permMode + " }\n"
	}
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	return root
}

func runTq(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestLaunchClaudeArgs(t *testing.T) {
	cases := []struct {
		in       []string
		agent    bool
		want     string
		wantMode string
	}{
		{[]string{"-p", "hi"}, false, "--dangerously-skip-permissions -p hi", ""},
		{nil, false, "--dangerously-skip-permissions", ""},
		{[]string{"--tq-mode", "strict", "-p", "hi"}, false, "--dangerously-skip-permissions -p hi", "strict"},
		{[]string{"-p", "hi", "--tq-mode=relaxed"}, false, "--dangerously-skip-permissions -p hi", "relaxed"},
		{[]string{"--permission-mode", "plan", "-p", "hi"}, false, "--permission-mode plan -p hi", ""},
		{[]string{"--permission-mode=acceptEdits"}, false, "--permission-mode=acceptEdits", ""},
		{[]string{"--dangerously-skip-permissions", "-p", "x"}, false, "--dangerously-skip-permissions -p x", ""},
		{[]string{"--version"}, false, "--version", ""},
		{[]string{"-h"}, false, "-h", ""},
		{[]string{"mcp", "list"}, false, "mcp list", ""},
		{[]string{"update"}, false, "update", ""},
		// An agent's nested claude never gets bypass from the launcher.
		{[]string{"-p", "hi"}, true, "-p hi", ""},
		// A prompt that happens to say "update" is not a subcommand.
		{[]string{"-p", "update the docs"}, false, "--dangerously-skip-permissions -p update the docs", ""},
	}
	for _, c := range cases {
		got, mode := launchClaudeArgs(c.in, c.agent)
		if strings.Join(got, " ") != c.want || mode != c.wantMode {
			t.Errorf("launchClaudeArgs(%q, agent=%v) = %q, %q; want %q, %q", c.in, c.agent, got, mode, c.want, c.wantMode)
		}
	}
}

// The launcher starts claude in bypass whatever claude.permission_mode says.
func TestRunClaudeAlwaysBypass(t *testing.T) {
	for _, pm := range manifest.PermissionModes {
		t.Run("mode="+pm, func(t *testing.T) {
			mkLaunchWS(t, "acme", pm)
			clearAgentMarkers(t)
			calls := fakeExec(t)
			if out, err := runTq(t, "run", "acme", "--", "claude", "-p", "hi"); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if len(*calls) != 1 || (*calls)[0].name != "claude" {
				t.Fatalf("calls %+v", *calls)
			}
			if got := strings.Join((*calls)[0].args, " "); got != "--dangerously-skip-permissions -p hi" {
				t.Fatalf("args %q", got)
			}
			if (*calls)[0].env["TQ_WS"] != "acme" || (*calls)[0].env["CLAUDE_CONFIG_DIR"] == "" {
				t.Fatalf("identity env missing: TQ_WS=%q", (*calls)[0].env["TQ_WS"])
			}
		})
	}
}

func TestRunTqMode(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	t.Setenv("TQ_MODE", "relaxed") // inherited: a human launch without a mode drops it

	calls := fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "claude", "-p", "x"); err != nil {
		t.Fatal(err)
	}
	if _, set := (*calls)[0].env["TQ_MODE"]; set {
		t.Fatalf("no mode given: TQ_MODE must be unset, got %q", (*calls)[0].env["TQ_MODE"])
	}

	for _, args := range [][]string{
		{"run", "acme", "--tq-mode", "strict", "--", "claude", "-p", "x"},
		{"run", "acme", "--", "claude", "--tq-mode", "strict", "-p", "x"},
	} {
		calls := fakeExec(t)
		if out, err := runTq(t, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		c := (*calls)[0]
		if c.env["TQ_MODE"] != "strict" || strings.Join(c.args, " ") != "--dangerously-skip-permissions -p x" {
			t.Fatalf("%v: env TQ_MODE=%q args %q", args, c.env["TQ_MODE"], c.args)
		}
	}

	// codex: the flag is stripped too
	calls = fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "codex", "--tq-mode=strict", "exec", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.env["TQ_MODE"] != "strict" || slices.Contains(c.args, "--tq-mode=strict") {
		t.Fatalf("codex: %+v", c)
	}

	for _, bad := range []string{"off", "bogus"} {
		fakeExec(t)
		if _, err := runTq(t, "run", "acme", "--tq-mode", bad, "--", "claude"); err == nil {
			t.Errorf("--tq-mode %s must be refused", bad)
		}
	}
}

func TestRunDotResolvesCwd(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	sub := filepath.Join(root, "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	calls := fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if (*calls)[0].env["TQ_WS"] != "acme" {
		t.Fatalf("want acme's identity: TQ_WS=%q", (*calls)[0].env["TQ_WS"])
	}

	// neutral folder: no identity, still the launcher
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "/other/client")
	calls = fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	c := (*calls)[0]
	if _, ok := c.env["TQ_WS"]; ok {
		t.Fatalf("neutral run carries a workspace: TQ_WS=%q", c.env["TQ_WS"])
	}
	if _, ok := c.env["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("another client's config dir leaked")
	}
	if strings.Join(c.args, " ") != "--dangerously-skip-permissions" {
		t.Fatalf("args %q", c.args)
	}
}

func TestRunDotUntrustedRunsNeutralWithWarning(t *testing.T) {
	root := mkUntrusted(t, "evil")
	clearAgentMarkers(t)
	chdir(t, root)
	calls := fakeExec(t)
	out, err := runTq(t, "run", ".", "--", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "untrusted") || len(*calls) != 1 {
		t.Fatalf("want a warning and a neutral launch: %q %+v", out, *calls)
	}
	if _, ok := (*calls)[0].env["TQ_WS"]; ok {
		t.Fatal("an untrusted workspace's identity must not be applied")
	}
}

func TestRunOff(t *testing.T) {
	mkLaunchWS(t, "acme", "acceptEdits")
	clearAgentMarkers(t)
	st := envplan.State{WS: "acme", Prev: map[string]*string{"TQ_WS": nil, "CLAUDE_CONFIG_DIR": nil}}
	t.Setenv(envplan.StateVar, st.Encode())
	t.Setenv("TQ_WS", "acme")
	t.Setenv("CLAUDE_CONFIG_DIR", "/id/acme/claude")
	t.Setenv("CODEX_HOME", "/id/acme/codex")

	calls := fakeExec(t)
	out, err := runTq(t, "run", "--off", "--", "claude", "-p", "x")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.Contains(out, "tq: off") || !strings.Contains(out, "main logins") {
		t.Fatalf("missing the off banner: %q", out)
	}
	c := (*calls)[0]
	want := `--dangerously-skip-permissions --settings {"enabledPlugins":{"tentaqles@tentaqles":false}} -p x`
	if c.name != "claude" || strings.Join(c.args, " ") != want {
		t.Fatalf("claude off args: %q", c.args)
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "TQ_WS", envplan.StateVar} {
		if _, ok := c.env[k]; ok {
			t.Errorf("%s must be removed in off", k)
		}
	}
	if c.env["TQ_MODE"] != "off" {
		t.Fatalf("TQ_MODE = %q", c.env["TQ_MODE"])
	}

	calls = fakeExec(t)
	if _, err := runTq(t, "run", "--off", "--", "codex", "exec", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; strings.Join(c.args, " ") != "--dangerously-bypass-approvals-and-sandbox exec x" || c.env["TQ_MODE"] != "off" {
		t.Fatalf("codex off: %+v", c)
	}

	// --off takes no workspace
	fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--off", "--", "claude"); err == nil {
		t.Fatal("tq run <ws> --off must be refused")
	}

	// --off starts claude or codex only (spec E §8.1); a path or an
	// extension still names them and still gets the off flags.
	for _, name := range []string{"bash", "pwsh", "claudex", "sh"} {
		calls = fakeExec(t)
		if _, err := runTq(t, "run", "--off", "--", name); err == nil || len(*calls) != 0 {
			t.Fatalf("tq run --off -- %s must be refused, got %v %+v", name, err, *calls)
		}
	}
	calls = fakeExec(t)
	if _, err := runTq(t, "run", "--off", "--", "/opt/bin/claude.exe", "-p", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.name != "/opt/bin/claude.exe" || !strings.HasPrefix(strings.Join(c.args, " "), "--dangerously-skip-permissions --settings") {
		t.Fatalf("claude by path: %+v", c)
	}
}

func TestRunOffRefusesAgents(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	for _, k := range agentMarkers {
		t.Run(k, func(t *testing.T) {
			clearAgentMarkers(t)
			t.Setenv(k, "1")
			calls := fakeExec(t)
			_, err := runTq(t, "run", "--off", "--", "claude")
			if !errors.Is(err, runner.ErrHumanOnly) || len(*calls) != 0 {
				t.Fatalf("%s: want ErrHumanOnly and no launch, got %v %+v", k, err, *calls)
			}
		})
	}
}

func TestRunFromAgentSession(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	t.Setenv("CLAUDECODE", "1")

	// the parent's TQ_MODE passes through, and no bypass is added
	t.Setenv("TQ_MODE", "strict")
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "claude", "-p", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.env["TQ_MODE"] != "strict" || strings.Join(c.args, " ") != "-p x" {
		t.Fatalf("agent launch: %+v", c)
	}
	// looser than the parent: refused
	for _, m := range []string{"default", "relaxed", "custom"} {
		calls := fakeExec(t)
		if _, err := runTq(t, "run", "acme", "--tq-mode", m, "--", "claude"); err == nil || len(*calls) != 0 {
			t.Errorf("parent strict: --tq-mode %s must be refused (%v)", m, err)
		}
	}
	// the same or stricter: fine
	t.Setenv("TQ_MODE", "")
	calls = fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--tq-mode", "strict", "--", "claude"); err != nil || (*calls)[0].env["TQ_MODE"] != "strict" {
		t.Fatalf("parent default -> strict: %v %+v", err, *calls)
	}
	fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--tq-mode", "relaxed", "--", "claude"); err == nil {
		t.Fatal("parent default -> relaxed must be refused")
	}
}
