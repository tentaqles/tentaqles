package gitcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// gitGlobal isolates HOME, USERPROFILE and GIT_CONFIG_GLOBAL so these tests can
// drive a real `git config --global` without ever reading or rewriting the
// developer's own ~/.gitconfig. On Windows os.UserHomeDir() reads USERPROFILE,
// so both must be set. It returns the path of the temp global config.
func gitGlobal(t *testing.T, body string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	}
	t.Setenv("HOME", h)
	gc := filepath.Join(h, ".gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.WriteFile(gc, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return gc
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const twoIncludeIf = "[user]\n" +
	"\tname = Renato\n" +
	"\temail = me@corp.com\n" +
	"[includeIf \"gitdir:C:/repos/alpha/\"]\n" +
	"\tpath = C:/repos/alpha/.gitconfig-tentaqles\n" +
	"[includeIf \"gitdir/i:C:/repos/beta with space/\"]\n" +
	"\tpath = C:/repos/beta with space/.gitconfig-tentaqles\n" +
	"[include]\n" +
	"\tpath = ~/.gitconfig-tentaqles\n" +
	"\tpath = /nope/missing.gitconfig\n"

func TestListIncludeIf_ParsesCondAndPath(t *testing.T) {
	gitGlobal(t, twoIncludeIf)
	got, err := ListIncludeIf(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	want := []IncludeIf{
		{Cond: "gitdir:C:/repos/alpha/", Path: "C:/repos/alpha/.gitconfig-tentaqles"},
		{Cond: "gitdir/i:C:/repos/beta with space/", Path: "C:/repos/beta with space/.gitconfig-tentaqles"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListIncludeIf_EmptyConfig(t *testing.T) {
	gitGlobal(t, "")
	got, err := ListIncludeIf(RunGit)
	if err != nil {
		t.Fatalf("an unset key must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// The point of --show-origin: includeIf blocks that live in tq's own managed
// include file are tq's, not drift, and must never be reported or removed.
func TestListIncludeIf_SkipsTqManagedIncludeFile(t *testing.T) {
	h := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	}
	t.Setenv("HOME", h)
	// git --null form: <origin>\0<key>\n<value>\0 per entry.
	out := "file:" + filepath.Join(h, ".gitconfig") + "\x00includeif.gitdir:C:/repos/alpha/.path\nC:/repos/alpha/.gitconfig-tentaqles\x00" +
		"file:" + IncludeFile() + "\x00includeif.gitdir:C:/repos/beta/.path\nC:/repos/beta/.gitconfig-tentaqles\x00"
	got, err := ListIncludeIf(func(...string) (string, error) { return out, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Cond != "gitdir:C:/repos/alpha/" {
		t.Fatalf("%+v", got)
	}
}

func TestListIncludes(t *testing.T) {
	gitGlobal(t, twoIncludeIf)
	got, err := ListIncludes(RunGit)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"~/.gitconfig-tentaqles", "/nope/missing.gitconfig"}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("include %d = %q, want %q", i, got[i], want[i])
		}
	}
}
