package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func fakeGit(...string) (string, error) { return "", nil }

// isolateHome creates a temp dir and points TQ_HOME/HOME (or USERPROFILE on
// Windows) at it, so gitcfg.Sync/EnsureGlobal never touch the real developer
// home directory. Returns the temp home dir.
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

func TestAdd_ScaffoldsEverything(t *testing.T) {
	home := isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", Identities: []string{"claude", "gh"}, PermissionMode: "acceptEdits", RunGit: fakeGit, Trust: true})
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
	isolateHome(t)
	base := testutil.TempDir(t)
	if _, err := Add(AddOptions{Base: base, Name: "Bad Name", GitEmail: "x@y", RunGit: fakeGit, Trust: true}); err == nil {
		t.Fatal("bad name accepted")
	}
	if _, err := Add(AddOptions{Base: base, Name: "ok", GitEmail: "x@y", RunGit: fakeGit, Trust: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{Base: base, Name: "ok", GitEmail: "x@y", RunGit: fakeGit, Trust: true}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestAdd_RejectsControlCharsInGitFields(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	if _, err := Add(AddOptions{Base: base, Name: "evil", GitEmail: "a@b\n[core]\n\tsshCommand = evil", RunGit: fakeGit, Trust: true}); err == nil {
		t.Fatal("expected error for control chars in git-email")
	}
	if _, statErr := os.Stat(filepath.Join(base, "evil")); statErr == nil {
		t.Fatal("workspace dir must not be created when git fields are invalid")
	}
	if _, err := Add(AddOptions{Base: base, Name: "evil2", GitEmail: "a@b", GitName: "Bad\nName", RunGit: fakeGit, Trust: true}); err == nil {
		t.Fatal("expected error for control chars in git-name")
	}
	if _, statErr := os.Stat(filepath.Join(base, "evil2")); statErr == nil {
		t.Fatal("workspace dir must not be created when git fields are invalid")
	}
}

// TestAdd_TrustFalseLeavesUntrusted checks that Trust:false still scaffolds
// the workspace on disk but leaves it untrusted and out of git's include
// chain.
func TestAdd_TrustFalseLeavesUntrusted(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", RunGit: fakeGit, Trust: false})
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil || ws.Root == "" || ws.Manifest == nil {
		t.Fatalf("expected a workspace value, got %+v", ws)
	}
	if trust.IsTrusted(ws.Hash) {
		t.Fatal("workspace should not be trusted when Trust is false")
	}
	inc, err := os.ReadFile(gitcfg.IncludeFile())
	if err == nil && strings.Contains(string(inc), ws.Root) {
		t.Fatalf("untrusted root leaked into include file:\n%s", inc)
	}
	// Everything else should still be on disk.
	if _, err := os.Stat(gitcfg.WorkspaceFile(ws.Root)); err != nil {
		t.Fatalf("missing workspace git identity file: %v", err)
	}
}
