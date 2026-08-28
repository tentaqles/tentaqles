package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/doctor"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/testutil"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

type hookExitSentinel struct{}

// runHook drives the root command with stdin/stdout/stderr wired to buffers and
// exitFunc captured, so a blocking decision records a code instead of exiting.
func runHook(t *testing.T, args []string, stdin string) (code int, out, errOut string) {
	t.Helper()
	prevExit := exitFunc
	exitFunc = func(c int) { code = c; panic(hookExitSentinel{}) }
	t.Cleanup(func() { exitFunc = prevExit })

	var stdoutBuf, stderrBuf bytes.Buffer
	root := NewRoot()
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&stdoutBuf)
	root.SetErr(&stderrBuf)
	root.SetArgs(args)

	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(hookExitSentinel); !ok {
					panic(r)
				}
			}
		}()
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	}()
	return code, stdoutBuf.String(), stderrBuf.String()
}

func jsonPath(t *testing.T, p string) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hookPayloadFor(t *testing.T, cwd, command string) string {
	t.Helper()
	return `{"tool_name":"Bash","cwd":` + jsonPath(t, cwd) + `,"tool_input":{"command":` + jsonPath(t, command) + `}}`
}

// setupTrustedWorkspaceWithManifest registers a base under an isolated home,
// writes the manifest body under a v2 header and trusts it. Returns the root.
func setupTrustedWorkspaceWithManifest(t *testing.T, name, body string) string {
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
	mp := filepath.Join(root, manifest.FileName)
	raw := "schema: tentaqles-client-v2\nclient: " + name + "\nidentities: { gh: {} }\n" + body
	if err := os.WriteFile(mp, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	// TQ_WS matches so the env-drift finding does not mask the rule under test.
	t.Setenv("TQ_WS", name)
	return root
}

func TestPreToolUse_NoWorkspace_AllowsLs(t *testing.T) {
	isolateHome(t)
	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, testutil.TempDir(t), "ls"))
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestPreToolUse_NoWorkspace_BlocksPush(t *testing.T) {
	isolateHome(t)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, testutil.TempDir(t), "git push"))
	if code != 2 || !strings.HasPrefix(errOut, "BLOCKED:") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPreToolUse_BlockedCommandFromManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  email: dev@acme.com\n  blocked_commands: [\"gh \"]\n")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "gh pr merge 1"))
	if code != 2 || !strings.Contains(out, `"rule":"blocked-command"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestPreToolUse_CloudBlockedCommandFromManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "cloud:\n  provider: azure\n  blocked_commands: [\"az group delete\"]\n")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "az group delete -n x"))
	if code != 2 || !strings.Contains(out, `"rule":"blocked-command"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestPreToolUse_ToolInputAsString(t *testing.T) {
	isolateHome(t)
	inner := `{"command":"git push"}`
	payload := `{"tool_name":"Bash","cwd":` + jsonPath(t, testutil.TempDir(t)) + `,"tool_input":` + jsonPath(t, inner) + `}`
	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestPreToolUse_NonBashToolAllowed(t *testing.T) {
	isolateHome(t)
	payload := `{"tool_name":"Write","cwd":` + jsonPath(t, testutil.TempDir(t)) + `,"tool_input":{"file_path":"x"}}`
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPreToolUse_MalformedStdinAllows(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, "not json")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPreToolUse_GHUserStubbed(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  user: acme-bot\n")
	prev := lookupGHUser
	lookupGHUser = func(map[string]string) string { return "someone" }
	t.Cleanup(func() { lookupGHUser = prev })

	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "gh pr list"))
	if code != 2 || !strings.Contains(out, `"rule":"gh-user"`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
}

func TestPreToolUse_GHUserNotLookedUpForNonGH(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  user: acme-bot\n")
	prev := lookupGHUser
	called := false
	lookupGHUser = func(map[string]string) string { called = true; return "someone" }
	t.Cleanup(func() { lookupGHUser = prev })

	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, "ls -la"))
	if code != 0 || called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}

func TestPreToolUse_JSONAllowStillExitsZero(t *testing.T) {
	isolateHome(t)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, testutil.TempDir(t), "ls"))
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var d struct {
		Block bool   `json:"block"`
		Rule  string `json:"rule"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if d.Block || d.Rule != "" {
		t.Fatalf("unexpected decision %+v", d)
	}
}

func TestSessionStart_Workspace(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "display_name: Acme\ngit:\n  host: github.com\n  user: acme-bot\n  email: dev@acme.com\n")
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, ws, ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	lines := strings.SplitN(out, "\n", 2)
	if lines[0] != "Client: Acme (en)" {
		t.Fatalf("first line = %q, want %q", lines[0], "Client: Acme (en)")
	}
	if !strings.Contains(out, "Identity: acme") {
		t.Fatalf("missing Identity: acme in %q", out)
	}
	if !strings.Contains(out, "tq doctor:") {
		t.Fatalf("missing tq doctor: block in %q", out)
	}
}

func TestSessionStart_Neutral(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, testutil.TempDir(t), ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	lines := strings.SplitN(out, "\n", 2)
	want := "Client: none (neutral cwd: outside any base)"
	if lines[0] != want {
		t.Fatalf("first line = %q, want %q", lines[0], want)
	}
}

func TestSessionStart_NeverFails(t *testing.T) {
	home := isolateHome(t)
	cfgPath := filepath.Join(home, ".tentaqles", "config.yaml")
	if err := os.MkdirAll(cfgPath, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, testutil.TempDir(t), ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.HasPrefix(out, "Tentaqles: tq could not") {
		t.Fatalf("out=%q", out)
	}
}

func TestGHEnv_StripsAmbientTokens(t *testing.T) {
	base := []string{"PATH=/bin", "GH_TOKEN=ambient", "GITHUB_TOKEN=ambient", "GH_HOST=ghe.example.com", "GH_ENTERPRISE_TOKEN=x", "GITHUB_ENTERPRISE_TOKEN=y", "GH_CONFIG_DIR=/ambient/gh"}
	got := ghEnv(base, map[string]string{"GH_CONFIG_DIR": "/ws/gh"})
	for _, kv := range got {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			t.Fatalf("%s survived: %v", k, got)
		}
	}
	var dirs []string
	for _, kv := range got {
		if strings.HasPrefix(kv, "GH_CONFIG_DIR=") {
			dirs = append(dirs, kv)
		}
	}
	if len(dirs) != 1 || dirs[0] != "GH_CONFIG_DIR=/ws/gh" {
		t.Fatalf("GH_CONFIG_DIR = %v, want exactly [GH_CONFIG_DIR=/ws/gh]", dirs)
	}
	if !containsStr(got, "PATH=/bin") {
		t.Fatalf("PATH dropped: %v", got)
	}
}

func TestGHEnv_NoWorkspaceDirKeepsBase(t *testing.T) {
	base := []string{"PATH=/bin", "GH_TOKEN=ambient"}
	got := ghEnv(base, map[string]string{})
	if len(got) != len(base) || !containsStr(got, "GH_TOKEN=ambient") {
		t.Fatalf("got %v, want base unchanged", got)
	}
}

func TestRenderSessionStart_Neutral(t *testing.T) {
	rep := doctor.CwdReport{Result: resolve.Result{Reason: "outside any base"}}
	got := renderSessionStart(rep, "")
	want := "Client: none (neutral cwd: outside any base)\n\nRules: remote git, gh and cloud CLI commands are blocked here until you cd into a trusted workspace (tq allow <name>).\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderSessionStart_Workspace(t *testing.T) {
	ws := &resolve.Workspace{Name: "acme", Manifest: &manifest.Manifest{
		Client:      "acme",
		DisplayName: "Acme",
		Git:         manifest.Git{Host: "github.com", User: "acme-bot", Email: "dev@acme.com"},
		Cloud:       map[string]any{"provider": "azure", "subscription_name": "acme-prod"},
	}}
	rep := doctor.CwdReport{Result: resolve.Result{Workspace: ws}}
	got := renderSessionStart(rep, "/home/acme/.tentaqles/identities/acme/claude")
	want := "Client: Acme (en)\n" +
		"Git: github.com as acme-bot (dev@acme.com)\n" +
		"Cloud: azure (acme-prod subscription)\n" +
		"Identity: acme · CLAUDE_CONFIG_DIR=/home/acme/.tentaqles/identities/acme/claude · permission_mode=default\n" +
		"\ntq doctor:\n" +
		"- [ok] all checks passed\n" +
		"\nRules: tq blocks git/gh/cloud commands on identity drift (exit 2). Run `tq doctor` for details.\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderSessionStart_UntrustedWithFindings(t *testing.T) {
	ws := &resolve.Workspace{Name: "acme", Manifest: &manifest.Manifest{Client: "acme"}}
	rep := doctor.CwdReport{
		Result:   resolve.Result{Untrusted: ws},
		Findings: []doctor.Finding{{Level: "warn", Code: "untrusted", Workspace: "acme", Msg: "manifest not trusted", Fix: "tq allow acme"}},
	}
	got := renderSessionStart(rep, "")
	if !strings.Contains(got, "- [warn] untrusted: manifest not trusted (→ tq allow acme)") {
		t.Fatalf("missing untrusted bullet: %q", got)
	}
	if !strings.HasPrefix(got, "Client: acme (en)\n") {
		t.Fatalf("first line wrong: %q", got)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func TestPreToolUse_UntrustedWorkspaceSkipsGHLookup(t *testing.T) {
	ws := mkUntrustedWithManifest(t, "acme", "git:\n  user: acme-bot\n")
	prev := lookupGHUser
	called := false
	lookupGHUser = func(map[string]string) string { called = true; return "someone" }
	t.Cleanup(func() { lookupGHUser = prev })

	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "gh pr list"))
	if called {
		t.Fatal("lookupGHUser called for an untrusted workspace")
	}
	if code != 0 {
		t.Fatalf("code=%d (gh in an untrusted ws is judged by untrusted/neutral rules only)", code)
	}
}

// mkUntrustedWithManifest is setupTrustedWorkspaceWithManifest without the trust step.
func mkUntrustedWithManifest(t *testing.T, name, body string) string {
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
	raw := "schema: tentaqles-client-v2\nclient: " + name + "\nidentities: { gh: {} }\n" + body
	if err := os.WriteFile(filepath.Join(root, manifest.FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
