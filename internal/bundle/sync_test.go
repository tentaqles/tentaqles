package bundle

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
	"gopkg.in/yaml.v3"
)

// isolateHome creates a temp dir and points TQ_HOME/HOME (or USERPROFILE on
// Windows) at it, so tests never touch the real developer home directory.
// Copied from internal/workspace/scaffold_test.go per the task-5 brief.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	return home
}

func fakeGit(...string) (string, error) { return "", nil }

// setupWorkspace creates a trusted workspace named "acme" with the given
// bundle set on its manifest, returning the resolved workspace.
func setupWorkspace(t *testing.T, b *manifest.Bundle) (*resolve.Workspace, *registry.Config) {
	t.Helper()
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Add(workspace.AddOptions{
		Base: base, Name: "acme", GitEmail: "a@acme.com",
		Identities: []string{"claude"}, RunGit: fakeGit, Trust: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Load(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Claude.Bundle = b
	if err := saveManifest(ws.ManifestPath, m); err != nil {
		t.Fatal(err)
	}

	h, err := trust.HashFile(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}

	cfg2, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	res := resolve.Resolve(ws.Root, cfg2)
	if res.Workspace == nil {
		t.Fatalf("workspace did not resolve as trusted: %+v", res)
	}
	return res.Workspace, cfg2
}

func saveManifest(path string, m *manifest.Manifest) error {
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func identityDirForTest(ws *resolve.Workspace) string {
	return paths.IdentityDir(ws.Name, "claude")
}

func testCatalog(t *testing.T, mktSrcDir, skillSrcDir string) *Catalog {
	t.Helper()
	return &Catalog{
		Marketplaces: map[string]Marketplace{
			"mymkt": {Source: "local", Path: mktSrcDir},
		},
		Skills: map[string]Skill{
			"myskill": {Path: skillSrcDir},
		},
		MCP: map[string]MCPServer{
			"github": {"command": "gh-mcp"},
		},
	}
}

func TestSync_EndToEnd(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# my skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := testCatalog(t, t.TempDir(), skillSrc)

	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Marketplaces: []string{"mymkt"},
		Skills:       []string{"myskill"},
		MCP:          []string{"github"},
	})

	rep, err := Sync(ws, cat, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !rep.Settings {
		t.Fatal("expected settings.json to change")
	}
	if len(rep.Skills) != 1 || rep.Skills[0] != "myskill" {
		t.Fatalf("Skills = %v", rep.Skills)
	}
	if len(rep.MCP) != 1 || rep.MCP[0] != "github" {
		t.Fatalf("MCP = %v", rep.MCP)
	}

	drifts, err := Diff(ws, cat)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("expected no drift after sync, got %+v", drifts)
	}

	// second sync: nothing to do
	rep2, err := Sync(ws, cat, Options{})
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if rep2.Settings {
		t.Fatal("expected no settings change on second sync")
	}
	if len(rep2.Skills) != 0 || len(rep2.MCP) != 0 {
		t.Fatalf("expected no changes on second sync, got skills=%v mcp=%v", rep2.Skills, rep2.MCP)
	}
}

func TestDiff_ReportsMissingAndExtra(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# s"), 0o644)

	cat := testCatalog(t, t.TempDir(), skillSrc)
	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Marketplaces: []string{"mymkt"},
		Skills:       []string{"myskill"},
		MCP:          []string{"github"},
	})

	// no sync yet: everything desired should be reported missing
	drifts, err := Diff(ws, cat)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	kinds := map[string]bool{}
	for _, d := range drifts {
		kinds[d.Kind] = true
	}
	for _, want := range []string{"marketplace-missing", "skill-missing", "mcp-missing"} {
		if !kinds[want] {
			t.Fatalf("expected drift kind %q, got %+v", want, drifts)
		}
	}

	if _, err := Sync(ws, cat, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// now shrink the manifest's bundle so previously-synced entries are extra
	m, err := manifest.Load(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Claude.Bundle = &manifest.Bundle{}
	if err := saveManifest(ws.ManifestPath, m); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	cfg2, _ := registry.Load()
	res := resolve.Resolve(ws.Root, cfg2)
	if res.Workspace == nil {
		t.Fatalf("workspace did not resolve: %+v", res)
	}
	ws2 := res.Workspace

	drifts2, err := Diff(ws2, cat)
	if err != nil {
		t.Fatalf("Diff after shrink: %v", err)
	}
	kinds2 := map[string]bool{}
	for _, d := range drifts2 {
		kinds2[d.Kind] = true
	}
	if !kinds2["skill-extra"] {
		t.Fatalf("expected skill-extra, got %+v", drifts2)
	}
	if !kinds2["mcp-extra"] {
		t.Fatalf("expected mcp-extra, got %+v", drifts2)
	}
}

func TestSync_RefusesInUse(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# s"), 0o644)
	cat := testCatalog(t, t.TempDir(), skillSrc)

	ws, _ := setupWorkspace(t, &manifest.Bundle{Skills: []string{"myskill"}})

	dir := identityDirForTest(ws)
	sessions := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions, "recent.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(ws, cat, Options{}); err == nil {
		t.Fatal("expected Sync to refuse an in-use identity dir")
	}

	if _, err := Sync(ws, cat, Options{Force: true}); err != nil {
		t.Fatalf("Sync with Force should succeed: %v", err)
	}
}

func TestCapture_FromExistingDir(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "settings.json"), `{
		"enabledPlugins": {"superpowers": true, "aws-core@aws": true},
		"extraKnownMarketplaces": {"aws": {"source": {"source": "github", "repo": "aws/plugins"}}}
	}`)
	writeFile(t, filepath.Join(dir, "plugins", "known_marketplaces.json"), `{
		"aws": {"source": {"source": "github", "repo": "aws/plugins"}}
	}`)
	writeFile(t, filepath.Join(dir, "skills", "myskill", "SKILL.md"), "# skill")
	writeFile(t, filepath.Join(dir, ".claude.json"), `{
		"mcpServers": {"github": {"command": "gh-mcp"}}
	}`)

	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if len(c.Bundle.Plugins) != 2 {
		t.Fatalf("Plugins = %v", c.Bundle.Plugins)
	}
	if len(c.Bundle.Marketplaces) != 1 || c.Bundle.Marketplaces[0] != "aws" {
		t.Fatalf("Marketplaces = %v", c.Bundle.Marketplaces)
	}
	if len(c.Bundle.Skills) != 1 || c.Bundle.Skills[0] != "myskill" {
		t.Fatalf("Skills = %v", c.Bundle.Skills)
	}
	if len(c.Bundle.MCP) != 1 || c.Bundle.MCP[0] != "github" {
		t.Fatalf("MCP = %v", c.Bundle.MCP)
	}
	if len(c.Catalog.Marketplaces) != 1 {
		t.Fatalf("Catalog.Marketplaces = %+v", c.Catalog.Marketplaces)
	}
	if len(c.Catalog.Skills) != 1 {
		t.Fatalf("Catalog.Skills = %+v", c.Catalog.Skills)
	}
	if len(c.Catalog.MCP) != 1 {
		t.Fatalf("Catalog.MCP = %+v", c.Catalog.MCP)
	}

	yamlStr := c.BundleYAML()
	full := "schema: tentaqles-client-v2\nclient: x\n" + yamlStr
	tmp := filepath.Join(t.TempDir(), ".tentaqles.yaml")
	if err := os.WriteFile(tmp, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(tmp)
	if err != nil {
		t.Fatalf("BundleYAML output failed to load as manifest: %v\n%s", err, full)
	}
	if m.Claude.Bundle == nil || len(m.Claude.Bundle.Plugins) != 2 {
		t.Fatalf("round-tripped bundle = %+v", m.Claude.Bundle)
	}

	sort.Strings(c.Bundle.Plugins) // sanity: no panic on already-sorted slice
}

func TestDiff_CleanWithNumericField(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# my skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := testCatalog(t, t.TempDir(), skillSrc)
	// int here, float64 once round-tripped through .claude.json
	cat.MCP["github"] = MCPServer{"command": "gh-mcp", "timeout": 30000}

	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Marketplaces: []string{"mymkt"},
		Skills:       []string{"myskill"},
		MCP:          []string{"github"},
	})

	if _, err := Sync(ws, cat, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	drifts, err := Diff(ws, cat)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, dr := range drifts {
		if dr.Kind == "mcp-missing" || dr.Kind == "mcp-extra" {
			t.Fatalf("unexpected mcp drift: %+v (all: %+v)", dr, drifts)
		}
	}
}

func TestSync_StateSavedAfterSkillsWhenMCPFails(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# my skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	cat := testCatalog(t, t.TempDir(), skillSrc)

	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Marketplaces: []string{"mymkt"},
		Skills:       []string{"myskill"},
		MCP:          []string{"github"},
	})

	// Make .claude.json a directory so SyncMCP cannot read/write it.
	dir := identityDirForTest(ws)
	if err := os.MkdirAll(filepath.Join(dir, ".claude.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	rep, err := Sync(ws, cat, Options{})
	if err == nil {
		t.Fatal("expected Sync to fail on MCP step")
	}
	if len(rep.Skills) != 1 || rep.Skills[0] != "myskill" {
		t.Fatalf("partial report Skills = %v, want [myskill]", rep.Skills)
	}

	st := LoadState(dir)
	if len(st.Skills) != 1 || st.Skills[0] != "myskill" {
		t.Fatalf("state Skills = %v, want [myskill]", st.Skills)
	}
}

func TestSync_WarnsWhenNoSessionsDir(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# my skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat := testCatalog(t, t.TempDir(), skillSrc)

	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Skills: []string{"myskill"},
	})

	rep, err := Sync(ws, cat, Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "in-use check skipped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sessions warning, got %v", rep.Warnings)
	}
}
