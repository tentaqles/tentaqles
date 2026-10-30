package integration

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

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

// The plugin's development marketplace must announce plugin.json's version.
func TestDevMarketplaceMatchesPlugin(t *testing.T) {
	root := filepath.Join("..", "..")
	var plugin struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	readJSON(t, filepath.Join(root, "plugin", ".claude-plugin", "plugin.json"), &plugin)
	if plugin.Version == "" {
		t.Fatal("plugin.json has no version")
	}
	var dev struct {
		Plugins []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	readJSON(t, filepath.Join(root, "plugin", ".claude-plugin", "marketplace.json"), &dev)
	found := false
	for _, p := range dev.Plugins {
		if p.Name == plugin.Name {
			found = true
			if p.Version != plugin.Version {
				t.Errorf("plugin/.claude-plugin/marketplace.json version %q, plugin.json %q: bump both", p.Version, plugin.Version)
			}
		}
	}
	if !found {
		t.Errorf("plugin/.claude-plugin/marketplace.json does not list %q", plugin.Name)
	}
}

// The root marketplace (what `/plugin marketplace add tentaqles/tentaqles`
// reads) arrives in the post-release PR, after the tag exists and the
// release workflow passed (docs/RELEASING.md). It pins plugin/ at a release
// tag and that tag's commit: with both ref and sha set Claude Code checks
// out the sha, so a moved tag cannot change what installs. It carries no
// version: the installed version is plugin.json's at the pinned commit. CI
// (test.yml, job marketplace-pin) checks the sha is the tag's commit and on
// main; this test checks the shape.
func TestRootMarketplacePin(t *testing.T) {
	path := filepath.Join("..", "..", ".claude-plugin", "marketplace.json")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		t.Skip(".claude-plugin/marketplace.json is absent: it is added by the first post-release PR (docs/RELEASING.md)")
	}
	var mkt struct {
		Name    string                       `json:"name"`
		Plugins []map[string]json.RawMessage `json:"plugins"`
	}
	readJSON(t, path, &mkt)
	if mkt.Name != "tentaqles" || len(mkt.Plugins) != 1 {
		t.Fatalf("root marketplace: want name tentaqles with one plugin, got %q with %d", mkt.Name, len(mkt.Plugins))
	}
	entry := mkt.Plugins[0]
	var name string
	if err := json.Unmarshal(entry["name"], &name); err != nil || name != "tentaqles" {
		t.Errorf("root marketplace plugin name %q, want tentaqles", name)
	}
	if _, ok := entry["version"]; ok {
		t.Error("root marketplace entry has a version: drop it, the installed version is plugin.json's at the pinned sha")
	}
	var src struct {
		Source string `json:"source"`
		URL    string `json:"url"`
		Path   string `json:"path"`
		Ref    string `json:"ref"`
		SHA    string `json:"sha"`
	}
	if err := json.Unmarshal(entry["source"], &src); err != nil {
		t.Fatalf("root marketplace source is not an object: %v", err)
	}
	if src.Source != "git-subdir" || src.URL != "https://github.com/tentaqles/tentaqles.git" || src.Path != "plugin" {
		t.Errorf("root marketplace source = %+v, want git-subdir of https://github.com/tentaqles/tentaqles.git path plugin", src)
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(src.Ref) {
		t.Errorf("root marketplace ref %q is not a release tag vX.Y.Z", src.Ref)
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(src.SHA) {
		t.Errorf("root marketplace sha %q is not 40 lowercase hex", src.SHA)
	}
}
