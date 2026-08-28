// Package testutil provides small helpers shared by tq's test suites.
package testutil

import (
	"path/filepath"
	"testing"
)

// TempDir returns a symlink-resolved t.TempDir(). On macOS, t.TempDir() lives
// under /var/folders/... and /var is itself a symlink to /private/var, so a
// path that later goes through filepath.EvalSymlinks (as registry.Normalize
// does) would no longer match the raw string returned by t.TempDir(). Tests
// that use a temp dir as a base, workspace root, or HOME/USERPROFILE — i.e.
// anything later compared against a normalized path — should use this helper
// instead of t.TempDir() directly.
func TempDir(t testing.TB) string {
	t.Helper()
	d := t.TempDir()
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	return d
}
