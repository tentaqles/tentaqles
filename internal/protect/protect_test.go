package protect

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// fake replaces the OS, the account and icacls for one test and returns the
// calls icacls received.
func fake(t *testing.T, osName string, icaclsErr error) *[]string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	prevOS, prevUser, prevRun := goos, currentUser, runICACLS
	var calls []string
	goos = osName
	currentUser = func() (string, error) { return `BOX\someone`, nil }
	runICACLS = func(dir, account string) (string, error) {
		calls = append(calls, dir+"|"+account)
		if icaclsErr != nil {
			return "Access is denied.", icaclsErr
		}
		return "processed file: " + dir + "\nSuccessfully processed 1 files", nil
	}
	t.Cleanup(func() { goos, currentUser, runICACLS = prevOS, prevUser, prevRun })
	return &calls
}

func TestEnsureHome_CreatesPrivateDirAndRunsICACLSOnce(t *testing.T) {
	calls := fake(t, "windows", nil)
	if err := EnsureHome(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(paths.Home())
	if err != nil || !fi.IsDir() {
		t.Fatalf("home not created: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != paths.Home()+`|BOX\someone` {
		t.Fatalf("icacls calls = %v", *calls)
	}
	rec, err := ReadRecord()
	if err != nil || rec == nil || !rec.OK || !strings.Contains(rec.Detail, "processed") || rec.At.IsZero() {
		t.Fatalf("record = %+v, %v", rec, err)
	}
	// Once: a second call sees the record and does not run icacls again.
	if err := EnsureHome(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("icacls must run once, ran %d times", len(*calls))
	}
}

func TestEnsureHome_ICACLSFailureIsRecordedNeverReturned(t *testing.T) {
	fake(t, "windows", errors.New("exit status 5"))
	if err := EnsureHome(); err != nil {
		t.Fatalf("an icacls failure must not fail the command: %v", err)
	}
	rec, err := ReadRecord()
	if err != nil || rec == nil || rec.OK || !strings.Contains(rec.Detail, "exit status 5") || !strings.Contains(rec.Detail, "Access is denied") {
		t.Fatalf("record = %+v, %v", rec, err)
	}
}

func TestEnsureHome_UnixUsesModesOnly(t *testing.T) {
	calls := fake(t, "linux", nil)
	if err := EnsureHome(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("icacls is Windows-only, ran %v", *calls)
	}
	if rec, err := ReadRecord(); err != nil || rec != nil {
		t.Fatalf("no record off Windows, got %+v %v", rec, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(paths.Home()); fi.Mode().Perm() != 0o700 {
			t.Fatalf("home mode %o, want 0700", fi.Mode().Perm())
		}
	}
}

func TestReadRecord_CorruptIsAnError(t *testing.T) {
	fake(t, "windows", nil)
	os.MkdirAll(paths.Home(), 0o700)
	os.WriteFile(paths.ACLRecord(), []byte("{not json"), 0o600)
	if _, err := ReadRecord(); err == nil {
		t.Fatal("a corrupt record must be reported, not read as absent")
	}
}

func TestCommand(t *testing.T) {
	c := Command(`C:\Users\x\.tentaqles`, "")
	if !strings.Contains(c, "/inheritance:r") || !strings.Contains(c, "<user>:(OI)(CI)F") {
		t.Fatal(c)
	}
}
