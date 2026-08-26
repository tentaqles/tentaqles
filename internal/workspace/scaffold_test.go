package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
)

func fakeGit(...string) (string, error) { return "", nil }

func TestAdd_ScaffoldsEverything(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	base := t.TempDir()
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", Identities: []string{"claude", "gh"}, PermissionMode: "acceptEdits", RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "acme" || ws.Manifest.Claude.PermissionMode != "acceptEdits" {
		t.Fatalf("%+v", ws)
	}
	for _, p := range []string{
		filepath.Join(base, "acme", manifest.FileName),
		filepath.Join(base, "acme", ".gitconfig-tentaqles"),
		paths.IdentityDir("acme", "claude"),
		filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json"),
		paths.IdentityDir("acme", "gh"),
		filepath.Join(home, ".gitconfig-tentaqles"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s", p)
		}
	}
	cfg2, _ := registry.Load()
	if r := resolve.Resolve(filepath.Join(base, "acme", "deep"), cfg2); r.Workspace == nil {
		t.Fatalf("new workspace must resolve as trusted: %+v", r)
	}
}

func TestAdd_RejectsBadNameAndDuplicate(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	if _, err := Add(AddOptions{Base: base, Name: "Bad Name", GitEmail: "x@y", RunGit: fakeGit}); err == nil {
		t.Fatal("bad name accepted")
	}
	if _, err := Add(AddOptions{Base: base, Name: "ok", GitEmail: "x@y", RunGit: fakeGit}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{Base: base, Name: "ok", GitEmail: "x@y", RunGit: fakeGit}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
