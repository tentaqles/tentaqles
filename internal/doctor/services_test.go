package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// servicesWorkspace writes and trusts a workspace with the given extra manifest lines.
func servicesWorkspace(t *testing.T, extra string) (*registry.Config, string) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	body := "schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@acme.test }\nidentities: { claude: {} }\n" + extra
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.IdentityDir("acme", "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

func writeClaudeJSON(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func finding(fs []Finding, code string) (Finding, bool) {
	for _, f := range fs {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}

func TestChatEmail_MCPEntryPresent(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: slack }\nemail: { provider: gmail }\n")
	writeClaudeJSON(t, `{"mcpServers": {"slack": {"type": "http", "url": "https://mcp.example.test"}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if f, ok := finding(fs, "chat-ok"); !ok || f.Level != "ok" || !strings.Contains(f.Msg, "slack") {
		t.Fatalf("%+v", fs)
	}
	f, ok := finding(fs, "email-mcp-missing")
	if !ok || f.Level != "warn" || !strings.Contains(f.Msg, "gmail") || !strings.Contains(f.Fix, "claude.bundle.mcp") {
		t.Fatalf("%+v", fs)
	}
}

// Connectors also live in the workspace's project .mcp.json and in .claude.json under
// projects[<folder>].mcpServers; neither is a missing entry. Another folder's project entry is.
func TestChatEmail_ProjectScopedSources(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: slack }\nemail: { provider: gmail }\n")
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": {"slack": {"command": "x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.ToSlash(filepath.Join(filepath.Dir(root), "elsewhere"))
	writeClaudeJSON(t, `{"projects": {`+fmt.Sprintf("%q", other)+`: {"mcpServers": {"gmail": {"command": "y"}}}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if !has(fs, "chat-ok") {
		t.Fatalf("<root>/.mcp.json counts: %+v", fs)
	}
	if !has(fs, "email-mcp-missing") {
		t.Fatalf("another folder's project entry does not count: %+v", fs)
	}
	writeClaudeJSON(t, `{"projects": {`+fmt.Sprintf("%q", filepath.ToSlash(root))+`: {"mcpServers": {"gmail": {"command": "y"}}}}}`)
	if fs = Run(cfg, deps(nil, root, nil)); !has(fs, "email-ok") {
		t.Fatalf("projects[<root>].mcpServers counts: %+v", fs)
	}
}

func TestChatEmail_NoneAndNoFile(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: none }\nemail: { provider: outlook }\n")
	fs := Run(cfg, deps(nil, root, nil))
	if has(fs, "chat-ok") || has(fs, "chat-mcp-missing") {
		t.Fatalf("chat none is not checked: %+v", fs)
	}
	if !has(fs, "email-mcp-missing") {
		t.Fatalf("no .claude.json means the entry is missing: %+v", fs)
	}
}

// A user may name the server after the connector ("slack-mcp"); the provider id inside the key
// is enough.
func TestChatEmail_LooseServerName(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: teams }\n")
	writeClaudeJSON(t, `{"mcpServers": {"ms-Teams-mcp": {"command": "x"}}}`)
	if fs := Run(cfg, deps(nil, root, nil)); !has(fs, "chat-ok") {
		t.Fatalf("%+v", fs)
	}
}

// The provider id must be a whole token of the key: "steamship" is not a Teams server.
func TestChatEmail_LooseMatchNeedsTokenBoundary(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: teams }\n")
	writeClaudeJSON(t, `{"mcpServers": {"steamship": {"command": "x"}, "teamsy": {"command": "y"}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if has(fs, "chat-ok") || !has(fs, "chat-mcp-missing") {
		t.Fatalf("%+v", fs)
	}
}

func TestAllChecksPassedWithOnlyOKFindings(t *testing.T) {
	fs := finish([]Finding{{Level: "ok", Code: "chat-ok", Msg: "x"}})
	if !has(fs, "ok") {
		t.Fatalf("%+v", fs)
	}
	fs = finish([]Finding{{Level: "warn", Code: "w", Msg: "x"}})
	if has(fs, "ok") {
		t.Fatalf("%+v", fs)
	}
}
