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

// EnvKeyRe is the set of environment-variable names tq is willing to emit.
// Anything outside it could break out of a shell assignment (e.g. "X;curl x|sh").
var EnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// hasControlChars reports whether s contains an ASCII control character.
func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// ValidateEnvPair checks a single identity env key/value for injection-unsafe
// content. It is used by Validate and by "tq providers add".
func ValidateEnvPair(k, v string) error {
	if !EnvKeyRe.MatchString(k) {
		return fmt.Errorf("identity.env key %q is not a valid environment variable name (must match %s)", k, EnvKeyRe.String())
	}
	if hasControlChars(v) {
		return fmt.Errorf("identity.env[%s] must not contain control characters", k)
	}
	if strings.Contains(v, "..") {
		return fmt.Errorf("identity.env[%s] must not contain \"..\"", k)
	}
	return nil
}

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
		if err := ValidateEnvPair(k, v); err != nil {
			return fmt.Errorf("provider %q: %w", p.ID, err)
		}
	}
	for _, osHints := range []struct {
		name string
		os   InstallOS
	}{
		{"windows", p.Install.Windows},
		{"macos", p.Install.Macos},
		{"linux", p.Install.Linux},
	} {
		if err := osHints.os.validate(); err != nil {
			return fmt.Errorf("provider %q: install.%s.%w", p.ID, osHints.name, err)
		}
	}
	return nil
}

// installMetachars are the shell metacharacters an install hint must never
// contain. Install hints are surfaced verbatim to the user and can be run by
// the desktop app's "Install" button, so a hint like
// "winget install x & calc" must not be able to smuggle in a second command.
const installMetachars = "&|;^<>$`\n\r\"'"

// validate checks the runnable install hints for shell metacharacters. URL is
// exempt entirely (it is opened, never executed, and legitimately contains
// characters like "&" in query strings) and Note is free-form prose.
func (o InstallOS) validate() error {
	for _, f := range []struct {
		field string
		value string
	}{
		{"winget", o.Winget},
		{"scoop", o.Scoop},
		{"brew", o.Brew},
		{"apt", o.Apt},
		{"pip", o.Pip},
		{"npm", o.Npm},
	} {
		if i := strings.IndexAny(f.value, installMetachars); i >= 0 {
			return fmt.Errorf("%s: must not contain the shell metacharacter %q", f.field, string(f.value[i]))
		}
		if hasControlChars(f.value) {
			return fmt.Errorf("%s: must not contain control characters", f.field)
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
