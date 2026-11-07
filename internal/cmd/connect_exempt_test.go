package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// L-17: only the entry `tentaqles connect` writes is exempt from tq/cross-client-mcp. A server of
// the same name with any other entry, in any source the harness loads, stays denied as another
// client's; a server whose only entry is the catalog's passes.

const foreignGitHub = `{"mcpServers":{"github":{"type":"http","url":"https://mcp.beta-corp.example/"}}}`

// gh in acme's scope, memory in acme's bundle, github in beta's bundle: the name is another
// client's by bundle, and only acme's connect entry can make it acme's.
func ghScopedTwoBundles(t *testing.T) (root, beta string) {
	t.Helper()
	return twoBundled(t, "identities: { claude: {} }\nscope: [gh]\nclaude: { bundle: { mcp: [memory] } }\n", "github")
}

func TestHook_ForeignSameNameInProjectMCPJSONDenied(t *testing.T) {
	root, _ := ghScopedTwoBundles(t)
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(foreignGitHub), 0o600); err != nil {
		t.Fatal(err)
	}
	code, category, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}")
	if code != 2 || category != policy.CategoryIdentity || rule != "tq/cross-client-mcp" ||
		!strings.Contains(errOut, `MCP server "github" belongs to client "beta"`) {
		t.Errorf("code=%d %s/%s %q", code, category, rule, errOut)
	}
}

func TestHook_ForeignBesideCatalogEntryDenied(t *testing.T) {
	root, _ := ghScopedTwoBundles(t)
	chdir(t, root)
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect gh: %v %q", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(foreignGitHub), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}")
	if code != 2 || rule != "tq/cross-client-mcp" || !strings.Contains(errOut, `belongs to client "beta"`) {
		t.Errorf("code=%d %s %q", code, rule, errOut)
	}
}

// Only the catalog's entry in .claude.json, and nothing else of that name: the call passes.
func TestHook_OnlyCatalogEntryPasses(t *testing.T) {
	root, _ := ghScopedTwoBundles(t)
	chdir(t, root)
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect gh: %v %q", err, out)
	}
	if code, _, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}"); code != 0 {
		t.Errorf("the catalog's own entry denied (%s): %s", rule, errOut)
	}
}

// A .claude.json that cannot be read leaves no exemption: the catalog's entry in .mcp.json alone
// is not enough while the identity's own config cannot be checked.
func TestHook_UnreadableConfigNoExemption(t *testing.T) {
	root, _ := ghScopedTwoBundles(t)
	chdir(t, root)
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect gh: %v %q", err, out)
	}
	if err := os.WriteFile(claudeJSONOf("acme"), []byte(`{"mcpServers": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(catalog), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}")
	if code != 2 || rule != "tq/cross-client-mcp" {
		t.Errorf("code=%d %s %q", code, rule, errOut)
	}
}
