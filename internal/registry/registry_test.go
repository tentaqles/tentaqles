package registry

import (
	"os"
	"path/filepath"
	"testing"

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
