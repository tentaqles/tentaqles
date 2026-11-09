package sense

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

// Place says where a call ran: the repository's key (empty outside a repository) and the working
// folder relative to the repository, or to the client folder when there is no repository.
type Place struct {
	Repo string
	Cwd  string
}

// Locate finds the place of cwd inside the client folder clientDir.
func Locate(clientDir, cwd string) Place {
	c := absClean(clientDir)
	w := absClean(cwd)
	rel, err := filepath.Rel(c, w)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Place{Cwd: "."}
	}
	var parts []string
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	// Walk up from cwd to the client folder, inclusive. The walk is built from the client folder
	// and the path elements, so it never looks above it and keeps the folder names as the client
	// folder spells them.
	for k := len(parts); k >= 0; k-- {
		dir := filepath.Join(append([]string{c}, parts[:k]...)...)
		if hasGit(dir) {
			return Place{Repo: store.RepoKey(c, dir), Cwd: slashed(parts[k:])}
		}
	}
	return Place{Cwd: slashed(parts)}
}

// hasGit reports whether dir holds an entry named .git: a folder or a file. The entry is looked at
// without following a link.
func hasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// slashed joins path elements with '/', or gives "." for none.
func slashed(parts []string) string {
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// absClean is the absolute, cleaned form of p. It does not resolve symbolic links.
func absClean(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}
