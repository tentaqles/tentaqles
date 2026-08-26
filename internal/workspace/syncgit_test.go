package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
)

// An untrusted workspace's .gitconfig-tentaqles must never be wired into the
// git include chain: git would otherwise honour whatever a tampered file says
// for every repo under that root.
func TestSyncGit_ExcludesUntrusted(t *testing.T) {
	home := isolateHome(t)
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Trusted: created (and allowed) through Add.
	good, err := Add(AddOptions{Base: base, Name: "good", GitEmail: "g@x.io", RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}

	// Untrusted: manifest written by hand, never allowed.
	evilRoot := filepath.Join(base, "evil")
	if err := os.MkdirAll(evilRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evilRoot, manifest.FileName),
		[]byte("schema: tentaqles-client-v2\nclient: evil\ngit: { email: e@x.io }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg2, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncGit(cfg2); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".gitconfig-tentaqles"))
	if err != nil {
		t.Fatal(err)
	}
	got := filepath.ToSlash(string(raw))
	if !strings.Contains(got, filepath.ToSlash(good.Root)) {
		t.Fatalf("trusted root %s missing from include file:\n%s", good.Root, got)
	}
	if strings.Contains(got, filepath.ToSlash(evilRoot)) {
		t.Fatalf("untrusted root %s must not be included:\n%s", evilRoot, got)
	}
}
