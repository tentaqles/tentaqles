package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestLoad_Missing_IsEmpty(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c, err := Load()
	if err != nil || len(c.Bases) != 0 {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestAddBase_SaveLoad_Roundtrip_Dedup(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	c, _ := Load()
	if added, err := c.AddBase(base); err != nil || !added {
		t.Fatalf("first add: %v %v", added, err)
	}
	if added, _ := c.AddBase(base + string(os.PathSeparator)); added {
		t.Fatal("trailing separator should dedup")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil || len(c2.Bases) != 1 || c2.Bases[0] != filepath.Clean(base) {
		t.Fatalf("roundtrip: %+v %v", c2, err)
	}
}

// Normalize must resolve a symlink that appears in an existing prefix of the
// path even when the tail of the path doesn't exist yet — this is the
// macOS /var -> /private/var situation (t.TempDir() lives under a symlinked
// ancestor), applied to a not-yet-created child path.
func TestNormalize_ExistingPrefixSymlink_NonexistentTail(t *testing.T) {
	real := testutil.TempDir(t)
	target := filepath.Join(real, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(real, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink (likely missing privilege on this platform/CI): %v", err)
	}

	wantBase, err := Normalize(target)
	if err != nil {
		t.Fatal(err)
	}
	gotBase, err := Normalize(link)
	if err != nil {
		t.Fatal(err)
	}
	if gotBase != wantBase {
		t.Fatalf("Normalize(link) = %q, want %q", gotBase, wantBase)
	}

	// Now a nonexistent child of the symlinked path: Normalize must still
	// resolve the symlinked prefix and append the nonexistent tail.
	gotChild, err := Normalize(filepath.Join(link, "does-not-exist-yet"))
	if err != nil {
		t.Fatal(err)
	}
	wantChild := filepath.Join(wantBase, "does-not-exist-yet")
	if gotChild != wantChild {
		t.Fatalf("Normalize(link/child) = %q, want %q", gotChild, wantChild)
	}
}

func TestAddBase_NonexistentDir_Error(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c, _ := Load()
	if _, err := c.AddBase(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfig_BindFolderUnbind(t *testing.T) {
	c := &Config{}
	if _, ok := c.Folder("acme"); ok {
		t.Fatal("empty config must report no folder")
	}
	dir := t.TempDir()
	if err := c.Bind("acme", dir); err != nil {
		t.Fatal(err)
	}
	normalizedDir, err := Normalize(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.Folder("acme")
	if !ok || !SamePath(got, normalizedDir) {
		t.Fatalf("Folder(acme) = %q, %v; want %q", got, ok, normalizedDir)
	}
	if err := c.Bind("acme", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(c.Clients) != 1 {
		t.Fatalf("rebinding must replace the record: %+v", c.Clients)
	}
	c.Unbind("acme")
	if _, ok := c.Folder("acme"); ok {
		t.Fatal("Unbind must remove the record")
	}
	if err := c.Bind("", dir); err == nil {
		t.Fatal("empty client name must be refused")
	}
	if runtime.GOOS == "windows" {
		if err := c.Bind("acme", dir); err != nil {
			t.Fatal(err)
		}
		if _, ok := c.Folder("ACME"); !ok {
			t.Fatal("on Windows a client name matches without case")
		}
		if err := c.Bind("Acme", dir); err != nil {
			t.Fatal(err)
		}
		if len(c.Clients) != 1 {
			t.Fatalf("on Windows a case-only rebind must keep one entry: %+v", c.Clients)
		}
	}
}

func TestConfig_ClientsRoundTrip(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	folder := testutil.TempDir(t)
	c := &Config{}
	if _, err := c.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("acme", folder); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.Bases) != 1 || !SamePath(c2.Bases[0], base) {
		t.Fatalf("bases after roundtrip: %+v", c2.Bases)
	}
	got, ok := c2.Folder("acme")
	if !ok || !SamePath(got, folder) {
		t.Fatalf("Folder(acme) after roundtrip = %q, %v; want %q", got, ok, folder)
	}
}

func TestConfig_OldFileHasNoClients(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.MkdirAll(paths.Home(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config(), []byte("bases:\n  - /some/base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Bases) != 1 {
		t.Fatalf("bases: %+v", c.Bases)
	}
	if _, ok := c.Folder("acme"); ok {
		t.Fatal("a file written before the record existed must report no folder")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.Config())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "clients") {
		t.Fatalf("an empty record must not be written:\n%s", raw)
	}
}
