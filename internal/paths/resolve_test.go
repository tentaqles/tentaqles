package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// symlinkedRoot makes real/ and link -> real; it skips when the OS refuses a
// symlink (Windows without the privilege or developer mode).
func symlinkedRoot(t *testing.T) (real, link string) {
	t.Helper()
	base := t.TempDir()
	real = filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "infra", "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(base, "link")
	if runtime.GOOS == "windows" {
		// filepath.EvalSymlinks does not follow junctions, and a symlink
		// needs a privilege; packs resolves Windows paths through the
		// handle-based packs.FinalPath instead.
		t.Skip("symlinked roots are a macOS and Linux case (/var -> /private/var)")
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	return real, link
}

// CI on macOS: /var is a symlink to /private/var, so a root spelled through
// the link and a cwd the OS reports resolved must still nest.
func TestRelThroughASymlinkedRoot(t *testing.T) {
	real, link := symlinkedRoot(t)
	for _, c := range []struct{ root, p, want string }{
		{link, filepath.Join(real, "infra", "envs"), "infra/envs"},
		{real, filepath.Join(link, "infra"), "infra"},
		{link, real, "."},
		{link, filepath.Join(link, "infra", "new-not-yet"), "infra/new-not-yet"},
	} {
		got, err := Rel(c.root, c.p)
		if err != nil || filepath.ToSlash(got) != c.want {
			t.Errorf("Rel(%s, %s) = %q, %v; want %q", c.root, c.p, got, err, c.want)
		}
	}
	if r, err := Rel(link, filepath.Dir(real)); err == nil && !IsOutside(r) {
		t.Errorf("a parent of the root nests: %q", r)
	}
	if Resolve(filepath.Join(link, "infra", "missing")) != Resolve(filepath.Join(real, "infra", "missing")) {
		t.Errorf("Resolve keeps the missing tail on the resolved prefix: %q vs %q", Resolve(filepath.Join(link, "infra", "missing")), Resolve(filepath.Join(real, "infra", "missing")))
	}
}
