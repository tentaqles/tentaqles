package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/migrate"
	"github.com/tentaqles/tentaqles/internal/paths"
)

func TestUninstall_WithoutRestoreIsAnError(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "uninstall")
	if err == nil {
		t.Fatal("expected an error")
	}
	if err.Error() != "only --restore is implemented in this version" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestUninstall_PositionalWithoutRestoreIsAnError(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "uninstall", "latest")
	if err == nil || !strings.Contains(err.Error(), "only --restore is implemented in this version") {
		t.Fatalf("error = %v", err)
	}
}

// setupApplied runs a real apply on the synthetic tree and returns the env plus
// a snapshot of the tree as it was before the migration.
func setupApplied(t *testing.T) (*migEnv, map[string]string) {
	t.Helper()
	e := newMigEnv(t)
	e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
	e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
	migWrite(t, e.gitcfg, "[user]\n\tname = Old Name\n\temail = old@example.com\n")

	before := snapshot(t, e.home)
	code, out, errOut, err := runTQ(t, "migrate", "--apply", "--steps", "identity,git")
	if err != nil || code != 0 {
		t.Fatalf("migrate --apply: code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	return e, before
}

func TestUninstall_RestoreWithoutYesIsADryRun(t *testing.T) {
	e, before := setupApplied(t)
	after := snapshot(t, e.home)

	code, out, errOut, err := runTQ(t, "uninstall", "--restore", migTestTS)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q", code, err, errOut)
	}
	if !strings.HasPrefix(out, "journal: "+filepath.Join(e.tqHome, "backups", migTestTS)+"\n") {
		t.Fatalf("first line wrong:\n%s", out)
	}
	if !strings.Contains(out, "move-dir") || !strings.Contains(out, "make-link") {
		t.Errorf("the listing does not name what it would undo:\n%s", out)
	}
	if !strings.HasSuffix(out, "dry run — nothing changed. Re-run with --yes.\n") {
		t.Errorf("wrong final line:\n%s", out)
	}
	if strings.Contains(out, "restored ") {
		t.Errorf("a dry run claimed to restore:\n%s", out)
	}
	if d := diffSnapshots(after, snapshot(t, e.home)); len(d) != 0 {
		t.Fatalf("a restore dry run changed the tree:\n  %s", strings.Join(d, "\n  "))
	}
	_ = before
}

func TestUninstall_RestorePutsTheTreeBack(t *testing.T) {
	e, before := setupApplied(t)
	dir := paths.IdentityDir("alpha", "claude")
	if linked, _ := migrate.IsLink(dir); linked {
		t.Fatal("precondition: the identity directory should be real after the migration")
	}

	code, out, errOut, err := runTQ(t, "uninstall", "--restore", migTestTS, "--yes")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	if !strings.Contains(out, "restored ") || !strings.HasSuffix(out, " entries\n") {
		t.Fatalf("missing the restored count:\n%s", out)
	}
	if d := diffSnapshots(before, snapshot(t, e.home)); len(d) != 0 {
		t.Fatalf("the restore did not put the home tree back:\n  %s", strings.Join(d, "\n  "))
	}
	// The global email is back too.
	v, present, gerr := gitcfg.GetGlobal(gitcfg.RunGit, "user.email")
	if gerr != nil || !present || v != "old@example.com" {
		t.Fatalf("user.email = %q present=%v err=%v", v, present, gerr)
	}
}

func TestUninstall_RestoreLatest(t *testing.T) {
	e, before := setupApplied(t)
	code, out, errOut, err := runTQ(t, "uninstall", "--restore", "latest", "--yes")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q out=%s", code, err, errOut, out)
	}
	if !strings.Contains(out, migTestTS) {
		t.Errorf("the output does not name the journal it picked:\n%s", out)
	}
	if d := diffSnapshots(before, snapshot(t, e.home)); len(d) != 0 {
		t.Fatalf("restore --restore latest left differences:\n  %s", strings.Join(d, "\n  "))
	}
}

// --restore with no value at all also means latest.
func TestUninstall_RestoreBareFlagMeansLatest(t *testing.T) {
	e, before := setupApplied(t)
	code, _, errOut, err := runTQ(t, "uninstall", "--restore", "--yes")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v stderr=%q", code, err, errOut)
	}
	if d := diffSnapshots(before, snapshot(t, e.home)); len(d) != 0 {
		t.Fatalf("bare --restore left differences:\n  %s", strings.Join(d, "\n  "))
	}
}

func TestUninstall_UnknownTimestamp(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "uninstall", "--restore", "19700101T000000Z")
	if err == nil {
		t.Fatal("expected an error for a journal that does not exist")
	}
	if !strings.Contains(err.Error(), "19700101T000000Z") {
		t.Fatalf("error = %v", err)
	}
}

func TestUninstall_NoJournalsAtAll(t *testing.T) {
	newMigEnv(t)
	_, _, _, err := runTQ(t, "uninstall", "--restore", "latest")
	if err == nil {
		t.Fatal("expected an error when there are no journals")
	}
}

// Restore is not idempotent by design (reversing a move-dir puts a directory
// back where a newer make-link entry expects a link). A second run must report
// what it did rather than pretend to succeed.
func TestUninstall_SecondRestoreReportsAlreadyReversed(t *testing.T) {
	e, _ := setupApplied(t)
	if _, _, _, err := runTQ(t, "uninstall", "--restore", migTestTS, "--yes"); err != nil {
		t.Fatal(err)
	}
	code, out, _, err := runTQ(t, "uninstall", "--restore", migTestTS, "--yes")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v out=%s", code, err, out)
	}
	if !strings.Contains(out, "already reversed by an earlier restore") {
		t.Fatalf("a second restore did not say the entries were already done:\n%s", out)
	}
	if _, serr := os.Stat(filepath.Join(e.tqHome, "backups", migTestTS, "restore.log")); serr != nil {
		t.Errorf("restore.log: %v", serr)
	}
}
