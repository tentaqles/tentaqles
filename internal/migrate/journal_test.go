package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/testutil"
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

// rawLinkTarget reads a link's target without going through IsLink, the
// function under test. os.Readlink handles POSIX symlinks and, on current Go,
// Windows junctions; it is a completely different mechanism from the reparse
// parsing in fsops_windows.go, so agreement between the two is real evidence.
func rawLinkTarget(path string) (string, bool) {
	tgt, err := os.Readlink(path)
	if err != nil || tgt == "" {
		return "", false
	}
	tgt = strings.TrimPrefix(tgt, `\??\`)
	if len(tgt) > 3 {
		tgt = strings.TrimSuffix(tgt, string(os.PathSeparator))
	}
	return tgt, true
}

// assertLinkTarget checks where a link points using os.Readlink rather than
// IsLink, so a bug in link-target detection cannot make the assertion pass.
func assertLinkTarget(t *testing.T, path, want string) {
	t.Helper()
	got, ok := rawLinkTarget(path)
	if !ok {
		t.Fatalf("os.Readlink(%s): no target (is it a link at all?)", path)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("os.Readlink(%s) = %q, want %q", path, got, want)
	}
}

// hashTree returns a stable hash of a directory tree: relative paths, whether
// each entry is a link (plus its target), and file contents.
//
// Link detection here goes through os.Readlink, not IsLink: hashing a tree with
// the same function the tests are trying to validate would let a wrong link
// target compare equal to itself and pass.
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
		if tgt, ok := rawLinkTarget(p); ok {
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

// ---------------------------------------------------------------- fake runners

// fakeGit is an in-memory `git config --global`. It answers --get, --unset and
// plain sets, so a test can assert the config ends up exactly as it started.
type fakeGit struct {
	cfg   map[string]string
	calls [][]string
	// unsetErr, when set, is returned by --unset instead of doing the work.
	unsetErr error
}

func newFakeGit(kv map[string]string) *fakeGit {
	cfg := map[string]string{}
	for k, v := range kv {
		cfg[k] = v
	}
	return &fakeGit{cfg: cfg}
}

func (f *fakeGit) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
		switch args[2] {
		case "--get":
			v, ok := f.cfg[args[3]]
			if !ok {
				return "", fmt.Errorf("git config --get %s: not set", args[3])
			}
			return v, nil
		case "--unset":
			if f.unsetErr != nil {
				return "", f.unsetErr
			}
			if _, ok := f.cfg[args[3]]; !ok {
				return "", fmt.Errorf("git config --unset %s: not set", args[3])
			}
			delete(f.cfg, args[3])
			return "", nil
		default:
			f.cfg[args[2]] = args[3]
			return "", nil
		}
	}
	return "", fmt.Errorf("fakeGit: unexpected args %v", args)
}

// fakeReg is an in-memory registry hive that renders `reg query` output the way
// reg.exe does, so type handling is exercised end to end.
type fakeReg struct {
	vals  map[string]RegValue // "KEY\NAME" -> value
	calls [][]string
}

func newFakeReg() *fakeReg { return &fakeReg{vals: map[string]RegValue{}} }

func (f *fakeReg) set(key, name string, v RegValue) { f.vals[key+`\`+name] = v }
func (f *fakeReg) get(key, name string) (RegValue, bool) {
	v, ok := f.vals[key+`\`+name]
	return v, ok
}

func (f *fakeReg) run(action, key, name string, v RegValue) (string, error) {
	f.calls = append(f.calls, []string{action, key, name, v.Type, v.Data})
	k := key + `\` + name
	switch action {
	case "query":
		cur, ok := f.vals[k]
		if !ok {
			return "ERROR: The system was unable to find the specified registry key or value.",
				fmt.Errorf("reg query %s: not found", k)
		}
		return fmt.Sprintf("\r\n%s\r\n    %s    %s    %s\r\n\r\n", key, name, cur.Type, cur.Data), nil
	case "set":
		if !SupportedRegType(v.Type) {
			return "", fmt.Errorf("reg add %s: unsupported type %q", k, v.Type)
		}
		f.vals[k] = v
		return "The operation completed successfully.", nil
	case "delete":
		if _, ok := f.vals[k]; !ok {
			return "", fmt.Errorf("reg delete %s: not found", k)
		}
		delete(f.vals, k)
		return "The operation completed successfully.", nil
	}
	return "", fmt.Errorf("fakeReg: unknown action %q", action)
}

// exitErr runs a trivial command that exits with code, so a test can hand the
// production code a real *exec.ExitError.
func exitErr(t *testing.T, code int) error {
	t.Helper()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", "exit", strconv.Itoa(code))
	} else {
		c = exec.Command("sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := c.Run()
	if err == nil {
		t.Fatalf("expected a non-zero exit")
	}
	return err
}

// ------------------------------------------------------------------- layout

func TestOpenCreatesLayout(t *testing.T) {
	tqHome(t)
	j, err := Open("20260828-101010")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(j.Dir, "files")); err != nil {
		t.Fatalf("files dir: %v", err)
	}
	fi, err := os.Stat(filepath.Join(j.Dir, "journal.json"))
	if err != nil {
		t.Fatalf("journal.json: %v", err)
	}
	// The journal holds old git identities and registry values.
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("journal.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestRecordPersistsImmediately(t *testing.T) {
	tqHome(t)
	j, err := Open("ts1")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("step-a", "p", Backup{}); err != nil {
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

// TestRecordNeverLeavesJournalAbsent watches journal.json while records are
// written. The old code removed the destination before renaming the temp file
// over it on Windows, so there was a window on every Record in which the
// journal did not exist at all -- a crash or a scanner holding the file there
// loses it outright. os.Rename replaces an existing destination on every OS tq
// supports, so the file must be observable at every instant.
func TestRecordNeverLeavesJournalAbsent(t *testing.T) {
	tqHome(t)
	j, err := Open("nowindow")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(j.Dir, "journal.json")

	var missing atomic.Int64
	var samples atomic.Int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
				missing.Add(1)
			}
			samples.Add(1)
			// Yield: a hard stat loop keeps a handle on the file often enough
			// to provoke sharing violations of its own, which is not what this
			// test is about.
			time.Sleep(100 * time.Microsecond)
		}
	}()

	for i := 0; i < 400; i++ {
		if err := j.RecordWriteFile("step", fmt.Sprintf("p%d", i), Backup{}); err != nil {
			close(stop)
			<-done
			t.Fatal(err)
		}
	}
	close(stop)
	<-done

	if samples.Load() == 0 {
		t.Fatal("watcher never sampled the journal")
	}
	if n := missing.Load(); n != 0 {
		t.Fatalf("journal.json was absent in %d of %d samples; Record must never unlink it", n, samples.Load())
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

// TestLatestRefusesHalfWrittenJournal covers the second half of the
// remove-then-rename bug: a backup directory holding only journal.json.tq-tmp
// used to be skipped silently, so "latest" resolved to an older, unrelated
// migration whose restore would put the machine into a state that never
// existed.
func TestLatestRefusesHalfWrittenJournal(t *testing.T) {
	tqHome(t)
	if _, err := Open("20250101-000000"); err != nil {
		t.Fatal(err)
	}
	j, err := Open("20260828-120000")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash between the temp write and the rename.
	if err := os.Rename(filepath.Join(j.Dir, "journal.json"), filepath.Join(j.Dir, "journal.json.tq-tmp")); err != nil {
		t.Fatal(err)
	}

	_, err = Load("latest")
	if err == nil {
		t.Fatal("want an error, not a silent fall back to the older backup")
	}
	for _, want := range []string{"20260828-120000", "journal.json.tq-tmp"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should name %q", err, want)
		}
	}
}

// TestLoadRecoversFromTemp: the temp file is fsynced before the rename, so its
// contents are complete. An explicit Load of that timestamp should use it
// rather than report no journal.
func TestLoadRecoversFromTemp(t *testing.T) {
	tqHome(t)
	j, err := Open("recov")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("s", "p", Backup{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(j.Dir, "journal.json"), filepath.Join(j.Dir, "journal.json.tq-tmp")); err != nil {
		t.Fatal(err)
	}
	j2, err := Load("recov")
	if err != nil {
		t.Fatalf("Load should recover from the temp file: %v", err)
	}
	if len(j2.Entries) != 1 || j2.Entries[0].Step != "s" {
		t.Fatalf("recovered entries = %+v", j2.Entries)
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

// ------------------------------------------------------------------- backups

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
	b, err := j.BackupFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Rel == "" || !strings.HasPrefix(b.Rel, "files/") {
		t.Fatalf("rel = %q", b.Rel)
	}
	if b.Bytes != int64(len("original")) {
		t.Fatalf("Bytes = %d, want %d", b.Bytes, len("original"))
	}
	sum := sha256.Sum256([]byte("original"))
	if b.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("SHA256 = %q", b.SHA256)
	}
	raw, err := os.ReadFile(filepath.Join(j.Dir, filepath.FromSlash(b.Rel)))
	if err != nil || string(raw) != "original" {
		t.Fatalf("backup content = %q, %v", raw, err)
	}
	// Missing file backs up to the zero Backup.
	b2, err := j.BackupFile(filepath.Join(base, "nope"))
	if err != nil || b2.Rel != "" {
		t.Fatalf("BackupFile(missing) = %+v, %v", b2, err)
	}
	// Second backup of the same path gets a distinct slot.
	b3, err := j.BackupFile(p)
	if err != nil || b3.Rel == b.Rel {
		t.Fatalf("BackupFile reused slot: %q vs %q (%v)", b3.Rel, b.Rel, err)
	}
}

// TestBackupSlotsAreNeverReused: nextSlot used to return len(files/), so
// deleting one backup made the next one land on a slot an earlier entry still
// pointed at and overwrite it.
func TestBackupSlotsAreNeverReused(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	j, err := Open("slots")
	if err != nil {
		t.Fatal(err)
	}
	var slots []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(base, fmt.Sprintf("f%d", i))
		mustWrite(t, p, fmt.Sprintf("content-%d", i))
		b, err := j.BackupFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.RecordWriteFile("w", p, b); err != nil {
			t.Fatal(err)
		}
		slots = append(slots, b.Rel)
	}
	// Someone (or something) removes the first backup file.
	if err := os.Remove(filepath.Join(j.Dir, filepath.FromSlash(slots[0]))); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(base, "f3")
	mustWrite(t, p, "content-3")
	b, err := j.BackupFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, used := range slots {
		if b.Rel == used {
			t.Fatalf("new backup reused slot %q, which entry %v still points at", b.Rel, slots)
		}
	}
	// The surviving backups still hold their own content.
	for i := 1; i < 3; i++ {
		raw, err := os.ReadFile(filepath.Join(j.Dir, filepath.FromSlash(slots[i])))
		if err != nil || string(raw) != fmt.Sprintf("content-%d", i) {
			t.Fatalf("slot %s clobbered: %q %v", slots[i], raw, err)
		}
	}
}

// ------------------------------------------------------------------ validation

func TestRecordRejectsBadArgs(t *testing.T) {
	tqHome(t)
	j, err := Open("val")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		op   OpKind
		args map[string]string
		want string
	}{
		{"misspelled key", OpMoveDir, map[string]string{"From": "a", "to": "b"}, `unknown argument "to"`},
		{"missing key", OpMoveDir, map[string]string{"From": "a"}, `missing required argument "To"`},
		{"empty link target", OpRemoveLink, map[string]string{"Path": "p", "Target": ""}, `missing required argument "Target"`},
		{"backup without digest", OpWriteFile, map[string]string{"Path": "p", "Backup": "files/1"}, `"Bytes" is required alongside Backup`},
		{"digest without backup", OpWriteFile, map[string]string{"Path": "p", "SHA256": strings.Repeat("a", 64)}, "without a Backup"},
		{"bad present", OpGitGlobalSet, map[string]string{"Key": "k", "Present": "yes"}, `Present must be`},
		{"reg without type", OpRegSet, map[string]string{"Key": "K", "Name": "N", "Present": "true", "Old": "v"}, "Type is required"},
		{"reg unrestorable type", OpRegSet, map[string]string{"Key": "K", "Name": "N", "Present": "true", "Old": "a\\0b", "Type": "REG_MULTI_SZ"}, "cannot restore faithfully"},
		{"unknown op", OpKind("frobnicate"), map[string]string{}, "unknown op"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := j.Record("s", c.op, c.args)
			if err == nil {
				t.Fatalf("Record accepted %v", c.args)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q should contain %q", err, c.want)
			}
		})
	}
	if len(j.Entries) != 0 {
		t.Fatalf("rejected records still landed in the journal: %+v", j.Entries)
	}
}

// -------------------------------------------------------------- round trip

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
	assertLinkTarget(t, existingLink, linkTgt)

	before := hashTree(t, work)

	j, err := Open("rt")
	if err != nil {
		t.Fatal(err)
	}

	// 1. move-dir
	moved := filepath.Join(work, "identity-moved")
	if err := j.RecordMoveDir("move", filepath.Join(work, "identity"), moved); err != nil {
		t.Fatal(err)
	}
	if err := MoveDir(filepath.Join(work, "identity"), moved); err != nil {
		t.Fatal(err)
	}

	// 2. make-link
	newLink := filepath.Join(work, "identity")
	if err := j.RecordMakeLink("link", newLink, moved); err != nil {
		t.Fatal(err)
	}
	if err := MakeLink(newLink, moved); err != nil {
		t.Fatal(err)
	}
	assertLinkTarget(t, newLink, moved)

	// 3. remove-link
	if err := j.RecordRemoveLink("unlink", existingLink, linkTgt); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(existingLink); err != nil {
		t.Fatal(err)
	}

	// 4. write-file (with backup)
	prof := filepath.Join(work, "profile.ps1")
	pb, err := j.BackupFile(prof)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("profile", prof, pb); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, prof, "after\n")

	// 5. write-file creating a new file (no backup) -> reverse deletes it
	fresh := filepath.Join(work, "fresh.txt")
	if err := j.RecordWriteFile("fresh", fresh, Backup{}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, fresh, "new\n")

	// 6. delete-file
	doomed := filepath.Join(work, "doomed.txt")
	db, err := j.BackupFile(doomed)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordDeleteFile("delete", doomed, db); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(doomed); err != nil {
		t.Fatal(err)
	}

	// 7. git-global-set (present) and (absent)
	git := newFakeGit(map[string]string{"user.email": "old@x"})
	if err := j.RecordGitGlobalSet("git", "user.email", "old@x", "new@x", true); err != nil {
		t.Fatal(err)
	}
	git.cfg["user.email"] = "new@x"
	if err := j.RecordGitGlobalSet("git2", "core.hooksPath", "", "/h", false); err != nil {
		t.Fatal(err)
	}
	git.cfg["core.hooksPath"] = "/h"

	// 8. reg-set -- windows only. The old value is REG_EXPAND_SZ, the type most
	// of HKCU\Environment actually uses; restoring it as REG_SZ would freeze
	// %USERPROFILE% into a literal path.
	reg := newFakeReg()
	if runtime.GOOS == "windows" {
		reg.set(`HKCU\Environment`, "GOPATH", RegValue{Type: "REG_EXPAND_SZ", Data: `%USERPROFILE%\go`})
		if err := j.RecordRegSet("reg", `HKCU\Environment`, "GOPATH",
			RegValue{Type: "REG_EXPAND_SZ", Data: `%USERPROFILE%\go`}, true); err != nil {
			t.Fatal(err)
		}
		reg.set(`HKCU\Environment`, "GOPATH", RegValue{Type: "REG_SZ", Data: `C:\tq\go`})
	}

	r := Runner{Git: git.run, Reg: reg.run}

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
	assertLinkTarget(t, existingLink, linkTgt)

	if git.cfg["user.email"] != "old@x" {
		t.Fatalf("user.email = %q, want old@x", git.cfg["user.email"])
	}
	if v, ok := git.cfg["core.hooksPath"]; ok {
		t.Fatalf("core.hooksPath should be unset, got %q", v)
	}
	if runtime.GOOS == "windows" {
		got, ok := reg.get(`HKCU\Environment`, "GOPATH")
		if !ok {
			t.Fatal("GOPATH was not restored")
		}
		want := RegValue{Type: "REG_EXPAND_SZ", Data: `%USERPROFILE%\go`}
		if got != want {
			t.Fatalf("GOPATH restored as %+v, want %+v (the type must round-trip)", got, want)
		}
	}
	// restore.log written.
	if _, err := os.Stat(filepath.Join(j.Dir, "restore.log")); err != nil {
		t.Fatalf("restore.log: %v", err)
	}

	// Restoring a second time must not fail: every entry is now in its
	// pre-operation state, and the resume marker says so too.
	lines2, err := j.Restore(r)
	if err != nil {
		t.Fatalf("second Restore: %v (lines: %v)", err, lines2)
	}
	for _, l := range lines2 {
		if !strings.Contains(l, "already reversed by an earlier restore") {
			t.Fatalf("second Restore did work again: %q", l)
		}
	}
	if after := hashTree(t, work); after != before {
		t.Fatalf("the second Restore disturbed the tree:\nbefore=%s\nafter =%s", before, after)
	}
}

// TestRestoreToleratesRecordWithoutMutation covers the crash-between-Record-
// and-mutation case. Entries are written before their mutations, so the newest
// entry in a journal routinely describes something that never happened; a
// reverse that hard-failed on that state made the whole journal unreversible,
// because Restore starts at the newest entry.
func TestRestoreToleratesRecordWithoutMutation(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	work := filepath.Join(base, "work")
	mustMkdir(t, filepath.Join(work, "id"))
	tgt := filepath.Join(work, "tgt")
	mustMkdir(t, tgt)
	standing := filepath.Join(work, "standing-link")
	if err := MakeLink(standing, tgt); err != nil {
		t.Fatal(err)
	}
	before := hashTree(t, work)

	j, err := Open("norun")
	if err != nil {
		t.Fatal(err)
	}
	// Every one of these is recorded and then NOT performed.
	if err := j.RecordMoveDir("move", filepath.Join(work, "id"), filepath.Join(work, "id-moved")); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordMakeLink("link", filepath.Join(work, "newlink"), tgt); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordRemoveLink("unlink", standing, tgt); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("fresh", filepath.Join(work, "fresh.txt"), Backup{}); err != nil {
		t.Fatal(err)
	}
	git := newFakeGit(nil)
	if err := j.RecordGitGlobalSet("git", "core.hooksPath", "", "/h", false); err != nil {
		t.Fatal(err)
	}
	reg := newFakeReg()
	if runtime.GOOS == "windows" {
		if err := j.RecordRegSet("reg", `HKCU\Environment`, "TQ_NEW", RegValue{}, false); err != nil {
			t.Fatal(err)
		}
	}

	lines, err := j.Restore(Runner{Git: git.run, Reg: reg.run})
	if err != nil {
		t.Fatalf("Restore of an all-unperformed journal must succeed: %v (lines %v)", err, lines)
	}
	if len(lines) != len(j.Entries) {
		t.Fatalf("got %d lines for %d entries", len(lines), len(j.Entries))
	}
	for _, l := range lines {
		if !strings.Contains(l, "nothing to undo") {
			t.Fatalf("expected every entry to report nothing to undo, got %q", l)
		}
	}
	if after := hashTree(t, work); after != before {
		t.Fatalf("restoring unperformed entries changed the tree:\nbefore=%s\nafter =%s", before, after)
	}
}

// TestRestoreResumesAfterMiddleFailure exercises the partial-revert path: a
// journal whose middle entry cannot be reversed leaves the newer entries
// reverted and the older ones untouched, and a re-run after the obstruction is
// cleared must continue rather than start again at the newest entry.
func TestRestoreResumesAfterMiddleFailure(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	work := filepath.Join(base, "work")
	mustMkdir(t, work)
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		mustWrite(t, filepath.Join(work, n), "orig-"+n)
	}
	mustMkdir(t, filepath.Join(work, "dir"))

	j, err := Open("resume")
	if err != nil {
		t.Fatal(err)
	}
	backupAndWrite := func(step, name, content string) {
		t.Helper()
		p := filepath.Join(work, name)
		b, err := j.BackupFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := j.RecordWriteFile(step, p, b); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, p, content)
	}

	backupAndWrite("a", "a.txt", "new-a") // seq 1
	backupAndWrite("b", "b.txt", "new-b") // seq 2
	from := filepath.Join(work, "dir")    // seq 3
	to := filepath.Join(work, "dir-moved")
	if err := j.RecordMoveDir("move", from, to); err != nil {
		t.Fatal(err)
	}
	if err := MoveDir(from, to); err != nil {
		t.Fatal(err)
	}
	backupAndWrite("c", "c.txt", "new-c") // seq 4

	// The user recreates the original directory by hand: seq 3 now describes a
	// world tq does not recognise and must refuse to touch.
	mustMkdir(t, from)

	lines, err := j.Restore(Runner{})
	if err == nil {
		t.Fatal("want a failure at seq 3")
	}
	if !strings.Contains(err.Error(), "seq 3") {
		t.Fatalf("error should name seq 3: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("want exactly seq 4 reversed before the stop, got %v", lines)
	}
	// Partial revert: the newest entry was undone, the older ones were not.
	assertFile(t, filepath.Join(work, "c.txt"), "orig-c.txt")
	assertFile(t, filepath.Join(work, "a.txt"), "new-a")
	assertFile(t, filepath.Join(work, "b.txt"), "new-b")

	// The user removes the directory they recreated and re-runs.
	if err := os.RemoveAll(from); err != nil {
		t.Fatal(err)
	}
	lines2, err := j.Restore(Runner{})
	if err != nil {
		t.Fatalf("resume: %v (lines %v)", err, lines2)
	}
	if len(lines2) == 0 || !strings.Contains(lines2[0], "already reversed by an earlier restore") {
		t.Fatalf("the resumed run should skip seq 4, got %v", lines2)
	}
	assertFile(t, filepath.Join(work, "a.txt"), "orig-a.txt")
	assertFile(t, filepath.Join(work, "b.txt"), "orig-b.txt")
	assertFile(t, filepath.Join(work, "c.txt"), "orig-c.txt")
	if _, err := os.Stat(filepath.Join(work, "dir")); err != nil {
		t.Fatalf("dir not moved back: %v", err)
	}
}

// TestRestoreRefusesCorruptBackup: BackupFile fsyncs its copy before the entry
// pointing at it is recorded, but a backup can still be truncated by other
// means. Restore must verify the copy against the size and digest in the
// journal instead of writing it over the user's real file and reporting
// success.
func TestRestoreRefusesCorruptBackup(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	p := filepath.Join(base, "cfg")
	mustWrite(t, p, "the original content\n")

	j, err := Open("corrupt")
	if err != nil {
		t.Fatal(err)
	}
	b, err := j.BackupFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("cfg", p, b); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, p, "the live content the user depends on\n")

	slot := filepath.Join(j.Dir, filepath.FromSlash(b.Rel))

	t.Run("truncated to zero", func(t *testing.T) {
		if err := os.WriteFile(slot, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := j.Restore(Runner{})
		if err == nil {
			t.Fatal("want a refusal to restore an empty backup")
		}
		if !strings.Contains(err.Error(), "bytes") {
			t.Fatalf("error should say the size is wrong: %v", err)
		}
		assertFile(t, p, "the live content the user depends on\n")
	})

	t.Run("same size, wrong bytes", func(t *testing.T) {
		if err := j.ResetRestore(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(slot, []byte(strings.Repeat("x", int(b.Bytes))), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := j.Restore(Runner{})
		if err == nil {
			t.Fatal("want a refusal to restore a corrupt backup")
		}
		if !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("error should mention the checksum: %v", err)
		}
		assertFile(t, p, "the live content the user depends on\n")
	})

	t.Run("intact backup still restores", func(t *testing.T) {
		if err := j.ResetRestore(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(slot, []byte("the original content\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := j.Restore(Runner{}); err != nil {
			t.Fatal(err)
		}
		assertFile(t, p, "the original content\n")
	})
}

// TestRestoreRefusesForeignState: "already in the pre-operation state" is
// tolerated, but a world tq does not recognise still stops the replay loudly
// and leaves everything else alone.
func TestRestoreRefusesForeignState(t *testing.T) {
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
	b, err := j.BackupFile(keep)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordWriteFile("keep", keep, b); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, keep, "changed\n")

	// Entry 2 (undone first): tq linked work/id at work/id-moved, and the user
	// then replaced the link with a real directory of their own.
	moved := filepath.Join(work, "id-moved")
	if err := MoveDir(filepath.Join(work, "id"), moved); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(work, "id")
	if err := j.RecordMakeLink("link", link, moved); err != nil {
		t.Fatal(err)
	}
	if err := MakeLink(link, moved); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(link); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, link)
	mustWrite(t, filepath.Join(link, "mine.txt"), "the user's own data\n")

	lines, err := j.Restore(Runner{})
	if err == nil {
		t.Fatal("want error restoring over a real directory the user created")
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
	// The user's directory was not deleted.
	assertFile(t, filepath.Join(link, "mine.txt"), "the user's own data\n")
	// The rest of the journal was left untouched: keep.txt is still "changed".
	assertFile(t, keep, "changed\n")
	// Journal file itself is untouched.
	j2, err := Load("alt")
	if err != nil {
		t.Fatal(err)
	}
	if len(j2.Entries) != 2 {
		t.Fatalf("journal mutated: %d entries", len(j2.Entries))
	}
}

// TestRestoreMakeLinkRefusesSomeoneElsesLink: the link is there, but it points
// somewhere tq never pointed it.
func TestRestoreMakeLinkRefusesSomeoneElsesLink(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	ours := filepath.Join(base, "ours")
	theirs := filepath.Join(base, "theirs")
	mustMkdir(t, ours)
	mustMkdir(t, theirs)
	link := filepath.Join(base, "link")

	j, err := Open("wronglink")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordMakeLink("link", link, ours); err != nil {
		t.Fatal(err)
	}
	if err := MakeLink(link, theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want a refusal to remove a link pointing somewhere else")
	}
	if ok, _ := IsLink(link); !ok {
		t.Fatal("the link was removed anyway")
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
	if err := j.RecordMoveDir("mv", from, to); err != nil {
		t.Fatal(err)
	}
	// The mutation never happened: To is missing and From is still the
	// directory. That is the pre-operation state, so there is nothing to undo
	// and the restore must continue, not stop.
	lines, err := j.Restore(Runner{})
	if err != nil {
		t.Fatalf("To missing and From intact is the pre-op state, not a failure: %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "nothing to undo") {
		t.Fatalf("lines = %v", lines)
	}
	if _, err := os.Stat(from); err != nil {
		t.Fatalf("From disturbed: %v", err)
	}

	// Now do the move, but recreate From by hand -> a state tq does not
	// recognise, so it stops.
	if err := j.ResetRestore(); err != nil {
		t.Fatal(err)
	}
	if err := MoveDir(from, to); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, from)
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want error when From already exists")
	}

	// Both gone: the directory is unrecoverable and tq must say so rather than
	// report a successful no-op.
	if err := j.ResetRestore(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(from); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(to); err != nil {
		t.Fatal(err)
	}
	_, err = j.Restore(Runner{})
	if err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("want a loud failure when both locations are gone, got %v", err)
	}
}

// ------------------------------------------------------------------ git / reg

func TestRestoreGitAlreadyInPreOpState(t *testing.T) {
	tqHome(t)
	j, err := Open("gitpre")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordGitGlobalSet("a", "user.email", "old@x", "new@x", true); err != nil {
		t.Fatal(err)
	}
	if err := j.RecordGitGlobalSet("b", "core.hooksPath", "", "/h", false); err != nil {
		t.Fatal(err)
	}
	// The world is already exactly as it was before the migration.
	git := newFakeGit(map[string]string{"user.email": "old@x"})
	lines, err := j.Restore(Runner{Git: git.run})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, l := range lines {
		if !strings.Contains(l, "nothing to undo") {
			t.Fatalf("expected no-ops, got %q", l)
		}
	}
	for _, c := range git.calls {
		if len(c) > 2 && c[2] != "--get" {
			t.Fatalf("git was told to change something: %v", c)
		}
	}
}

// TestRestoreGitUnsetExit5 covers git's real behaviour: `config --global
// --unset` of a key that is not set exits 5, which RunGit surfaces as an error.
// That must not stop the restore, because "the key is not set" is the state we
// were asking for.
func TestRestoreGitUnsetExit5(t *testing.T) {
	tqHome(t)
	j, err := Open("git5")
	if err != nil {
		t.Fatal(err)
	}
	if err := j.RecordGitGlobalSet("b", "core.hooksPath", "", "/h", false); err != nil {
		t.Fatal(err)
	}
	git := newFakeGit(map[string]string{"core.hooksPath": "/h"})
	git.unsetErr = exitErr(t, 5)
	lines, err := j.Restore(Runner{Git: git.run})
	if err != nil {
		t.Fatalf("exit 5 from --unset means the key is gone, not a failure: %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "unset") {
		t.Fatalf("lines = %v", lines)
	}

	// A different exit code is a real failure.
	if err := j.ResetRestore(); err != nil {
		t.Fatal(err)
	}
	git2 := newFakeGit(map[string]string{"core.hooksPath": "/h"})
	git2.unsetErr = exitErr(t, 1)
	if _, err := j.Restore(Runner{Git: git2.run}); err == nil {
		t.Fatal("want a failure for a non-5 exit from --unset")
	}
}

func TestParseRegQuery(t *testing.T) {
	out := "\r\nHKEY_CURRENT_USER\\Environment\r\n" +
		"    GOPATH    REG_EXPAND_SZ    %USERPROFILE%\\go\r\n" +
		"    GOPATHX    REG_SZ    decoy\r\n" +
		"    Path    REG_SZ    C:\\a b\\c;C:\\d\r\n\r\n"
	for _, c := range []struct {
		name string
		want RegValue
	}{
		{"GOPATH", RegValue{Type: "REG_EXPAND_SZ", Data: `%USERPROFILE%\go`}},
		{"GOPATHX", RegValue{Type: "REG_SZ", Data: "decoy"}},
		{"Path", RegValue{Type: "REG_SZ", Data: `C:\a b\c;C:\d`}},
	} {
		got, ok := parseRegQuery(out, c.name)
		if !ok || got != c.want {
			t.Fatalf("parseRegQuery(%q) = %+v, %v; want %+v", c.name, got, ok, c.want)
		}
	}
	if _, ok := parseRegQuery(out, "NOPE"); ok {
		t.Fatal("parseRegQuery found a value that is not there")
	}
}

// TestRegTypeRoundTripReal writes and reads a real registry value under a
// scratch key (never HKCU\Environment), to prove the type survives reg.exe in
// both directions.
func TestRegTypeRoundTripReal(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	key := fmt.Sprintf(`HKCU\Software\tentaqles-test-%d`, os.Getpid())
	t.Cleanup(func() { _ = exec.Command("reg", "delete", key, "/f").Run() })

	cases := []RegValue{
		{Type: "REG_EXPAND_SZ", Data: `%USERPROFILE%\go`},
		{Type: "REG_SZ", Data: `C:\literal\path`},
		{Type: "REG_DWORD", Data: "0x1"},
	}
	for _, want := range cases {
		if _, err := runReg("set", key, "TQ_VAL", want); err != nil {
			t.Fatalf("set %+v: %v", want, err)
		}
		out, err := runReg("query", key, "TQ_VAL", RegValue{})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		got, ok := parseRegQuery(out, "TQ_VAL")
		if !ok {
			t.Fatalf("query output did not contain TQ_VAL:\n%s", out)
		}
		if got.Type != want.Type {
			t.Fatalf("type round-trip: wrote %s, read %s", want.Type, got.Type)
		}
		if want.Type != "REG_DWORD" && got.Data != want.Data {
			t.Fatalf("data round-trip: wrote %q, read %q", want.Data, got.Data)
		}
	}
	// A type tq cannot reproduce is refused rather than silently downgraded.
	if _, err := runReg("set", key, "TQ_VAL", RegValue{Type: "REG_MULTI_SZ", Data: `a\0b`}); err == nil {
		t.Fatal("want a refusal to write REG_MULTI_SZ")
	}
	if _, err := runReg("delete", key, "TQ_VAL", RegValue{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := runReg("query", key, "TQ_VAL", RegValue{}); err == nil {
		t.Fatal("query of a deleted value should fail")
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
	if err := j.RecordRegSet("reg", `HKCU\Environment`, "X", RegValue{}, false); err != nil {
		t.Fatal(err)
	}
	_, err = j.Restore(Runner{Reg: func(a, k, n string, v RegValue) (string, error) { return "", nil }})
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
	if err := j.RecordGitGlobalSet("git", "user.name", "o", "n", true); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Restore(Runner{}); err == nil {
		t.Fatal("want error when Runner.Git is nil")
	}
}

// TestRestoreOrdersBySeq: Restore documents descending Seq order, so it must
// sort rather than walk the slice backwards.
func TestRestoreOrdersBySeq(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	j, err := Open("order")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		p := filepath.Join(base, fmt.Sprintf("f%d", i))
		mustWrite(t, p, "x")
		if err := j.RecordWriteFile("w", p, Backup{}); err != nil {
			t.Fatal(err)
		}
	}
	// Someone hands us the entries out of order (a hand-edited journal, or a
	// merge of two files).
	j.Entries[0], j.Entries[2] = j.Entries[2], j.Entries[0]

	lines, err := j.Restore(Runner{})
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, l := range lines {
		// Every line names the file it acted on; recover the order from those.
		for i := 1; i <= 3; i++ {
			if strings.Contains(l, fmt.Sprintf("f%d", i)) {
				seqs = append(seqs, i)
			}
		}
	}
	want := []int{3, 2, 1}
	if len(seqs) != 3 || seqs[0] != want[0] || seqs[1] != want[1] || seqs[2] != want[2] {
		t.Fatalf("reverse order = %v, want %v (descending Seq)", seqs, want)
	}
}

// TestReverseRemoveLinkRefusesEmptyTarget: IsLink reports (true, "") for a link
// whose target it could not read, so a journal written by older code (or by a
// caller bypassing the typed constructors) can carry an empty Target.
// Recreating the link anyway would point it at whatever now sits at that name.
func TestReverseRemoveLinkRefusesEmptyTarget(t *testing.T) {
	tqHome(t)
	base := testutil.TempDir(t)
	j, err := Open("emptytgt")
	if err != nil {
		t.Fatal(err)
	}
	// Record rejects this outright...
	if err := j.RecordRemoveLink("u", filepath.Join(base, "p"), ""); err == nil {
		t.Fatal("Record should refuse an empty Target")
	}
	// ...and the reverse refuses it too, for a journal that already has one.
	j.Entries = append(j.Entries, Entry{
		Seq: 1, Step: "u", Op: OpRemoveLink,
		Args: map[string]string{"Path": filepath.Join(base, "p"), "Target": ""},
	})
	_, err = j.Restore(Runner{})
	if err == nil || !strings.Contains(err.Error(), "pointed") {
		t.Fatalf("want a refusal to guess the target, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "p")); err == nil {
		t.Fatal("a link was created anyway")
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

func assertFile(t *testing.T, p, want string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading %s: %v", p, err)
	}
	if string(b) != want {
		t.Fatalf("%s = %q, want %q", p, b, want)
	}
}
