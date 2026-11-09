package store

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// RepoKey returns the key of a repository folder: "r_" and the first 8 hex characters of the SHA-256
// of its path relative to the client folder, written with '/'. A repository outside the client folder
// is keyed by its absolute path. Windows paths are compared in lower case.
func RepoKey(clientDir, repoDir string) string {
	c := absClean(clientDir)
	r := absClean(repoDir)
	key := r
	if rel, err := filepath.Rel(c, r); err == nil && !filepath.IsAbs(rel) &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		key = rel
	}
	key = filepath.ToSlash(key)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	return "r_" + hex.EncodeToString(sum[:])[:8]
}

func absClean(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
