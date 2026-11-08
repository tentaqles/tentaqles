package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/trust"
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

// A github server the render removed while gh was out of scope does not keep the server blocked
// once gh joins the scope and `tentaqles connect gh` writes the catalog's entry: only the live
// sources count for the exemption. A foreign entry in a live source still denies.
func TestHook_RemovedEntryDoesNotBlockConnectedServer(t *testing.T) {
	root, _ := twoBundled(t, "identities: { claude: {} }\nclaude: { bundle: { mcp: [memory] } }\n", "github")
	chdir(t, root)
	old := `{"mcpServers":{"github":{"command":"docker","args":["run","-i","--rm","ghcr.io/github/github-mcp-server"]}}}`
	if err := os.MkdirAll(filepath.Dir(claudeJSONOf("acme")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeJSONOf("acme"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runTq(t, "settings", "render", "acme"); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	removed, err := os.ReadFile(filepath.Join(paths.IdentityDir("acme", "claude"), mcpscope.RemovedFile))
	if err != nil || !strings.Contains(string(removed), "github-mcp-server") {
		t.Fatalf("the render did not keep the removed github entry: %v %s", err, removed)
	}

	// gh joins the scope; the human signs the edited manifest.
	mp := paths.ManifestPath("acme")
	body := "schema: tentaqles-client-v2\nclient: acme\nidentities: { claude: {} }\nscope: [gh]\nclaude: { bundle: { mcp: [memory] } }\n"
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect gh: %v %q", err, out)
	}
	if code, _, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}"); code != 0 {
		t.Errorf("the removed entry still blocks the connected server (%s): %s", rule, errOut)
	}

	// Still denied: the same foreign entry back in a live source.
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, rule, errOut := hookDecision(t, root, "mcp__github__list_issues", "{}"); code != 2 || rule != "tq/cross-client-mcp" {
		t.Errorf("a live foreign entry passed: code=%d %s %q", code, rule, errOut)
	}
}

// A .mcp.json that cannot be read, at the root or in a parent of the cwd up to the root, leaves no
// exemption, though the catalog's entry is in .claude.json.
func TestHook_UnreadableMCPJSONNoExemption(t *testing.T) {
	for _, where := range []string{"root", "parent"} {
		t.Run(where, func(t *testing.T) {
			root, _ := ghScopedTwoBundles(t)
			cwd := filepath.Join(root, "pkg", "sub")
			if err := os.MkdirAll(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			chdir(t, root)
			if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
				t.Fatalf("connect gh: %v %q", err, out)
			}
			dir := root
			if where == "parent" {
				dir = filepath.Join(root, "pkg")
			}
			if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers": {`), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, rule, errOut := hookDecision(t, cwd, "mcp__github__list_issues", "{}")
			if code != 2 || rule != "tq/cross-client-mcp" {
				t.Errorf("code=%d %s %q", code, rule, errOut)
			}
		})
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
