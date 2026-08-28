package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/tentaqles/tentaqles/internal/workspace"
	"gopkg.in/yaml.v3"
)

func fakeGitBundle(...string) (string, error) { return "", nil }

func saveManifestBundle(path string, m *manifest.Manifest) error {
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// mkTrustedWithBundle creates a trusted workspace named "acme" with the
// given bundle set on its manifest.
func mkTrustedWithBundle(t *testing.T, b *manifest.Bundle) *resolve.Workspace {
	t.Helper()
	isolateHome(t)
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	ws, err := workspace.Add(workspace.AddOptions{
		Base: base, Name: "acme", GitEmail: "a@acme.com",
		Identities: []string{"claude"}, RunGit: fakeGitBundle, Trust: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Load(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Claude.Bundle = b
	if err := saveManifestBundle(ws.ManifestPath, m); err != nil {
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
	return res.Workspace
}

func TestBundleSync_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"bundle", "sync", "evil"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func TestBundleDiff_ExitCode(t *testing.T) {
	ws := mkTrustedWithBundle(t, &manifest.Bundle{MCP: []string{"github"}})

	cat := &bundle.Catalog{
		Marketplaces: map[string]bundle.Marketplace{},
		Skills:       map[string]bundle.Skill{},
		MCP: map[string]bundle.MCPServer{
			"github": {"command": "gh-mcp"},
		},
	}
	if err := cat.Save(); err != nil {
		t.Fatal(err)
	}

	var exitCode int
	old := exitFunc
	exitFunc = func(code int) { exitCode = code }
	defer func() { exitFunc = old }()

	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"bundle", "diff", ws.Name})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 recorded, got %d; output: %s", exitCode, out.String())
	}
}
