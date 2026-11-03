package cmd

// Hook-level tests of the commit secret scan (tq/commit-secret). The secret
// is a fake API key made of a repeated pair; the reason must never echo it.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSecret is a line the commit scan reports as a secret.
var fakeSecret = "API" + "_KEY=" + strings.Repeat("q7", 10) + "\n"

func hook(t *testing.T, ws, cmd string) (int, string) {
	t.Helper()
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, ws, cmd))
	return code, out + errOut
}

func wantDeny(t *testing.T, ws, cmd, rule string) {
	t.Helper()
	if code, out := hook(t, ws, cmd); code != 2 || !strings.Contains(out, rule) || strings.Contains(out, "q7q7") {
		t.Errorf("%s: want deny %s, got code=%d %q", cmd, rule, code, out)
	}
}

func wantAsk(t *testing.T, ws, cmd, rule, sub string) {
	t.Helper()
	code, out := hook(t, ws, cmd)
	if code == 2 || !strings.Contains(out, `"ask"`) || !strings.Contains(out, rule) || !strings.Contains(out, sub) {
		t.Errorf("%s: want ask %s %q, got code=%d %q", cmd, rule, sub, code, out)
	}
}

// gitRepo initialises a repository in ws with one commit and returns a
// runner for git commands in it.
func gitRepo(t *testing.T, ws string) func(args ...string) string {
	t.Helper()
	run := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = ws
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return run
}

// stage writes name under ws and stages it.
func stage(t *testing.T, ws string, run func(...string) string, name, body string) {
	t.Helper()
	p := filepath.Join(ws, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", name)
}

// noHooks is a git -c option that keeps repository hooks out of a test commit.
func noHooks(t *testing.T) string { return "core.hooksPath=" + testDir(t) }

func testDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// C1: the commit is scanned in the repository the command targets, not in
// the hook's cwd.
func TestFinal_C1_ScansTargetRepository(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	gitRepo(t, ws) // the hook cwd: a clean repository
	wt := filepath.Join(ws, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	runWt := gitRepo(t, wt)
	stage(t, wt, runWt, "cfg.py", fakeSecret)
	for _, c := range []string{
		`git -C wt commit -m "feat: cfg"`,
		`cd wt && git commit -m "feat: cfg"`,
		`cd "` + wt + `" && git commit -m "feat: cfg"`,
		`cd ` + filepath.ToSlash(wt) + `; git commit -m "feat: cfg"`,
	} {
		wantDeny(t, ws, c, "tq/commit-secret")
	}
	// A repository the hook cannot open is not vouched for: the one ask left.
	wantAsk(t, ws, `git -C missing commit -m "feat: cfg"`, "tq/commit-secret", "")
	// The clean cwd commits.
	if code, out := hook(t, ws, `git commit --allow-empty -m "feat: nothing"`); code != 0 || out != "" {
		t.Fatalf("clean commit: code=%d %q", code, out)
	}
}

// C2: `git commit <pathspec>` scans the named paths' working tree.
func TestFinal_C2_CommitPathspec(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	run := gitRepo(t, ws)
	stage(t, ws, run, "cfg.py", "x = 1\n")
	run("-c", noHooks(t), "commit", "-q", "-m", "chore: track")
	if err := os.WriteFile(filepath.Join(ws, "cfg.py"), []byte("x = 1\n"+fakeSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	wantDeny(t, ws, `git commit -m "feat: x" cfg.py`, "tq/commit-secret")
	wantDeny(t, ws, `git commit -m "feat: x" -- cfg.py`, "tq/commit-secret")
	wantDeny(t, ws, `git commit -m "feat(api): x" --include cfg.py`, "tq/commit-secret")
}

// C3: every command-word form reaches the scan.
func TestFinal_C3_CommandWordForms(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	run := gitRepo(t, ws)
	stage(t, ws, run, "cfg.py", fakeSecret)
	for _, c := range []string{
		`GIT_AUTHOR_DATE=now git commit -m "feat: cfg"`,
		`"git" commit -m "feat: cfg"`,
		`git.exe commit -m "feat: cfg"`,
		`/usr/bin/git commit -m "feat: cfg"`,
		`command git commit -m "feat: cfg"`,
		`env A=1 git commit -m "feat: cfg"`,
	} {
		wantDeny(t, ws, c, "tq/commit-secret")
	}
}

// C4: a file too large to scan is never skipped silently.
func TestFinal_C4_LargeFileAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	gitRepo(t, ws)
	line := strings.Repeat("x", 99) + "\n"
	if err := os.WriteFile(filepath.Join(ws, "export.csv"), []byte(strings.Repeat(line, (1<<20)/len(line)+100)), 0o600); err != nil {
		t.Fatal(err)
	}
	wantAsk(t, ws, `git add -A && git commit -m "feat: export"`, "tq/commit-secret", "scan limit")
}

// I1: one budget for the whole decision, under the plugin's 15 s timeout.
func TestFinal_I1_SharedBudget(t *testing.T) {
	if hookScanBudget() > 10*time.Second || hookScanBudget() <= 0 {
		t.Fatalf("default budget %v must stay well under the 15 s hook timeout", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "3s")
	if hookScanBudget() != 3*time.Second {
		t.Fatalf("env override: %v", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "60s")
	if hookScanBudget() > 12*time.Second {
		t.Fatalf("an override must be clamped under the hook timeout: %v", hookScanBudget())
	}
	t.Setenv("TQ_HOOK_SCAN_BUDGET", "")

	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	gitRepo(t, ws)
	origGit, origBudget := pushGit, scanBudget
	defer func() { pushGit, scanBudget = origGit, origBudget }()
	scanBudget = 300 * time.Millisecond
	pushGit = func(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
		<-ctx.Done()
		return "", false, ctx.Err()
	}
	start := time.Now()
	code, out := hook(t, ws, `git commit -m "feat: x"`)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("commit took %v with a 300ms budget: the budget is not honoured", d)
	}
	if code == 2 || !strings.Contains(out, "scan did not finish in time") || !strings.Contains(out, "tq/commit-secret") {
		t.Fatalf("the commit rule must ask on the deadline: code=%d %q", code, out)
	}
}

func TestFinal_I1_OversizedDiffAsks(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	run := gitRepo(t, ws)
	line := strings.Repeat("x", 99) + "\n"
	stage(t, ws, run, "big.txt", strings.Repeat(line, (9<<20)/len(line)))
	wantAsk(t, ws, `git commit -m "feat: big"`, "tq/commit-secret", "scan limit")
}
