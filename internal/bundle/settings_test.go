package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
)

func TestCompute_ResolvesAndErrors(t *testing.T) {
	cat := &Catalog{
		Marketplaces: map[string]Marketplace{
			"aws": {Source: "github", Repo: "org/aws-marketplace"},
		},
		Skills: map[string]Skill{
			"known-skill": {Path: "/some/path"},
		},
		MCP: map[string]MCPServer{
			"known-mcp": {"command": "foo"},
		},
	}

	b := &manifest.Bundle{
		Marketplaces: []string{"aws"},
		Plugins:      []string{"aws-core@aws"},
		Skills:       []string{"unknown-skill"},
		MCP:          []string{"known-mcp"},
	}

	d, errs := Compute(b, cat)

	if len(errs) != 1 {
		t.Fatalf("expected exactly 1 error (unknown skill), got %d: %v", len(errs), errs)
	}

	if !d.EnabledPlugins["aws-core@aws"] {
		t.Errorf("expected EnabledPlugins[aws-core@aws]=true, got %v", d.EnabledPlugins)
	}
	if _, ok := d.Marketplaces["aws"]; !ok {
		t.Errorf("expected Marketplaces[aws] to be set, got %v", d.Marketplaces)
	}
	if _, ok := d.Skills["unknown-skill"]; ok {
		t.Errorf("unknown skill should not appear in Desired.Skills")
	}
	if got, ok := d.MCP["known-mcp"]; !ok || got["command"] != "foo" {
		t.Errorf("expected known-mcp to be copied into Desired.MCP, got %v", d.MCP)
	}
}

func TestSyncSettings_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	initial := map[string]any{
		"permissions":    map[string]any{"allow": []any{"Bash(git *)"}},
		"env":            map[string]any{"FOO": "bar"},
		"enabledPlugins": map[string]any{"old": true},
	}
	writeJSON(t, settingsPath, initial)

	d := Desired{
		EnabledPlugins: map[string]bool{"aws-core@aws": true},
		Marketplaces:   map[string]map[string]any{"aws": {"source": map[string]any{"source": "github"}}},
		Skills:         map[string]string{},
		MCP:            map[string]MCPServer{},
	}

	changed, err := SyncSettings(dir, d)
	if err != nil {
		t.Fatalf("SyncSettings: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on first sync")
	}

	got := readJSON(t, settingsPath)

	if _, ok := got["permissions"]; !ok {
		t.Errorf("permissions key was dropped: %v", got)
	}
	if _, ok := got["env"]; !ok {
		t.Errorf("env key was dropped: %v", got)
	}

	enabled, ok := got["enabledPlugins"].(map[string]any)
	if !ok {
		t.Fatalf("enabledPlugins missing or wrong type: %v", got["enabledPlugins"])
	}
	if _, ok := enabled["old"]; ok {
		t.Errorf("old enabledPlugins entry should be gone, got %v", enabled)
	}
	if v, ok := enabled["aws-core@aws"]; !ok || v != true {
		t.Errorf("expected aws-core@aws=true, got %v", enabled)
	}
}

func TestSyncSettings_Idempotent(t *testing.T) {
	dir := t.TempDir()

	d := Desired{
		EnabledPlugins: map[string]bool{"aws-core@aws": true},
		Marketplaces:   map[string]map[string]any{"aws": {"source": map[string]any{"source": "github"}}},
		Skills:         map[string]string{},
		MCP:            map[string]MCPServer{},
	}

	changed, err := SyncSettings(dir, d)
	if err != nil {
		t.Fatalf("first SyncSettings: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on first sync")
	}

	changed, err = SyncSettings(dir, d)
	if err != nil {
		t.Fatalf("second SyncSettings: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false on second, idempotent sync")
	}
}

func TestWriteJSONAtomic_NoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if err := WriteJSONAtomic(path, map[string]any{"a": 1}); err != nil {
		t.Fatalf("WriteJSONAtomic: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" || (len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] != ".json" && e.Name() != "settings.json") {
			// crude leftover-tmp check
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected settings.json to exist: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) >= 4 && name[:4] == "tmp-" {
			t.Errorf("leftover temp file: %s", name)
		}
		if containsTmpMarker(name) {
			t.Errorf("leftover temp file: %s", name)
		}
	}
}

func containsTmpMarker(name string) bool {
	for i := 0; i+5 <= len(name); i++ {
		if name[i:i+5] == ".tmp-" {
			return true
		}
	}
	return false
}

func writeJSON(t *testing.T, path string, m map[string]any) {
	t.Helper()
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}
