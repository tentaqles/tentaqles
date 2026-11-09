package store

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/registry"
)

// maxGitFile bounds what is read from a .git file or a commondir file.
const maxGitFile = 4 << 10

// RepoKey returns the key of a repository folder: "r_" and the first 8 hex characters of the SHA-256
// of its path relative to the client folder, written with '/'. A repository outside the client folder
// is keyed by its absolute path. Windows paths are compared in lower case.
//
// A linked git worktree (a .git file naming a folder under the main repository's .git folder) takes
// the key of its main repository, so a worktree shares its repository's lessons and caches. A
// submodule, a worktree whose main repository lies outside the client folder, and a worktree of a
// bare repository (its common folder is not named .git) keeps its own key.
func RepoKey(clientDir, repoDir string) string {
	if main, ok := worktreeMain(repoDir); ok {
		if rel, ok := relInside(normalized(clientDir), normalized(main)); ok {
			return keyOf(rel)
		}
	}
	c := absClean(clientDir)
	r := absClean(repoDir)
	key := r
	if rel, ok := relInside(c, r); ok {
		key = rel
	}
	return keyOf(key)
}

// keyOf hashes a repository key: the path written with '/', lower-cased on Windows.
func keyOf(key string) string {
	key = filepath.ToSlash(key)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	return "r_" + hex.EncodeToString(sum[:])[:8]
}

// relInside is p relative to c, and whether p lies inside c (c itself included).
func relInside(c, p string) (string, bool) {
	rel, err := filepath.Rel(c, p)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// worktreeMain returns the main repository folder of the linked worktree at repoDir. It reads the
// .git file of repoDir, then the commondir file of the folder it names. A submodule has no commondir
// file, and any failed read gives false.
func worktreeMain(repoDir string) (string, bool) {
	dotGit := filepath.Join(repoDir, ".git")
	st, err := os.Stat(dotGit)
	if err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	raw, err := readSmall(dotGit)
	if err != nil {
		return "", false
	}
	gitdir, ok := gitdirLine(raw)
	if !ok {
		return "", false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(repoDir, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	raw, err = readSmall(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	common := strings.TrimSpace(string(raw))
	if common == "" {
		return "", false
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	// The common folder is the main repository's .git folder; the repository is its parent. A common
	// folder not named .git (a bare repository such as bare.git) has no such parent: false, so the
	// worktree keeps its own key.
	common = filepath.Clean(common)
	if !isDotGitName(filepath.Base(common)) {
		return "", false
	}
	return filepath.Dir(common), true
}

// isDotGitName reports whether name is ".git": case-insensitively on Windows, exactly elsewhere.
func isDotGitName(name string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name, ".git")
	}
	return name == ".git"
}

// gitdirLine returns the path after the first "gitdir:" line of a .git file, trimmed.
func gitdirLine(raw []byte) (string, bool) {
	const prefix = "gitdir:"
	for _, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, prefix) {
			if v := strings.TrimSpace(t[len(prefix):]); v != "" {
				return v, true
			}
			return "", false
		}
	}
	return "", false
}

// readSmall reads at most maxGitFile bytes of the file at path.
func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxGitFile))
}

// normalized is p as the resolver normalizes it (registry.Normalize), or its absolute, cleaned form
// when that fails.
func normalized(p string) string {
	if n, err := registry.Normalize(p); err == nil {
		return n
	}
	return absClean(p)
}

func absClean(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
