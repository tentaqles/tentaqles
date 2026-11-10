package pathcmp

import (
	"path/filepath"
	"testing"
)

// TestSamePathTrailingSeparator: a trailing separator and a relative spelling name the same folder.
func TestSamePathTrailingSeparator(t *testing.T) {
	dir := t.TempDir()
	if !SamePath(dir, dir+string(filepath.Separator)) {
		t.Errorf("%s and the same folder with a trailing separator should match", dir)
	}
	if !SamePath(dir, filepath.Join(dir, "..", filepath.Base(dir))) {
		t.Errorf("a spelling that cleans to %s should match", dir)
	}
}

// TestSamePathDifferentFolders: two folders that differ, and a sibling that shares a prefix, do not match.
func TestSamePathDifferentFolders(t *testing.T) {
	dir := t.TempDir()
	if SamePath(dir, filepath.Join(dir, "x")) {
		t.Errorf("a folder and its child should not match")
	}
	if SamePath(dir, dir+"2") {
		t.Errorf("a folder and a sibling with a longer name should not match")
	}
}
