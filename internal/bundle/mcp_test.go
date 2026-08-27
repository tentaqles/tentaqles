package bundle

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if len(changed) != 2 || changed[0] != "-old" || changed[1] != "github" {
		t.Fatalf("changed = %v, want [-old github]", changed)
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

func TestSyncMCP_SecondRunNoChangeAndNoRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")

	d := Desired{MCP: map[string]MCPServer{"github": {"command": "gh"}}}
	st := &State{}
	if _, err := SyncMCP(dir, d, st); err != nil {
		t.Fatalf("first SyncMCP: %v", err)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	st2 := &State{MCP: st.MCP}
	changed, err := SyncMCP(dir, d, st2)
	if err != nil {
		t.Fatalf("second SyncMCP: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want empty on unchanged second run", changed)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf(".claude.json was rewritten: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestSyncMCP_ReportsRemovals(t *testing.T) {
	dir := t.TempDir()

	d := Desired{MCP: map[string]MCPServer{"github": {"command": "gh"}}}
	st := &State{}
	if _, err := SyncMCP(dir, d, st); err != nil {
		t.Fatalf("first SyncMCP: %v", err)
	}

	d2 := Desired{MCP: map[string]MCPServer{}}
	st2 := &State{MCP: st.MCP}
	changed, err := SyncMCP(dir, d2, st2)
	if err != nil {
		t.Fatalf("second SyncMCP: %v", err)
	}
	if len(changed) != 1 || changed[0] != "-github" {
		t.Fatalf("changed = %v, want [-github]", changed)
	}
	if len(st2.MCP) != 0 {
		t.Fatalf("st2.MCP = %v, want empty", st2.MCP)
	}

	result, err := ReadJSONMap(filepath.Join(dir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers, _ := result["mcpServers"].(map[string]any)
	if _, ok := servers["github"]; ok {
		t.Fatalf("github should have been removed: %v", servers)
	}
}

func TestSyncMCP_RejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	d := Desired{MCP: map[string]MCPServer{"": {"command": "gh"}}}
	st := &State{}
	if _, err := SyncMCP(dir, d, st); err == nil {
		t.Fatalf("expected error for invalid (empty) mcp server name")
	}
}

func TestSyncMCP_IdempotentWithNumericField(t *testing.T) {
	dir := t.TempDir()

	// timeout arrives from YAML as an int, but round-trips through the
	// JSON file as a float64.
	d := Desired{MCP: map[string]MCPServer{
		"x": {"command": "x", "timeout": 30000},
	}}
	st := &State{}

	changed, err := SyncMCP(dir, d, st)
	if err != nil {
		t.Fatalf("SyncMCP: %v", err)
	}
	if len(changed) != 1 || changed[0] != "x" {
		t.Fatalf("first sync changed = %v, want [x]", changed)
	}

	path := filepath.Join(dir, ".claude.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := info.ModTime()

	changed, err = SyncMCP(dir, d, st)
	if err != nil {
		t.Fatalf("second SyncMCP: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("second sync changed = %v, want []", changed)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(before) {
		t.Fatalf("file was rewritten: mtime %v -> %v", before, info.ModTime())
	}
}
