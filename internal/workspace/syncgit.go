package workspace

import (
	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

// SyncGit rewrites ~/.gitconfig-tentaqles so that ONLY trusted workspaces are
// wired into the git include chain. An untrusted workspace's
// .gitconfig-tentaqles is attacker-controllable content that git would
// otherwise execute-as-config for any repo under that root, so it must never
// be included. Call this after any change to trust (allow/deny) or to the set
// of workspaces (add), so the include set always tracks trust.
func SyncGit(cfg *registry.Config) error {
	all, _ := resolve.ListWorkspaces(cfg)
	roots := make([]string, 0, len(all))
	for _, w := range all {
		if !trust.IsTrusted(w.Hash) {
			continue
		}
		roots = append(roots, w.Root)
	}
	return gitcfg.Sync(roots)
}
