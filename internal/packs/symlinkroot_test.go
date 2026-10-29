package packs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CI on macOS (PR #50): /var is a symlink to /private/var. The workspace
// root is spelled through the link while Cwd() is the final path, so a file
// named from the cwd must still be inside the roots.
func TestReadLocalUnderASymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a macOS and Linux case: Windows junctions resolve through FinalPath on both sides")
	}
	isolate(t)
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.MkdirAll(filepath.Join(real, "supabase", ".temp"), 0o755)
	os.WriteFile(filepath.Join(real, "supabase", ".temp", "project-ref"), []byte("aaaaaaaaaaaaaaaaaaaa\n"), 0o644)
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	g := NewGuardCtx(GuardEnv{Workspace: "acme", WorkspaceRoot: link, RepoRoot: link, Cwd: link}, "db")
	for _, p := range []string{
		filepath.Join(g.Cwd(), "supabase", ".temp", "project-ref"), // the final path
		filepath.Join(link, "supabase", ".temp", "project-ref"),
		"supabase/.temp/project-ref",
	} {
		if _, err := g.ReadLocal(p, 256); err != nil {
			t.Errorf("ReadLocal(%s): %v", p, err)
		}
	}
	if _, err := g.ReadLocal(filepath.Join(base, "outside"), 256); err == nil {
		t.Error("a file outside the roots was read")
	}
}
