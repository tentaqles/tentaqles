package commitscan

// Test values: 529.982.247-25 is the public CPF test number used across this tq:allow-pii
// repository's tests (valid check digits, no real person). Secrets are built
// at run time by fakeKey.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
)

const testCPFRow = "Ana;529.982.247-25;rua x" // tq:allow-pii

func addedFiles(lines []AddedLine) map[string]bool {
	out := map[string]bool{}
	for _, l := range lines {
		out[l.File] = true
	}
	return out
}

func hasText(lines []AddedLine, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l.Text, sub) {
			return true
		}
	}
	return false
}

// C2 / R2: `git commit <pathspec>` commits the working tree of those paths.
func TestPlanFor_CommitPathspecs(t *testing.T) {
	cases := []struct {
		cmd  string
		want Plan
	}{
		{`git commit -m "feat: x" export.csv`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"export.csv"}}},
		{`git commit -m "feat(api): x" -- export.csv`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"export.csv"}}},
		{`git commit --include -m x a.csv b.csv`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"a.csv", "b.csv"}}},
		{`git commit -o a.csv -m x`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"a.csv"}}},
		{`git commit -mfoo a.csv`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"a.csv"}}},
		{`git commit -qm x a.csv`, Plan{Commits: true, WorkingDir: true, CommitPaths: []string{"a.csv"}}},
		{`git commit --author "A <a@b.c>" --date now -m x`, Plan{Commits: true}},
		{`git commit -F msg.txt --cleanup strip`, Plan{Commits: true}},
		{`git commit --pathspec-from-file=list.txt -m x`, Plan{Commits: true, Ask: askPathspecFile}},
	}
	for _, c := range cases {
		if got := PlanFor(c.cmd); !reflect.DeepEqual(got, c.want) {
			t.Errorf("PlanFor(%q) = %+v, want %+v", c.cmd, got, c.want)
		}
	}
}

func TestAdded_CommitPathspecScansWorkingTree(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "export.csv", "nome;cpf\n")
	gitcfg.RunGitIn(dir, "add", "export.csv")
	gitcfg.RunGitIn(dir, "commit", "-qm", "track")
	write(t, dir, "export.csv", "nome;cpf\n"+testCPFRow+"\n")
	for _, c := range []string{
		`git commit -m "feat: x" export.csv`,
		`git commit -m "feat: x" -- export.csv`,
		`git commit -i -m "feat: x" export.csv`,
	} {
		added, err := Added(dir, c, gitcfg.RunGitIn)
		if err != nil || !hasText(added, "529.982.247-25") { // tq:allow-pii
			t.Errorf("%s: %+v %v", c, added, err)
		}
	}
	if added, _ := Added(dir, `git commit -m "feat: x"`, gitcfg.RunGitIn); hasText(added, "529") {
		t.Fatal("a plain commit must not see unstaged changes")
	}
	if _, err := Added(dir, `git commit --pathspec-from-file=l.txt -m x`, gitcfg.RunGitIn); !IsAsk(err) {
		t.Fatalf("pathspec-from-file: err=%v, want an ask", err)
	}
}

// C4 / R4: a large untracked text file asks, naming the file and the cap.
func TestAdded_LargeUntrackedFileAsks(t *testing.T) {
	dir := gitRepo(t)
	big := strings.Repeat(testCPFRow+"\n", (maxFileBytes/len(testCPFRow))+10)
	write(t, dir, "export.csv", big)
	_, err := Added(dir, `git add -A && git commit -m "feat: export"`, gitcfg.RunGitIn)
	if !IsAsk(err) || !strings.Contains(err.Error(), "export.csv") || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("err=%v, want an ask naming export.csv and the cap", err)
	}
	if _, err := ScanCtx(context.Background(), dir, `git add -A && git commit -m "feat: export"`, gitcfg.RunGitIn); !IsAsk(err) {
		t.Fatalf("secret scan: err=%v, want an ask", err)
	}
	// a large binary still passes
	os.Remove(filepath.Join(dir, "export.csv"))
	write(t, dir, "img.bin", "\x00\x01"+strings.Repeat("x", maxFileBytes+10))
	if _, err := Added(dir, `git add -A && git commit -m "feat: img"`, gitcfg.RunGitIn); err != nil {
		t.Fatalf("large binary: %v", err)
	}
}

func utf16le(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, r := range utf16.Encode([]rune(s)) {
		b = append(b, byte(r), byte(r>>8))
	}
	return string(b)
}

func utf16be(s string) string {
	b := []byte{0xFE, 0xFF}
	for _, r := range utf16.Encode([]rune(s)) {
		b = append(b, byte(r>>8), byte(r))
	}
	return string(b)
}

// C4 / R4: UTF-16 with a BOM (Windows PowerShell 5.1 `>`) is decoded.
func TestAdded_UTF16Decoded(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "le.csv", utf16le("nome;cpf\r\n"+testCPFRow+"\r\n"))
	gitcfg.RunGitIn(dir, "add", "le.csv")
	added, err := Added(dir, `git commit -m "feat: export"`, gitcfg.RunGitIn)
	if err != nil || !hasText(added, "529.982.247-25") || !addedFiles(added)["le.csv"] { // tq:allow-pii
		t.Fatalf("staged UTF-16LE: %+v %v", added, err)
	}
	gitcfg.RunGitIn(dir, "reset", "-q")
	write(t, dir, "be.csv", utf16be(testCPFRow+"\n"))
	added, err = Added(dir, `git add -A && git commit -m "feat: export"`, gitcfg.RunGitIn)
	if err != nil || !addedFiles(added)["be.csv"] || !addedFiles(added)["le.csv"] || !hasText(added, "529.982.247-25") { // tq:allow-pii
		t.Fatalf("untracked UTF-16: %+v %v", added, err)
	}
	// tracked UTF-16 changed in the working tree, committed with -a
	gitcfg.RunGitIn(dir, "add", "-A")
	gitcfg.RunGitIn(dir, "commit", "-qm", "track")
	write(t, dir, "le.csv", utf16le("nome;cpf\r\nBia;529.982.247-25\r\n")) // tq:allow-pii
	added, err = Added(dir, `git commit -am "feat: export"`, gitcfg.RunGitIn)
	if err != nil || !hasText(added, "Bia;529") { // tq:allow-pii
		t.Fatalf("worktree UTF-16 with -a: %+v %v", added, err)
	}
}

// I3 / R7: untracked symlinks and other non-regular files are never opened.
func TestAdded_UntrackedSymlinkSkipped(t *testing.T) {
	dir := gitRepo(t)
	target := filepath.Join(t.TempDir(), "outside.csv")
	if err := os.WriteFile(target, []byte(testCPFRow+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "z")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("no symlink privilege")
		}
		t.Fatal(err)
	}
	added, err := Added(dir, `git add -A && git commit -m x`, gitcfg.RunGitIn)
	if err != nil || hasText(added, "529") {
		t.Fatalf("symlink target was read: %+v %v", added, err)
	}
}

func TestReadBounded_NonRegularAndBounded(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := readBounded(dir, "d"); ok || err != nil {
		t.Fatalf("a directory must be skipped silently, got ok=%v err=%v", ok, err)
	}
	write(t, dir, "a.txt", "hello\n")
	text, ok, err := readBounded(filepath.Join(dir, "a.txt"), "a.txt")
	if err != nil || !ok || text != "hello\n" {
		t.Fatalf("%q %v %v", text, ok, err)
	}
}

// R1: a command that commits in another directory is scanned there.
func TestAdded_ScansTargetDirectory(t *testing.T) {
	cwd := gitRepo(t) // clean
	wt := gitRepo(t)
	write(t, wt, "seed.sql", testCPFRow+"\n")
	gitcfg.RunGitIn(wt, "add", "seed.sql")
	for _, c := range []string{
		`git -C "` + wt + `" commit -m "feat: seed"`,
		`cd "` + wt + `" && git commit -m "feat: seed"`,
		`cd "` + wt + `"; git commit -m "feat: seed"`,
	} {
		added, err := Added(cwd, c, gitcfg.RunGitIn)
		if err != nil || !hasText(added, "529") {
			t.Errorf("%s: %+v %v", c, added, err)
		}
	}
	if _, err := Added(cwd, `git -C nowhere commit -m x`, gitcfg.RunGitIn); !IsAsk(err) {
		t.Fatalf("missing dir: err=%v, want an ask", err)
	}
}

// R5: the deadline surfaces as an error from the secret scan too.
func TestScanCtx_DeadlineIsAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := func(dir string, args ...string) (string, error) { return "", ctx.Err() }
	if _, err := ScanCtx(ctx, t.TempDir(), `git commit -m x`, slow); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
