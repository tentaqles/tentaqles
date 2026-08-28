package integration

import (
	"github.com/tentaqles/tentaqles/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
)

func git(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestGitIdentity_InsideWorkspaceUsesEmail_OutsideRefused(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// Resolve symlinks / 8.3 short names (macOS /var, Windows RUNNER~1) so the
	// includeIf gitdir written by gitcfg matches git's canonical gitdir.
	home := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(""), 0o600)

	base := filepath.Join(home, "work")
	ws := filepath.Join(base, "acme")
	repo := filepath.Join(ws, "repo")
	os.MkdirAll(repo, 0o755)

	if err := gitcfg.WriteWorkspace(ws, "Acme Dev", "dev@acme.com"); err != nil {
		t.Fatal(err)
	}
	if err := gitcfg.Sync([]string{ws}); err != nil {
		t.Fatal(err)
	}
	if err := gitcfg.EnsureGlobal(gitcfg.RunGit); err != nil {
		t.Fatal(err)
	}

	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	git(t, repo, "add", "f")
	if out, err := git(t, repo, "commit", "-q", "-m", "x"); err != nil {
		t.Fatalf("commit inside workspace failed: %v\n%s", err, out)
	}
	email, _ := git(t, repo, "log", "-1", "--format=%ae")
	if email != "dev@acme.com" {
		t.Fatalf("email %q", email)
	}

	outside := filepath.Join(home, "elsewhere")
	os.MkdirAll(outside, 0o755)
	git(t, outside, "init", "-q")
	os.WriteFile(filepath.Join(outside, "f"), []byte("x"), 0o644)
	git(t, outside, "add", "f")
	out, err := git(t, outside, "commit", "-q", "-m", "x")
	if err == nil {
		t.Fatalf("commit outside workspace must be refused, got success:\n%s", out)
	}
}
