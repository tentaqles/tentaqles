// Package providers defines the provider catalog schema: CLI metadata, install
// hints, identity env templates, and login/verify commands. It is loaded from
// an embedded catalog plus user overrides under ~/.tentaqles/providers/.
package providers

import (
	"fmt"
	"regexp"
	"strings"
)

// CLI describes how to invoke and version-check a provider's command-line tool.
type CLI struct {
	Command     string   `yaml:"command"`
	VersionArgs []string `yaml:"version_args"`
}

// InstallOS holds per-package-manager install hints for one operating system.
type InstallOS struct {
	Winget string `yaml:"winget"`
	Scoop  string `yaml:"scoop"`
	Brew   string `yaml:"brew"`
	Apt    string `yaml:"apt"`
	Pip    string `yaml:"pip"`
	Npm    string `yaml:"npm"`
	URL    string `yaml:"url"`
	Note   string `yaml:"note"`
}

// Install holds install hints across the three supported operating systems.
type Install struct {
	Windows InstallOS `yaml:"windows"`
	Macos   InstallOS `yaml:"macos"`
	Linux   InstallOS `yaml:"linux"`
}

// Identity holds the env-var template map for pointing a CLI at a private
// config home. Values may contain the literal placeholder "{dir}".
type Identity struct {
	Env map[string]string `yaml:"env"`
}

// Cmd is a command plus arguments. Command defaults to the provider's
// CLI.Command when empty.
type Cmd struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args"`
}

// Provider is one entry in the catalog.
type Provider struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Category string `yaml:"category"`
	Docs     string `yaml:"docs"`

	CLI      *CLI     `yaml:"cli"`
	Install  Install  `yaml:"install"`
	Identity Identity `yaml:"identity"`
	Login    *Cmd     `yaml:"login"`
	Verify   *Cmd     `yaml:"verify"`

	BlockedSuggested []string `yaml:"blocked_commands_suggested"`

	// Source is "embedded" for catalog entries or the file path for user
	// overrides. Never serialized.
	Source string `yaml:"-"`
}

// Categories is the exact set of valid provider categories.
var Categories = []string{"cloud", "vcs", "data", "deploy", "pm", "agent", "other"}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Validate checks the provider's structural rules.
func (p Provider) Validate() error {
	if !idRe.MatchString(p.ID) {
		return fmt.Errorf("provider %q: invalid id (must match %s)", p.ID, idRe.String())
	}
	if p.Name == "" {
		return fmt.Errorf("provider %q: name is required", p.ID)
	}
	validCategory := false
	for _, c := range Categories {
		if p.Category == c {
			validCategory = true
			break
		}
	}
	if !validCategory {
		return fmt.Errorf("provider %q: invalid category %q (must be one of %v)", p.ID, p.Category, Categories)
	}
	if p.CLI != nil && p.CLI.Command == "" {
		return fmt.Errorf("provider %q: cli.command is required when cli is set", p.ID)
	}
	for k, v := range p.Identity.Env {
		if strings.Contains(v, "..") {
			return fmt.Errorf("provider %q: identity.env[%s] must not contain \"..\"", p.ID, k)
		}
	}
	return nil
}

// Vars expands the identity env template for the given directory. Each value
// has "{dir}" replaced by dir, then is normalized with filepath.FromSlash.
func (p Provider) Vars(dir string) map[string]string {
	return expandVars(p.Identity.Env, dir)
}

// HasIdentity reports whether the provider defines any identity env vars.
func (p Provider) HasIdentity() bool {
	return len(p.Identity.Env) > 0
}
