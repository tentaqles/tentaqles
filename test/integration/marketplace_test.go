package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

type marketplaceFile struct {
	Name    string `json:"name"`
	Plugins []struct {
		Name    string          `json:"name"`
		Source  json.RawMessage `json:"source"`
		Version string          `json:"version"`
	} `json:"plugins"`
}

type gitSubdirSource struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Ref    string `json:"ref"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// The root marketplace (what `/plugin marketplace add tentaqles/tentaqles`
// reads) and the plugin's own development marketplace must announce the
// version plugin.json carries, and the root entry must point at the plugin/
// subdirectory of this repo at a release tag. Claude Code makes a new cache
// copy only when the version changes, so a drift here ships stale code. The
// release workflow checks the ref against the pushed tag.
func TestRootMarketplaceMatchesPlugin(t *testing.T) {
	root := filepath.Join("..", "..")

	var plugin struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	readJSON(t, filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"), &plugin)
	if plugin.Version == "" {
		t.Fatal("plugin.json has no version")
	}

	var rootMkt marketplaceFile
	readJSON(t, filepath.Join(root, ".claude-plugin", "marketplace.json"), &rootMkt)
	if rootMkt.Name != "tentaqles" || len(rootMkt.Plugins) != 1 {
		t.Fatalf("root marketplace: want name tentaqles with one plugin, got %q with %d", rootMkt.Name, len(rootMkt.Plugins))
	}
	entry := rootMkt.Plugins[0]
	if entry.Name != plugin.Name {
		t.Errorf("root marketplace plugin name %q, plugin.json %q", entry.Name, plugin.Name)
	}
	if entry.Version != plugin.Version {
		t.Errorf("root marketplace version %q, plugin.json %q: bump both", entry.Version, plugin.Version)
	}
	var src gitSubdirSource
	if err := json.Unmarshal(entry.Source, &src); err != nil {
		t.Fatalf("root marketplace source is not an object: %v", err)
	}
	if src.Source != "git-subdir" || src.URL != "https://github.com/tentaqles/tentaqles.git" || src.Path != "plugin" {
		t.Errorf("root marketplace source = %+v, want git-subdir of https://github.com/tentaqles/tentaqles.git path plugin", src)
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(src.Ref) {
		t.Errorf("root marketplace ref %q is not a release tag vX.Y.Z", src.Ref)
	}

	var devMkt marketplaceFile
	readJSON(t, filepath.Join(root, "plugin", ".claude-plugin", "marketplace.json"), &devMkt)
	for _, p := range devMkt.Plugins {
		if p.Name == plugin.Name && p.Version != plugin.Version {
			t.Errorf("plugin/.claude-plugin/marketplace.json version %q, plugin.json %q", p.Version, plugin.Version)
		}
	}
}
