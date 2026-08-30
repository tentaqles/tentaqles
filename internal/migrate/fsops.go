// Package migrate provides the reversible journal and filesystem helpers that
// back `tq migrate` and `tq uninstall --restore`. Every mutation `tq migrate`
// performs on a user's machine is recorded as a journal entry with an exact
// inverse, so the whole migration can be replayed backwards.
package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsLink reports whether path is a symlink or (on Windows) a directory
// junction, and returns the target it points at.
//
// On Windows the reparse point is read directly with
// DeviceIoControl(FSCTL_GET_REPARSE_POINT), so only the two tags that actually
// mean "link" report true: IO_REPARSE_TAG_MOUNT_POINT (junctions, which is what
// MakeLink creates) and IO_REPARSE_TAG_SYMLINK. Every other reparse point --
// OneDrive/cloud-file placeholders, AppExec aliases, dedup stubs, WIM/WOF
// backing -- is an ordinary file or directory as far as tq is concerned;
// reporting one as a link would let RemoveLink delete real data.
//
// A path that does not exist, or is a plain file or directory, reports false.
// A link whose target cannot be read reports (true, ""); callers that act on
// the target must check for the empty string rather than trusting it.
func IsLink(path string) (bool, string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, ""
	}
	if runtime.GOOS == "windows" {
		return windowsIsLink(path, fi)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return false, ""
	}
	tgt, err := os.Readlink(path)
	if err != nil || tgt == "" {
		return true, ""
	}
	return true, normalizeTarget(tgt)
}

// normalizeTarget strips the Windows NT object-namespace prefix that reparse
// points carry, so targets compare equal to ordinary paths.
func normalizeTarget(t string) string {
	t = strings.TrimPrefix(t, `\??\`)
	if len(t) > 3 {
		t = strings.TrimSuffix(t, string(os.PathSeparator))
	}
	return t
}

// samePath compares two filesystem paths for equality, case-insensitively on
// Windows. It is used to tell "the state tq recorded" from "someone else's
// state" during a restore, so it must not report two different directories as
// the same one.
func samePath(a, b string) bool {
	a = filepath.Clean(normalizeTarget(a))
	b = filepath.Clean(normalizeTarget(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// checkNoQuote rejects a path containing a double quote. The Windows helpers
// build a cmd.exe command line by wrapping each path in quotes (see
// runCmdLine), and a path carrying a quote of its own would break out of that
// quoting. No legal Windows path contains one.
func checkNoQuote(who string, paths ...string) error {
	for _, p := range paths {
		if strings.Contains(p, `"`) {
			return fmt.Errorf("%s: refusing to use %q: a path containing a double quote cannot be passed to cmd.exe safely", who, p)
		}
	}
	return nil
}

// MakeLink creates a link at path pointing at target. On Windows it creates a
// directory junction (which, unlike a symlink, needs no elevation); elsewhere
// it creates a symlink.
//
// target must be an existing directory: an empty or missing target would
// produce a link into nowhere (or, worse, into whatever later appears at that
// name), and IsLink legitimately reports (true, "") for a link whose target it
// could not read, so callers can reach here with an empty target by accident.
func MakeLink(path, target string) error {
	if path == "" {
		return fmt.Errorf("MakeLink: empty path")
	}
	if target == "" {
		return fmt.Errorf("MakeLink: refusing to create %s with an empty target", path)
	}
	if err := checkNoQuote("MakeLink", path, target); err != nil {
		return err
	}
	fi, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("MakeLink: target %s: %w", target, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("MakeLink: target %s is not a directory", target)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("MakeLink: %s already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("MakeLink: %s: %w", path, err)
	}
	if runtime.GOOS == "windows" {
		return mklinkJ(path, target)
	}
	return os.Symlink(target, path)
}

// RemoveLink removes the link at path. It refuses to touch anything that is
// not a link, so a real identity directory can never be deleted by mistake.
func RemoveLink(path string) error {
	if ok, _ := IsLink(path); !ok {
		return fmt.Errorf("RemoveLink: %s is not a link", path)
	}
	return os.Remove(path)
}

// MoveDir renames from to to.
//
// It refuses to move a path that is itself a link (callers remove links
// first), refuses to overwrite an existing destination, and never falls back to
// a copy: copying an identity directory would duplicate credentials rather than
// move them. A rename that fails because the two paths are on different volumes
// says so; any other failure is reported as the operating system reported it,
// because the usual cause is a sharing violation from a process still holding
// the directory open, and telling the user to move it by hand in that case
// would be dangerous advice.
func MoveDir(from, to string) error {
	if ok, _ := IsLink(from); ok {
		return fmt.Errorf("MoveDir: %s is a link, not a directory (remove the link first)", from)
	}
	if _, err := os.Lstat(from); err != nil {
		return fmt.Errorf("MoveDir: source %s: %w", from, err)
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("MoveDir: destination %s already exists", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("MoveDir: destination %s: %w", to, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("MoveDir: creating parent of %s: %w", to, err)
	}
	if err := os.Rename(from, to); err != nil {
		if isCrossDevice(err) {
			return fmt.Errorf("MoveDir: %s and %s are on different volumes; tq does not copy identity directories (that would duplicate credentials instead of moving them). Move it yourself and re-run: %w", from, to, err)
		}
		return fmt.Errorf("MoveDir: renaming %s to %s: %w (if this is a sharing violation, close anything still using the directory -- an editor, a shell, a running agent -- and re-run)", from, to, err)
	}
	return nil
}
