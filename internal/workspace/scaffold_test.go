package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func fakeGit(...string) (string, error) { return "", nil }

// tq add creates the protected layout: a name-only marker in the
// root, the manifest under $TQ_HOME/identities/<name>/manifest.yaml, private
// to the user.
func TestAdd_CreatesMarkerAndProtectedManifest(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}
	pp := paths.ManifestPath("acme")
	if !registry.SamePath(ws.ManifestPath, pp) {
		t.Fatalf("%+v", ws)
	}
	marker, err := os.ReadFile(filepath.Join(base, "acme", manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != string(manifest.MarkerBytes("acme")) {
		t.Fatalf("root file must be the name-only marker:\n%s", marker)
	}
	raw, err := os.ReadFile(pp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "schema: tentaqles-client-v2") || !strings.Contains(string(raw), "email: m@acme.com") || !strings.HasPrefix(string(raw), "# tentaqles workspace manifest") {
		t.Fatalf("protected manifest:\n%s", raw)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{pp: 0o600, filepath.Dir(pp): 0o700, paths.IdentitiesRoot(): 0o700, paths.Home(): 0o700} {
			fi, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != want {
				t.Errorf("%s mode %o, want %o", p, fi.Mode().Perm(), want)
			}
		}
	}
	cfg2, _ := registry.Load()
	r := resolve.Resolve(filepath.Join(base, "acme", "deep"), cfg2)
	if r.Workspace == nil || r.Workspace.Manifest.Git.Email != "m@acme.com" {
		t.Fatalf("%+v", r)
	}
}

func TestAdd_RefusesALeftoverProtectedManifest(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	pp := paths.ManifestPath("acme")
	os.MkdirAll(filepath.Dir(pp), 0o700)
	os.WriteFile(pp, []byte("schema: tentaqles-client-v2\nclient: acme\n"), 0o600)
	if _, err := Add(AddOptions{Base: base, Name: "acme", GitEmail: "x@y", RunGit: fakeGit}); err == nil || !strings.Contains(err.Error(), pp) {
		t.Fatalf("a leftover protected manifest must not be overwritten: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "acme")); err == nil {
		t.Fatal("nothing may be scaffolded")
	}
}

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

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", Identities: []string{"claude", "gh"}, RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "acme" {
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

func TestAdd_BindsTheClientToItsFolder(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	cfg.Save()

	ws, err := Add(AddOptions{Base: base, Name: "acme", GitName: "Maria", GitEmail: "m@acme.com", RunGit: fakeGit})
	if err != nil {
		t.Fatal(err)
	}
	cfg2, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg2.Folder("acme")
	if !ok || !registry.SamePath(got, ws.Root) {
		t.Fatalf("Folder(acme) = %q, %v; want %q", got, ok, ws.Root)
	}
	raw, err := os.ReadFile(ws.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	normalizedBase, err := registry.Normalize(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), base) || strings.Contains(string(raw), normalizedBase) {
		t.Fatalf("the manifest must hold names only, not the base folder:\n%s", raw)
	}
}

func TestAdd_RejectsControlCharsInGitFields(t *testing.T) {
	isolateHome(t)
	base := testutil.TempDir(t)
	if _, err := Add(AddOptions{Base: base, Name: "evil", GitEmail: "a@b\n[core]\n\tsshCommand = evil", RunGit: fakeGit}); err == nil {
		t.Fatal("expected error for control chars in git-email")
	}
	if _, statErr := os.Stat(filepath.Join(base, "evil")); statErr == nil {
		t.Fatal("workspace dir must not be created when git fields are invalid")
	}
	if _, err := Add(AddOptions{Base: base, Name: "evil2", GitEmail: "a@b", GitName: "Bad\nName", RunGit: fakeGit}); err == nil {
		t.Fatal("expected error for control chars in git-name")
	}
	if _, statErr := os.Stat(filepath.Join(base, "evil2")); statErr == nil {
		t.Fatal("workspace dir must not be created when git fields are invalid")
	}
}
