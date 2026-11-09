package sense

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// Place says where a call ran: the repository's key (empty outside a repository) and the working
// folder relative to the repository, or to the client folder when there is no repository.
type Place struct {
	Repo string
	Cwd  string
}

// Locate finds the place of cwd inside the client folder clientDir.
//
// Both paths are normalized the way the resolver normalizes them (registry.Normalize: absolute,
// cleaned, with symbolic links and Windows short names resolved), so a folder reached through a
// link or a short name is the same folder as its real spelling. A path that cannot be normalized
// keeps its absolute, cleaned form.
func Locate(clientDir, cwd string) Place {
	c := normalized(clientDir)
	w := normalized(cwd)
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

// normalized is p as the resolver normalizes it. If that fails, it is the absolute, cleaned form
// of p, which does not resolve symbolic links.
func normalized(p string) string {
	if n, err := registry.Normalize(p); err == nil {
		return n
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
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
