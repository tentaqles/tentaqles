package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func toolPayload(t *testing.T, cwd, tool, input string) string {
	t.Helper()
	return `{"tool_name":` + jsonPath(t, tool) + `,"cwd":` + jsonPath(t, cwd) + `,"tool_input":` + input + `}`
}

// askReason parses the PreToolUse ask JSON, failing the test if out isn't one.
func askReason(t *testing.T, out string) string {
	t.Helper()
	var v struct {
		H struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.H.Decision != "ask" || v.H.Event != "PreToolUse" {
		t.Fatalf("not an ask decision: %q (%v)", out, err)
	}
	return v.H.Reason
}

func TestPolicy_ReadEnvDenied_WindowsPath(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	in := `{"file_path":` + jsonPath(t, filepath.Join(dir, ".env")) + `}`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Read", in))
	if code != 2 || !strings.Contains(errOut, "tq dotenv run") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPolicy_ReadSSHKeyDenied(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	in := `{"file_path":` + jsonPath(t, `C:\Users\x\.ssh\id_rsa`) + `}`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Read", in))
	if code != 2 || !strings.Contains(errOut, "tq/secret-store-read") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPolicy_PowerShellGoesThroughIdentityGuard(t *testing.T) {
	isolateHome(t)
	// Neutral cwd: a remote git command must be refused for PowerShell too.
	in := `{"command":"git push origin main"}`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, testutil.TempDir(t), "PowerShell", in))
	if code != 2 || !strings.HasPrefix(errOut, "BLOCKED:") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPolicy_PowerShellEnvDumpDenied(t *testing.T) {
	isolateHome(t)
	in := `{"command":"Get-ChildItem Env:"}`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, testutil.TempDir(t), "PowerShell", in))
	if code != 2 || !strings.Contains(errOut, "tq/env-dump") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestPolicy_MCPWriteAsks(t *testing.T) {
	isolateHome(t)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"},
		toolPayload(t, testutil.TempDir(t), "mcp__n8n__publish_workflow", `{"workflowId":"1"}`))
	if code != 0 || !strings.Contains(askReason(t, out), "tq/mcp-n8n-write") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestPolicy_JSONFlagReportsAsk(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"},
		toolPayload(t, ws, "Bash", `{"command":"gh pr merge 3 --admin"}`))
	if code != 0 || !strings.Contains(out, `"block":false`) || !strings.Contains(out, `"rule":"tq/gh-admin-merge"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestPolicy_PortugueseEditAllowed(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	in := `{"file_path":` + jsonPath(t, filepath.Join(dir, "leia-me.md")) + `,"old_string":"a","new_string":"Configuração não é necessária 🚀"}`
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Edit", in))
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPolicy_ManifestDisablesAndAddsRules(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", `guard:
  disable: [tq/mcp-n8n-write]
  rules:
    - id: acme/no-prod-host
      action: deny
      command: 'prod-db\.acme\.internal'
      reason: never touch the prod database host
`)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "mcp__n8n__publish_workflow", `{}`))
	if code != 0 || out != "" {
		t.Fatalf("disabled rule still fired: code=%d out=%q", code, out)
	}
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":"psql -h prod-db.acme.internal"}`))
	if code != 2 || !strings.Contains(errOut, "acme/no-prod-host") || !strings.Contains(errOut, manifest.FileName) {
		t.Fatalf("manifest rule: code=%d err=%q", code, errOut)
	}
}

func TestPolicy_ProjectRulesOnlyAdd(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	sub := filepath.Join(ws, "app")
	os.MkdirAll(filepath.Join(sub, ".claude"), 0o755)
	// A project file cannot disable built-ins: the "disable" key is ignored.
	rules := `disable: [tq/env-dump]
rules:
  - id: app/no-console
    action: ask
    tool: Edit|Write
    content: 'console\.log'
    reason: use the logger
`
	if err := os.WriteFile(filepath.Join(sub, ".claude", "tq-rules.yaml"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	in := `{"file_path":"x.js","content":"console.log(1)"}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, sub, "Write", in))
	if code != 0 || !strings.Contains(askReason(t, out), "app/no-console") {
		t.Fatalf("project rule: code=%d out=%q", code, out)
	}
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, sub, "Bash", `{"command":"printenv"}`))
	if code != 2 || !strings.Contains(errOut, "tq/env-dump") {
		t.Fatalf("project file disabled a builtin: code=%d err=%q", code, errOut)
	}
}

// addTrustedWorkspace adds a second workspace under an already registered base.
func addTrustedWorkspace(t *testing.T, base, name, body string) string {
	t.Helper()
	root := filepath.Join(base, name)
	os.MkdirAll(root, 0o755)
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
	return root
}

func TestPolicy_CrossClientMCPDenied(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	mine := addTrustedWorkspace(t, base, "personal", "claude:\n  bundle:\n    mcp: [memory]\n")
	addTrustedWorkspace(t, base, "dirty", "claude:\n  bundle:\n    mcp: [n8n, postgres]\n")

	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, mine, "mcp__postgres__query", `{"sql":"select 1"}`))
	if code != 2 || !strings.Contains(errOut, "tq/cross-client-mcp") || !strings.Contains(errOut, `"dirty"`) {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	code, _, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, mine, "mcp__memory__read_graph", `{}`))
	if code != 0 {
		t.Fatalf("own MCP server refused: code=%d", code)
	}
}

func TestPolicy_CommitWithSecretDenied(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@example.invalid"}, {"config", "user.name", "t"}} {
		if out, err := gitcfg.RunGitIn(ws, args...); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	secret := "API" + "_KEY=" + strings.Repeat("z9", 10)
	if err := os.WriteFile(filepath.Join(ws, "cfg.py"), []byte(secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := `git add -A && git commit -m "cfg"`
	// The identity guard needs a matching email; the manifest declares none,
	// so only the policy layer is under test here.
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":`+jsonPath(t, cmd)+`}`))
	if code != 2 || !strings.Contains(errOut, "tq/commit-secret") || !strings.Contains(errOut, "cfg.py:1") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if strings.Contains(errOut, "z9z9") {
		t.Fatal("reason leaks the secret value")
	}
}
