package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
)

func TestWorktreePrunableRule(t *testing.T) {
	old := 10 * 24 * time.Hour
	cases := []struct {
		w    worktree
		want bool
	}{
		{worktree{Merged: true, Age: old}, true},
		{worktree{Merged: true, Age: time.Hour}, false},
		{worktree{Merged: true, Age: old, Dirty: true}, false},
		{worktree{Merged: true, Age: old, Locked: true}, false},
		{worktree{Merged: false, Age: old}, false},
		{worktree{Missing: true}, true},
	}
	for i, c := range cases {
		if got := c.w.prunable(3 * 24 * time.Hour); got != c.want {
			t.Errorf("case %d: prunable=%v want %v (%+v)", i, got, c.want, c.w)
		}
	}
}

func TestRepoWorktreesReadsRealRepo(t *testing.T) {
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		if out, err := gitcfg.RunGitIn(dir, args...); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	run(repo, "config", "user.email", "t@example.invalid")
	run(repo, "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	run(repo, "add", "a.txt")
	run(repo, "commit", "-qm", "init")
	merged := filepath.Join(t.TempDir(), "wt-merged")
	run(repo, "worktree", "add", "-q", merged, "-b", "done")
	wip := filepath.Join(t.TempDir(), "wt-wip")
	run(repo, "worktree", "add", "-q", wip, "-b", "wip")
	os.WriteFile(filepath.Join(wip, "b.txt"), []byte("b"), 0o644)
	run(wip, "add", "b.txt")
	run(wip, "commit", "-qm", "wip")

	wts := repoWorktrees(repo)
	if len(wts) != 2 {
		t.Fatalf("got %d worktrees: %+v", len(wts), wts)
	}
	by := map[string]worktree{}
	for _, w := range wts {
		by[w.Branch] = w
	}
	if !by["done"].Merged || by["wip"].Merged {
		t.Errorf("merged detection wrong: %+v", by)
	}
}
