package bundle

import (
	"path/filepath"
	"testing"
)

func TestSyncMCP_MergesAndPrunesOwnedOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")

	initial := map[string]any{
		"mcpServers": map[string]any{
			"keep": map[string]any{"command": "keep-cmd"},
			"old":  map[string]any{"command": "old-cmd"},
		},
		"oauthAccount": map[string]any{"x": float64(1)},
		"projects":     map[string]any{},
	}
	if err := WriteJSONAtomic(path, initial); err != nil {
		t.Fatal(err)
	}

	d := Desired{MCP: map[string]MCPServer{
		"github": {"command": "gh-cmd"},
	}}
	st := &State{MCP: []string{"old"}}

	changed, err := SyncMCP(dir, d, st)
	if err != nil {
		t.Fatalf("SyncMCP: %v", err)
	}
	if len(changed) != 1 || changed[0] != "github" {
		t.Fatalf("changed = %v, want [github]", changed)
	}

	result, err := ReadJSONMap(path)
	if err != nil {
		t.Fatal(err)
	}

	servers, ok := result["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing or wrong type: %v", result["mcpServers"])
	}
	if _, ok := servers["old"]; ok {
		t.Fatalf("old should have been pruned, servers = %v", servers)
	}
	if _, ok := servers["keep"]; !ok {
		t.Fatalf("keep should remain, servers = %v", servers)
	}
	if _, ok := servers["github"]; !ok {
		t.Fatalf("github should be added, servers = %v", servers)
	}

	if oauth, ok := result["oauthAccount"].(map[string]any); !ok || oauth["x"] != float64(1) {
		t.Fatalf("oauthAccount should be untouched, got %v", result["oauthAccount"])
	}
	if _, ok := result["projects"]; !ok {
		t.Fatalf("projects should be untouched")
	}

	if len(st.MCP) != 1 || st.MCP[0] != "github" {
		t.Fatalf("st.MCP = %v, want [github]", st.MCP)
	}
}

func TestSyncMCP_ErrorsWhenMcpServersNotMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := WriteJSONAtomic(path, map[string]any{"mcpServers": "not-a-map"}); err != nil {
		t.Fatal(err)
	}

	d := Desired{MCP: map[string]MCPServer{}}
	st := &State{}
	if _, err := SyncMCP(dir, d, st); err == nil {
		t.Fatalf("expected error when mcpServers is not a map")
	}
}

func TestSyncMCP_CreatesMcpServersWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	d := Desired{MCP: map[string]MCPServer{"github": {"command": "gh"}}}
	st := &State{}

	if _, err := SyncMCP(dir, d, st); err != nil {
		t.Fatalf("SyncMCP: %v", err)
	}

	result, err := ReadJSONMap(filepath.Join(dir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers, ok := result["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers not created: %v", result)
	}
	if _, ok := servers["github"]; !ok {
		t.Fatalf("github missing: %v", servers)
	}
}
