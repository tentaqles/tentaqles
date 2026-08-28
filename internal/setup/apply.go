package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/cli/internal/gitcfg"
	"github.com/tentaqles/tentaqles/cli/internal/hooks"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/providers"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/workspace"
)

// ApplyOptions carries the seams Apply needs so tests can fake git and
// point hook installs at temp profile files.
type ApplyOptions struct {
	RunGit   func(args ...string) (string, error)
	Profiles hooks.Profiles
}

// Report summarizes what Apply did.
type Report struct {
	Changes  []Change
	Logins   []string // "tq login <ws> <id>"
	Warnings []string // "<company>: <err>"
}

// loginCommand returns the effective command Provider p's login flow runs,
// preferring an explicit login.command over falling back to its CLI. Empty
// means the provider has no login flow.
func loginCommand(p providers.Provider) string {
	if p.Login != nil && p.Login.Command != "" {
		return p.Login.Command
	}
	if p.CLI != nil {
		return p.CLI.Command
	}
	return ""
}

// Apply executes a setup plan: register the base folder, ensure git's
// global include, scaffold each company as a workspace (skipping ones that
// already have a manifest), install shell hooks, and collect logins for
// every identity with a login flow. A single company's failure is recorded
// in Warnings and does not stop the others; Apply only returns an error if
// the registry/git-global step failed or every company failed.
func Apply(p *SetupPlan, cat *providers.Catalog, o ApplyOptions) (Report, error) {
	var report Report

	cfg, err := registry.Load()
	if err != nil {
		return report, fmt.Errorf("load registry: %w", err)
	}
	base, err := registry.Normalize(p.Base)
	if err != nil {
		return report, fmt.Errorf("normalize base %q: %w", p.Base, err)
	}
	if _, err := cfg.AddBase(base); err != nil {
		return report, fmt.Errorf("register base: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return report, fmt.Errorf("save registry: %w", err)
	}
	report.Changes = append(report.Changes, Change{Kind: "base-register", Target: p.Base})

	if err := gitcfg.EnsureGlobal(o.RunGit); err != nil {
		return report, fmt.Errorf("git global config: %w", err)
	}
	report.Changes = append(report.Changes, Change{Kind: "git-global", Target: "~/.gitconfig"})

	applied := 0
	for _, c := range p.Companies {
		root := filepath.Join(base, c.Name)
		mp := filepath.Join(root, manifest.FileName)
		if _, err := os.Stat(mp); err == nil {
			report.Changes = append(report.Changes, Change{Kind: "workspace-skip", Target: c.Name, Detail: "manifest already exists"})
			applied++
			continue
		}

		ids := effectiveIdentities(c)
		_, err := workspace.Add(workspace.AddOptions{
			Base:           base,
			Name:           c.Name,
			GitName:        c.GitName,
			GitEmail:       c.GitEmail,
			GitUser:        c.GitUser,
			DisplayName:    c.DisplayName,
			Color:          c.Color,
			Identities:     ids,
			PermissionMode: c.PermissionMode,
			RunGit:         o.RunGit,
		})
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		applied++

		report.Changes = append(report.Changes, Change{Kind: "workspace-create", Target: c.Name})
		for _, id := range ids {
			report.Changes = append(report.Changes, Change{Kind: "identity-dir", Target: c.Name + "/" + id})
		}
		if p.Trust {
			report.Changes = append(report.Changes, Change{Kind: "trust", Target: c.Name})
		}

		for _, id := range ids {
			prov, ok := cat.Get(id)
			if !ok {
				continue
			}
			if loginCommand(prov) != "" {
				report.Logins = append(report.Logins, fmt.Sprintf("tq login %s %s", c.Name, id))
			}
		}
	}

	for _, sh := range p.Hooks {
		pre := hooks.StatusOf(hooks.Shell(sh), o.Profiles)
		st, err := hooks.Install(hooks.Shell(sh), o.Profiles)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("hooks %s: %v", sh, err))
			continue
		}
		if pre.State == "installed" || pre.State == "present (unmanaged)" {
			report.Changes = append(report.Changes, Change{Kind: "hook-skip", Target: sh, Detail: st.State})
			continue
		}
		report.Changes = append(report.Changes, Change{Kind: "hook-install", Target: sh})
	}

	if len(p.Companies) > 0 && applied == 0 {
		return report, fmt.Errorf("no company could be applied: %v", report.Warnings)
	}
	return report, nil
}
