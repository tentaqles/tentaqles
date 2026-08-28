package setup

import (
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/cli/internal/hooks"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
)

// Change describes one action Apply would take (or skip). Kind is one of:
// base-register, git-global, workspace-create, workspace-skip,
// identity-dir, trust, hook-install, hook-skip.
type Change struct {
	Kind, Target, Detail string
}

// Preview is read-only: it inspects the filesystem (existing manifests,
// hook status) but never writes anything, and reports what Apply would do.
func Preview(p *SetupPlan, hp hooks.Profiles) ([]Change, error) {
	var changes []Change

	changes = append(changes, Change{Kind: "base-register", Target: p.Base})
	changes = append(changes, Change{Kind: "git-global", Target: "~/.gitconfig"})

	base, err := registry.Normalize(p.Base)
	if err != nil {
		base = p.Base
	}

	for _, c := range p.Companies {
		root := filepath.Join(base, c.Name)
		mp := filepath.Join(root, manifest.FileName)
		if _, err := os.Stat(mp); err == nil {
			changes = append(changes, Change{Kind: "workspace-skip", Target: c.Name, Detail: "manifest already exists"})
			continue
		}
		changes = append(changes, Change{Kind: "workspace-create", Target: c.Name})
		for _, id := range effectiveIdentities(c) {
			changes = append(changes, Change{Kind: "identity-dir", Target: c.Name + "/" + id})
		}
		if p.Trust {
			changes = append(changes, Change{Kind: "trust", Target: c.Name})
		}
	}

	for _, sh := range p.Hooks {
		st := hooks.StatusOf(hooks.Shell(sh), hp)
		if st.State == "installed" || st.State == "present (unmanaged)" {
			changes = append(changes, Change{Kind: "hook-skip", Target: sh, Detail: st.State})
			continue
		}
		changes = append(changes, Change{Kind: "hook-install", Target: sh})
	}

	return changes, nil
}
