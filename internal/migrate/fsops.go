// Package migrate provides the reversible journal and filesystem helpers that
// back `tq migrate` and `tq uninstall --restore`. Every mutation `tq migrate`
// performs on a user's machine is recorded as a journal entry with an exact
// inverse, so the whole migration can be replayed backwards.
package migrate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// fileAttributeReparsePoint is Windows' FILE_ATTRIBUTE_REPARSE_POINT. It is
// spelled out here rather than taken from the syscall package so this file
// stays buildable on every GOOS.
const fileAttributeReparsePoint = 0x400

// IsLink reports whether path is a symlink or (on Windows) a directory
// junction, and returns the target it points at.
//
// On Windows a junction is a reparse point. Depending on the Go version it may
// or may not carry os.ModeSymlink, so the reparse-point attribute is checked as
// well. The target is read with os.Readlink; if that fails on a junction we
// fall back to parsing `cmd /c dir /AL <parent>`, which prints the target in
// brackets after the entry name.
//
// A path that does not exist, or is a plain file or directory, reports false.
func IsLink(path string) (bool, string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, ""
	}
	isLink := fi.Mode()&os.ModeSymlink != 0
	if !isLink && runtime.GOOS == "windows" && hasReparsePoint(fi) {
		isLink = true
	}
	if !isLink {
		return false, ""
	}
	if tgt, err := os.Readlink(path); err == nil && tgt != "" {
		return true, normalizeTarget(tgt)
	}
	if runtime.GOOS == "windows" {
		if tgt, err := linkTargetViaDir(path); err == nil && tgt != "" {
			return true, normalizeTarget(tgt)
		}
	}
	// It is a link, but we could not read where it points.
	return true, ""
}

// normalizeTarget strips the Windows NT object-namespace prefix that reparse
// points sometimes carry, so targets compare equal to ordinary paths.
func normalizeTarget(t string) string {
	t = strings.TrimPrefix(t, `\??\`)
	return strings.TrimSuffix(t, string(os.PathSeparator))
}

// hasReparsePoint reads FILE_ATTRIBUTE_REPARSE_POINT off a Windows FileInfo.
// fi.Sys() is a *syscall.Win32FileAttributeData there, a type that does not
// exist on other platforms, so the field is read reflectively to keep this
// file free of build tags. It always returns false off Windows.
func hasReparsePoint(fi os.FileInfo) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	v := reflect.ValueOf(fi.Sys())
	if !v.IsValid() {
		return false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	f := v.FieldByName("FileAttributes")
	if !f.IsValid() || !f.CanUint() {
		return false
	}
	return f.Uint()&fileAttributeReparsePoint != 0
}

// linkTargetViaDir recovers a junction's target by parsing `dir /AL` output,
// which renders reparse entries as `<JUNCTION>  name [C:\real\target]`.
func linkTargetViaDir(path string) (string, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	out, err := exec.Command("cmd", "/c", "dir", "/AL", parent).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dir /AL %s: %w (%s)", parent, err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), strings.ToLower(base)) {
			continue
		}
		open := strings.LastIndex(line, "[")
		closeIdx := strings.LastIndex(line, "]")
		if open >= 0 && closeIdx > open {
			return strings.TrimSpace(line[open+1 : closeIdx]), nil
		}
	}
	return "", fmt.Errorf("dir /AL %s: no reparse entry for %s", parent, base)
}

// mklinkJ creates a Windows directory junction at link pointing at target.
func mklinkJ(link, target string) error {
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %s %s: %w (%s)", link, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MakeLink creates a link at path pointing at target. On Windows it creates a
// directory junction (which, unlike a symlink, needs no elevation); elsewhere
// it creates a symlink.
func MakeLink(path, target string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("MakeLink: %s already exists", path)
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
// a copy: a rename that fails because the two paths are on different volumes is
// returned as an error, because copying an identity directory would duplicate
// credentials rather than move them.
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
		return fmt.Errorf("MoveDir: renaming %s to %s failed (cross-volume moves are not supported; move the directory manually and re-run): %w", from, to, err)
	}
	return nil
}
