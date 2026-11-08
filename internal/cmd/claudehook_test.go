package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
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

// setupTrustedWorkspaceWithManifest writes client name (the manifest body after the schema and
// client lines) in a fresh clients folder. It returns the folder.
func setupTrustedWorkspaceWithManifest(t *testing.T, name, body string) string {
	t.Helper()
	return writeClient(t, newBase(t), name, body)
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
	if code != 2 || !strings.HasPrefix(errOut, "BLOCKED [identity/") {
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

// A payload tq cannot read is denied: the hook fails closed.
func TestPreToolUse_MalformedStdinDenies(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, "not json")
	if code != 2 || out != "" || !strings.Contains(errOut, "BLOCKED [tq/guard-error]: the hook payload cannot be read") {
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
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  provider: github\n  user: acme-bot\n  email: dev@acme.com\n")
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, ws, ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	lines := strings.SplitN(out, "\n", 2)
	if lines[0] != "Client: acme" {
		t.Fatalf("first line = %q, want %q", lines[0], "Client: acme")
	}
	if !strings.Contains(out, "Identity: acme") {
		t.Fatalf("missing Identity: acme in %q", out)
	}
	// A manifest that names no language gets the default.
	if !strings.Contains(out, "Language: English (United States) (en-US).") {
		t.Fatalf("missing the default language in %q", out)
	}
	if !strings.Contains(out, "tentaqles doctor:") {
		t.Fatalf("missing tentaqles doctor: block in %q", out)
	}
}

func TestSessionStart_Neutral(t *testing.T) {
	isolateHome(t)
	code, out, errOut := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, testutil.TempDir(t), ""))
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	lines := strings.SplitN(out, "\n", 2)
	want := "Client: none (neutral folder: outside any base)"
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
	want := "Client: none (neutral folder: outside any base)\n\nRules: remote git, gh and cloud CLI commands are blocked here; cd into a client folder first.\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderSessionStart_Workspace(t *testing.T) {
	ws := &resolve.Workspace{Name: "acme", Manifest: &manifest.Manifest{
		Client:     "acme",
		Language:   "pt-BR",
		Git:        manifest.Git{Provider: "github", User: "acme-bot", Email: "dev@acme.com"},
		Identities: map[string]manifest.Identity{"claude": {}, "az": {}},
		Cloud:      map[string]any{"provider": "azure", "also": []any{"aws"}},
	}}
	rep := doctor.CwdReport{Result: resolve.Result{Workspace: ws}}
	got := renderSessionStart(rep, "/home/acme/.tentaqles/identities/acme/claude")
	want := "Client: acme\n" +
		"Language: Portuguese (Brazil) (pt-BR). Write every report, document, email and message for this client's people (clients and teammates) in Portuguese (Brazil).\n" +
		"Git: github as acme-bot (dev@acme.com)\n" +
		"Cloud: azure, aws\n" +
		"Identity: acme · CLAUDE_CONFIG_DIR=/home/acme/.tentaqles/identities/acme/claude\n" +
		"Tools: aws, az, claude, gh. Other providers' CLIs are not on this terminal's PATH.\n" +
		"\ntentaqles doctor:\n" +
		"- [ok] all checks passed\n" +
		"\nRules: tq blocks git, gh and cloud commands that would run under another identity (exit 2). Run `tentaqles doctor` for details.\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
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

// An oversized payload is a protocol violation.
// pre-tool-use must fail closed instead of silently truncating (which would
// make the JSON unparseable and therefore "allow").
func TestPreToolUse_OversizedPayloadFailsClosed(t *testing.T) {
	isolateHome(t)
	pad := strings.Repeat("x", maxHookPayload)
	payload := `{"tool_name":"Bash","pad":"` + pad + `","tool_input":{"command":"ls"}}`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	if code != 2 {
		t.Fatalf("code=%d want 2 (err=%q)", code, errOut)
	}
	if !strings.Contains(errOut, "BLOCKED [tq/guard-error]: the hook payload cannot be read (hook payload exceeds 1 MiB)") {
		t.Fatalf("stderr=%q", errOut)
	}
}

func TestPreToolUse_PayloadAtLimitStillParsed(t *testing.T) {
	isolateHome(t)
	head := `{"tool_name":"Bash","pad":"`
	tail := `","tool_input":{"command":"git push"}}`
	payload := head + strings.Repeat("x", maxHookPayload-len(head)-len(tail)) + tail
	if len(payload) != maxHookPayload {
		t.Fatalf("payload len=%d", len(payload))
	}
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	if code != 2 || !strings.HasPrefix(errOut, "BLOCKED [identity/") || strings.Contains(errOut, "exceeds") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

// session-start must never block: an oversized payload still exits 0.
func TestSessionStart_OversizedPayloadExitsZero(t *testing.T) {
	isolateHome(t)
	payload := `{"pad":"` + strings.Repeat("x", maxHookPayload) + `"}`
	code, out, _ := runHook(t, []string{"claude-hook", "session-start"}, payload)
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "could not resolve this folder") {
		t.Fatalf("stdout=%q", out)
	}
}

// Dropped gh env keys match case-insensitively.
func TestGHEnv_DropsKeysCaseInsensitively(t *testing.T) {
	base := []string{"Gh_Token=t", "github_token=t2", "gH_HoSt=h", "PATH=/bin"}
	got := ghEnv(base, map[string]string{"GH_CONFIG_DIR": "/cfg"})
	for _, kv := range got {
		l := strings.ToLower(kv)
		if strings.HasPrefix(l, "gh_token=") || strings.HasPrefix(l, "github_token=") || strings.HasPrefix(l, "gh_host=") {
			t.Fatalf("ambient var survived: %q (env=%v)", kv, got)
		}
	}
	if len(got) != 2 || got[0] != "PATH=/bin" || got[1] != "GH_CONFIG_DIR=/cfg" {
		t.Fatalf("env=%v", got)
	}
}

// cloud.also names are normalised like cloud.provider: gcloud is gcp.
func TestPreToolUse_CloudAlsoAliasAndPrimaryAlias(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "cloud:\n  provider: azure\n  also: [gcloud]\n")
	for cmd, want := range map[string]int{"gcloud projects list": 0, "az account show": 0, "aws s3 ls": 2} {
		if code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd)); code != want {
			t.Fatalf("%q: code=%d want %d out=%s", cmd, code, want, out)
		}
	}
	ws = setupTrustedWorkspaceWithManifest(t, "acme2", "cloud:\n  provider: gcloud\n")
	for cmd, want := range map[string]int{"gcloud projects list": 0, "gsutil ls": 0, "az account show": 2} {
		if code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, cmd)); code != want {
			t.Fatalf("%q: code=%d want %d out=%s", cmd, code, want, out)
		}
	}
}
