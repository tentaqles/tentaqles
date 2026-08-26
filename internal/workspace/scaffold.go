// Package workspace creates and registers workspaces.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
	"gopkg.in/yaml.v3"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type AddOptions struct {
	Base, Name, GitName, GitEmail, DisplayName, Color string
	Identities                                        []string
	PermissionMode                                    string
	RunGit                                            func(args ...string) (string, error)
}

func Add(o AddOptions) (*resolve.Workspace, error) {
	if !nameRe.MatchString(o.Name) {
		return nil, fmt.Errorf("workspace name %q must match %s", o.Name, nameRe)
	}
	if o.GitEmail == "" {
		return nil, fmt.Errorf("--git-email is required")
	}
	if err := gitcfg.ValidateValue(o.GitEmail); err != nil {
		return nil, err
	}
	if err := gitcfg.ValidateValue(o.GitName); err != nil {
		return nil, err
	}
	if len(o.Identities) == 0 {
		o.Identities = []string{"claude", "gh"}
	}
	base, err := registry.Normalize(o.Base)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(base, o.Name)
	mp := filepath.Join(root, manifest.FileName)
	if _, err := os.Stat(mp); err == nil {
		return nil, fmt.Errorf("%s already exists", mp)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	m := manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: o.Name, DisplayName: o.DisplayName, Color: o.Color,
		Git:        manifest.Git{Name: o.GitName, Email: o.GitEmail},
		Identities: map[string]manifest.Identity{},
		Claude:     manifest.Claude{PermissionMode: o.PermissionMode},
	}
	for _, id := range o.Identities {
		m.Identities[id] = manifest.Identity{}
	}
	raw, err := yaml.Marshal(&m)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(mp, append([]byte("# tentaqles workspace manifest — names only, never secrets\n"), raw...), 0o644); err != nil {
		return nil, err
	}
	if _, err := manifest.Load(mp); err != nil {
		return nil, err
	}
	for _, id := range o.Identities {
		d := paths.IdentityDir(o.Name, id)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
		if id == "claude" {
			s := filepath.Join(d, "settings.json")
			if _, err := os.Stat(s); os.IsNotExist(err) {
				_ = os.WriteFile(s, []byte("{}\n"), 0o600)
			}
		}
	}
	if err := gitcfg.WriteWorkspace(root, o.GitName, o.GitEmail); err != nil {
		return nil, err
	}
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	if _, err := cfg.AddBase(base); err != nil {
		return nil, err
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}
	// Trust first, then sync: SyncGit only wires trusted workspaces into git's
	// include chain, so the new workspace must be trusted before it can appear.
	h, err := trust.HashFile(mp)
	if err != nil {
		return nil, err
	}
	if err := trust.Allow(h); err != nil {
		return nil, err
	}
	if err := SyncGit(cfg); err != nil {
		return nil, err
	}
	if o.RunGit != nil {
		if err := gitcfg.EnsureGlobal(o.RunGit); err != nil {
			return nil, err
		}
	}
	res := resolve.Resolve(root, cfg)
	if res.Workspace == nil {
		return nil, fmt.Errorf("scaffolded but cannot resolve: %s", res.Reason)
	}
	return res.Workspace, nil
}
