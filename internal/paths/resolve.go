package paths

import (
	"path/filepath"
	"strings"
)

// Resolve returns p absolute and clean, with the symlinks in its longest
// existing prefix resolved and the missing tail kept, so a folder that does
// not exist yet compares equal to its resolved root (/var vs /private/var on
// macOS).
func Resolve(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return evalExisting(filepath.Clean(p))
}

func evalExisting(p string) string {
	if e, err := filepath.EvalSymlinks(p); err == nil {
		return e
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(evalExisting(parent), filepath.Base(p))
}

// IsOutside reports a filepath.Rel result that leaves its base.
func IsOutside(rel string) bool {
	return filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Rel is filepath.Rel(root, p) that also compares the resolved forms
// (Resolve) when the spelled ones do not nest: a root spelled through a
// symlink and a cwd the OS reports resolved (or the reverse) still give the
// path inside the root.
func Rel(root, p string) (string, error) {
	r, err := filepath.Rel(root, p)
	if err == nil && !IsOutside(r) {
		return r, nil
	}
	return filepath.Rel(Resolve(root), Resolve(p))
}
