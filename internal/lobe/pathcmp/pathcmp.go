// Package pathcmp compares folder paths for the lobe: a login folder against a client's own login folder.
// It imports only the standard library, so the hook and the command rules can share it.
package pathcmp

import (
	"path/filepath"
	"runtime"
	"strings"
)

// SamePath reports whether a and b name the same folder whole: both made absolute and cleaned, then
// compared case-insensitively on Windows and exactly elsewhere. Links are not resolved.
func SamePath(a, b string) bool {
	x, y := AbsPath(a), AbsPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(x, y)
	}
	return x == y
}

// AbsPath is the absolute, cleaned form of p, which drops a trailing separator. On Windows a slash
// is a separator too. Nothing is resolved through links.
func AbsPath(p string) string {
	if runtime.GOOS == "windows" {
		p = filepath.FromSlash(p)
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
