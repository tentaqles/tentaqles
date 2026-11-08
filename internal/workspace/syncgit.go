package workspace

import (
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// SyncGit rewrites ~/.gitconfig-tentaqles so that every client folder is wired into git's
// include chain: each one's own name and email apply to every repository under it. Call it
// after the set of clients changes.
func SyncGit(cfg *registry.Config) error {
	all, _ := resolve.ListWorkspaces(cfg)
	roots := make([]string, 0, len(all))
	for _, w := range all {
		roots = append(roots, w.Root)
	}
	return gitcfg.Sync(roots)
}
