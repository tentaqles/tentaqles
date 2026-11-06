// Package protect keeps tq's state folder (paths.Home()) private to the
// user: 0700 directories and 0600 files on Unix (every writer under
// internal/ already uses those modes), and on Windows an ACL restricted to
// the current user, applied once with icacls and recorded for tq doctor
// (ADR-0002, PRD v2 §8.2). It is defense in depth: no command's correctness
// depends on it, so nothing here fails a command because icacls failed; the
// record says what happened and tq doctor reports it as protected-acl.
package protect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// Record is the outcome of the one icacls run, stored at paths.ACLRecord().
// Detail is icacls' output on success and the error on failure; it never
// carries a secret, and the account name only when icacls echoed it.
type Record struct {
	OK     bool      `json:"ok"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail"`
}

// Seams: the OS, the account and the one external command, so tests never
// change a real folder's ACL.
var (
	goos        = runtime.GOOS
	currentUser = func() (string, error) {
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		return u.Username, nil
	}
	runICACLS = func(dir, account string) (string, error) {
		out, err := exec.Command("icacls", dir, "/inheritance:r", "/grant:r", account+":(OI)(CI)F", systemGrant).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
)

// systemGrant keeps the local system account on the folder alongside the
// user: removing inheritance would otherwise strip it, and backup, indexing
// and the installer's own service context run as SYSTEM.
const systemGrant = "SYSTEM:(OI)(CI)F"

// Command is the icacls line tq runs, for messages. An empty account prints
// the placeholder <user>.
func Command(dir, account string) string {
	if account == "" {
		account = "<user>"
	}
	return fmt.Sprintf(`icacls "%s" /inheritance:r /grant:r "%s:(OI)(CI)F" "%s"`, dir, account, systemGrant)
}

// MkdirPrivate creates dir and its missing parents with mode 0700 and, on
// Unix, sets 0700 on dir itself even when it already existed with a wider
// mode (a migrated identity's folder was created 0755 by an older tq).
func MkdirPrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return os.Chmod(dir, 0o700)
}

// EnsureHome creates paths.Home() with mode 0700 and, on Windows, restricts
// it to the current user once: the first call writes paths.ACLRecord() with
// the outcome and later calls see the record and do nothing. An icacls
// failure is recorded, never returned; only a failure to create the folder
// is an error.
func EnsureHome() error {
	home := paths.Home()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if goos != "windows" {
		return nil
	}
	if _, err := os.Stat(paths.ACLRecord()); err == nil {
		return nil
	}
	rec := Record{At: time.Now().UTC()}
	account, err := currentUser()
	switch {
	case err != nil:
		rec.Detail = "cannot tell the current user: " + err.Error()
	default:
		out, err := runICACLS(home, account)
		if err != nil {
			rec.Detail = "icacls failed: " + err.Error()
			if out != "" {
				rec.Detail += " (" + out + ")"
			}
		} else {
			rec.OK = true
			rec.Detail = out
		}
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil
	}
	// Best effort, like the rest: a record that cannot be written leaves the
	// next command to try again.
	_ = os.WriteFile(paths.ACLRecord(), append(raw, '\n'), 0o600)
	return nil
}

// ReadRecord returns the recorded outcome, or nil when none was recorded.
func ReadRecord() (*Record, error) {
	raw, err := os.ReadFile(paths.ACLRecord())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", paths.ACLRecord(), err)
	}
	return &r, nil
}
