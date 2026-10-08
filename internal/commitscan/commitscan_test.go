package commitscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
)

func fakeKey() string { return "API" + "_KEY=" + strings.Repeat("q7", 10) }

func TestPlanFor(t *testing.T) {
	cases := []struct {
		cmd  string
		want Plan
	}{
		{"git status", Plan{}},
		{`git commit -m "fix a bug"`, Plan{Commits: true}},
		{`git commit -am "x"`, Plan{Commits: true, WorkingDir: true}},
		{`git commit --all -m x`, Plan{Commits: true, WorkingDir: true}},
		{`git add -A && git commit -m x`, Plan{Commits: true, WorkingDir: true, Untracked: true}},
		{`git -C repo add . ; git -C repo commit -m x`, Plan{Commits: true, WorkingDir: true, Untracked: true}},
		{`git commit --amend --no-edit`, Plan{Commits: true}},
	}
	for _, c := range cases {
		if got := PlanFor(c.cmd); got != c.want {
			t.Errorf("PlanFor(%q) = %+v, want %+v", c.cmd, got, c.want)
		}
	}
}

func TestScanDiffLineNumbers(t *testing.T) {
	diff := "diff --git a/x.py b/x.py\n--- a/x.py\n+++ b/x.py\n@@ -1,0 +10,2 @@\n+ok = 1\n+" + fakeKey() + "\n"
	hits := scanDiff(diff)
	if len(hits) != 1 || hits[0].File != "x.py" || hits[0].Line != 11 {
		t.Fatalf("hits = %+v", hits)
	}
	if strings.Contains(hits[0].String(), "q7q7") {
		t.Fatal("hit leaks the secret value")
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.invalid"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := gitcfg.RunGitIn(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write(t, dir, "README.md", "hello\n")
	gitcfg.RunGitIn(dir, "add", "README.md")
	if out, err := gitcfg.RunGitIn(dir, "commit", "-qm", "init"); err != nil {
		t.Fatalf("commit: %v %s", err, out)
	}
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanRealRepo(t *testing.T) {
	dir := gitRepo(t)

	// Staged secret is caught by a plain commit.
	write(t, dir, "app/config.py", "a = 1\n"+fakeKey()+"\n")
	gitcfg.RunGitIn(dir, "add", "app/config.py")
	hits := Scan(dir, `git commit -m "cfg"`, gitcfg.RunGitIn)
	if len(hits) != 1 || hits[0].File != "app/config.py" || hits[0].Line != 2 {
		t.Fatalf("staged: %+v", hits)
	}
	gitcfg.RunGitIn(dir, "reset", "-q")

	// Untracked secret is only in scope when the command also adds.
	if hits := Scan(dir, `git commit -m x`, gitcfg.RunGitIn); len(hits) != 0 {
		t.Fatalf("plain commit should not see untracked files: %+v", hits)
	}
	hits = Scan(dir, `git add -A && git commit -m x`, gitcfg.RunGitIn)
	if len(hits) == 0 {
		t.Fatal("add -A && commit missed an untracked secret")
	}

	// A new .env is refused by name; .env.example is fine.
	os.Remove(filepath.Join(dir, "app", "config.py"))
	write(t, dir, ".env", "X=1\n")
	write(t, dir, ".env.example", "X=\n")
	hits = Scan(dir, `git add . && git commit -m env`, gitcfg.RunGitIn)
	if len(hits) != 1 || hits[0].File != ".env" || hits[0].Pattern != "secret_file" {
		t.Fatalf(".env: %+v", hits)
	}

	// Non-commit commands never scan.
	if hits := Scan(dir, "git push", gitcfg.RunGitIn); hits != nil {
		t.Fatalf("push scanned: %+v", hits)
	}
}
