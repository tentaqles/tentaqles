package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/testutil"
)

// tqHome points TQ_HOME (and HOME/USERPROFILE, so nothing can escape to the
// real home) at a fresh temp dir and returns it.
func tqHome(t *testing.T) string {
	t.Helper()
	h := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(h, ".tentaqles"))
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	return h
}

// hashTree returns a stable hash of a directory tree: relative paths, whether
// each entry is a link (plus its target), and file contents.
func hashTree(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isLink, tgt := IsLink(p); isLink {
			lines = append(lines, "L "+rel+" -> "+filepath.ToSlash(tgt))
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			lines = append(lines, "D "+rel)
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		lines = append(lines, "F "+rel+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("hashTree(%s): %v", root, err)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func TestOpenCreatesLayout(t *testing.T) {
	tqHome(t)
	j, err := Open("20260828-101010")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "files")); err != nil {
		t.Fatalf("files dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "journal.json")); err != nil {
		t.Fatalf("journal.json: %v", err)
	}
}

func TestRecordPersistsImmediately(t *testing.T) {
	tqHome(t)
	j, err := Open("ts1")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("step-a", OpWriteFile, map[string]string{"Path": "p", "Backup": ""}); err != nil {
		t.Fatal(err)
	}
	// A separate reader sees the entry without j being closed.
	j2, err := Load("ts1")
	if err != nil {
		t.Fatal(err)
	}
	if len(j2.Entries) != 1 || j2.Entries[0].Seq != 1 || j2.Entries[0].Step != "step-a" {
		t.Fatalf("reloaded entries = %+v", j2.Entries)
	}
}

func TestLoadLatest(t *testing.T) {
	tqHome(t)
	for _, ts := range []string{"20260101-000000", "20260828-120000", "20250101-000000"} {
		if _, err := Open(ts); err != nil {
			t.Fatal(err)
		}
	}
	j, err := Load("latest")
	if err != nil {
		t.Fatal(err)
	}
	if j.TS != "20260828-120000" {
		t.Fatalf("latest = %q, want 20260828-120000", j.TS)
	}
}

func TestLoadTruncatedJournal(t *testing.T) {
	tqHome(t)
	j, err := Open("bad")
	if err != nil {
		t.Fatal(err)
	}
	jp := filepath.Join(j.Dir, "journal.json")
	if err := os.WriteFile(jp, []byte(`{"ts":"bad","entries":[{"seq":1,`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load("bad")
	if err == nil {
		t.Fatal("want error loading a truncated journal")
	}
	if !strings.Contains(err.Error(), "journal.json") {
		t.Fatalf("error should name the file, got: %v", err)
	}
}

func TestBackupFile(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	p := filepath.Join(base, "cfg")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := Open("bk")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := j.BackupFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if rel == "" || !strings.HasPrefix(rel, "files/") {
		t.Fatalf("rel = %q", rel)
	}
	b, err := os.ReadFile(filepath.Join(j.Dir, filepath.FromSlash(rel)))
	if err != nil || string(b) != "original" {
		t.Fatalf("backup content = %q, %v", b, err)
	}
	// Missing file backs up to "".
	rel2, err := j.BackupFile(filepath.Join(base, "nope"))
	if err != nil || rel2 != "" {
		t.Fatalf("BackupFile(missing) = %q, %v", rel2, err)
	}
	// Second backup of the same path gets a distinct slot.
	rel3, err := j.BackupFile(p)
	if err != nil || rel3 == rel {
		t.Fatalf("BackupFile reused slot: %q vs %q (%v)", rel3, rel, err)
	}
}

// TestRoundTripAllOps records every op kind against a real tree, performs the
// mutations, then restores and asserts the tree is byte-identical.
func TestRoundTripAllOps(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	work := filepath.Join(base, "work")
	mustMkdir(t, filepath.Join(work, "identity"))
	mustWrite(t, filepath.Join(work, "identity", "note.txt"), "id")
	mustWrite(t, filepath.Join(work, "profile.ps1"), "before\n")
	mustWrite(t, filepath.Join(work, "doomed.txt"), "doomed\n")
	linkTgt := filepath.Join(work, "linktgt")
	mustMkdir(t, linkTgt)
	existingLink := filepath.Join(work, "existing-link")
	if err := MakeLink(existingLink, linkTgt); err != nil {
		t.Fatal(err)
	}

	before := hashTree(t, work)

	j, err := Open("rt")
	if err != nil {
		t.Fatal(err)
	}

	// 1. move-dir
	moved := filepath.Join(work, "identity-moved")
	if err := j.Record("move", OpMoveDir, map[string]string{"From": filepath.Join(work, "identity"), "To": moved}); err != nil {
		t.Fatal(err)
	}
	if err := MoveDir(filepath.Join(work, "identity"), moved); err != nil {
		t.Fatal(err)
	}

	// 2. make-link
	newLink := filepath.Join(work, "identity")
	if err := j.Record("link", OpMakeLink, map[string]string{"Path": newLink, "Target": moved}); err != nil {
		t.Fatal(err)
	}
	if err := MakeLink(newLink, moved); err != nil {
		t.Fatal(err)
	}

	// 3. remove-link
	if err := j.Record("unlink", OpRemoveLink, map[string]string{"Path": existingLink, "Target": linkTgt}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(existingLink); err != nil {
		t.Fatal(err)
	}

	// 4. write-file (with backup)
	prof := filepath.Join(work, "profile.ps1")
	rel, err := j.BackupFile(prof)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("profile", OpWriteFile, map[string]string{"Path": prof, "Backup": rel}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, prof, "after\n")

	// 5. write-file creating a new file (no backup) -> reverse deletes it
	fresh := filepath.Join(work, "fresh.txt")
	if err := j.Record("fresh", OpWriteFile, map[string]string{"Path": fresh, "Backup": ""}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, fresh, "new\n")

	// 6. delete-file
	doomed := filepath.Join(work, "doomed.txt")
	drel, err := j.BackupFile(doomed)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("delete", OpDeleteFile, map[string]string{"Path": doomed, "Backup": drel}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(doomed); err != nil {
		t.Fatal(err)
	}

	// 7. git-global-set (present) and (absent)
	if err := j.Record("git", OpGitGlobalSet, map[string]string{"Key": "user.email", "Old": "old@x", "New": "new@x", "Present": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Record("git2", OpGitGlobalSet, map[string]string{"Key": "core.hooksPath", "Old": "", "New": "/h", "Present": "false"}); err != nil {
		t.Fatal(err)
	}

	var gitCalls [][]string
	var regCalls [][]string
	r := Runner{
		Git: func(args ...string) (string, error) { gitCalls = append(gitCalls, args); return "", nil },
		Reg: func(action, key, name, value string) (string, error) {
			regCalls = append(regCalls, []string{action, key, name, value})
			return "", nil
		},
	}

	// 8. reg-set -- windows only
	if runtime.GOOS == "windows" {
		if err := j.Record("reg", OpRegSet, map[string]string{"Key": `HKCU\Environment`, "Name": "TQ_X", "Old": "1", "Present": "true"}); err != nil {
			t.Fatal(err)
		}
	}

	if h := hashTree(t, work); h == before {
		t.Fatal("mutations did not change the tree; test is not exercising anything")
	}

	lines, err := j.Restore(r)
	if err != nil {
		t.Fatalf("Restore: %v (lines: %v)", err, lines)
	}
	if len(lines) != len(j.Entries) {
		t.Fatalf("got %d lines for %d entries", len(lines), len(j.Entries))
	}
	if after := hashTree(t, work); after != before {
		t.Fatalf("tree not restored:\nbefore=%s\nafter =%s", before, after)
	}
	if len(gitCalls) != 2 {
		t.Fatalf("git calls = %v", gitCalls)
	}
	// Reverse order: git2 (unset) is undone first, then git (set old).
	if strings.Join(gitCalls[0], " ") != "config --global --unset core.hooksPath" {
		t.Fatalf("git call 0 = %v", gitCalls[0])
	}
	if strings.Join(gitCalls[1], " ") != "config --global user.email old@x" {
		t.Fatalf("git call 1 = %v", gitCalls[1])
	}
	if runtime.GOOS == "windows" {
		if len(regCalls) != 1 || regCalls[0][0] != "set" || regCalls[0][3] != "1" {
			t.Fatalf("reg calls = %v", regCalls)
		}
	}
	// restore.log written.
	if _, err := os.Stat(filepath.Join(j.Dir, "restore.log")); err != nil {
		t.Fatalf("restore.log: %v", err)
	}
}

func TestRestoreFailsWhenStateAlteredByHand(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	work := filepath.Join(base, "work")
	mustMkdir(t, filepath.Join(work, "id"))
	mustWrite(t, filepath.Join(work, "keep.txt"), "keep\n")

	j, err := Open("alt")
	if err != nil {
		t.Fatal(err)
	}
	// Entry 1 (undone last): a write-file we could reverse.
	keep := filepath.Join(work, "keep.txt")
	rel, err := j.BackupFile(keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("keep", OpWriteFile, map[string]string{"Path": keep, "Backup": rel}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, keep, "changed\n")

	// Entry 2 (undone first): a link that the user removed by hand.
	moved := filepath.Join(work, "id-moved")
	if err := MoveDir(filepath.Join(work, "id"), moved); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(work, "id")
	if err := j.Record("link", OpMakeLink, map[string]string{"Path": link, "Target": moved}); err != nil {
		t.Fatal(err)
	}
	if err := MakeLink(link, moved); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(link); err != nil { // user removed it
		t.Fatal(err)
	}

	lines, err := j.Restore(Runner{})
	if err == nil {
		t.Fatal("want error restoring a hand-altered state")
	}
	msg := err.Error()
	for _, want := range []string{"seq 2", string(OpMakeLink), link} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %v, want none done before the failure", lines)
	}
	// The rest of the journal was left untouched: keep.txt is still "changed".
	b, _ := os.ReadFile(keep)
	if string(b) != "changed\n" {
		t.Fatalf("earlier entry was reversed despite the stop: %q", b)
	}
	// Journal file itself is untouched.
	j2, err := Load("alt")
	if err != nil {
		t.Fatal(err)
	}
	if len(j2.Entries) != 2 {
		t.Fatalf("journal mutated: %d entries", len(j2.Entries))
	}
}

func TestRestoreMoveDirPreconditions(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	from := filepath.Join(base, "a")
	to := filepath.Join(base, "b")
	mustMkdir(t, from)

	j, err := Open("mv")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("mv", OpMoveDir, map[string]string{"From": from, "To": to}); err != nil {
		t.Fatal(err)
	}
	// Mutation never happened: To missing -> precondition failure.
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want error when To does not exist")
	}
	// Now do the move, but recreate From by hand -> still a failure.
	if err := MoveDir(from, to); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, from)
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want error when From already exists")
	}
}

func TestRestoreRegSetNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	tqHome(t)
	j, err := Open("reg")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("reg", OpRegSet, map[string]string{"Key": `HKCU\Environment`, "Name": "X", "Old": "", "Present": "false"}); err != nil {
		t.Fatal(err)
	}
	_, err = j.Restore(Runner{Reg: func(a, k, n, v string) (string, error) { return "", nil }})
	if err == nil || !strings.Contains(err.Error(), "not supported on this OS") {
		t.Fatalf("err = %v", err)
	}
}

func TestRestoreMissingRunnerFunc(t *testing.T) {
	tqHome(t)
	j, err := Open("norunner")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Record("git", OpGitGlobalSet, map[string]string{"Key": "user.name", "Old": "o", "Present": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want error when Runner.Git is nil")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
