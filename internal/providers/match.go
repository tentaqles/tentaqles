package providers

import (
	"fmt"
	"regexp"
)

// Match is what a client's terminal hides when this provider is outside the client's scope: its
// CLIs, masked on the client's PATH, and its variables, unset in the client's environment
// (internal/scope). Every list is optional; an empty Match means nothing is hidden, which is what
// a generic tool (docker, kubectl, terraform, npm, psql) wants: it talks to whatever the project
// configures, not to one vendor.
type Match struct {
	// Executables are command names, without a Windows suffix: `aws` covers aws, aws.exe,
	// aws.cmd. The provider owns the whole command.
	Executables []string `yaml:"executables,omitempty"`
	// Subcommands claims part of a CLI another provider owns wholesale: exe -> the first
	// subcommands this provider owns (azure-devops owns `az devops|repos|...`, not `az vm`). A
	// CLI in scope through a subcommand is never masked.
	Subcommands map[string][]string `yaml:"subcommands,omitempty"`
	// Env are variable names (a trailing * is a prefix) the provider's CLI reads.
	Env []string `yaml:"env,omitempty"`
}

// Empty reports a Match that hides nothing.
func (m Match) Empty() bool {
	return len(m.Executables)+len(m.Subcommands)+len(m.Env) == 0
}

var (
	matchExeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	matchSubRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	matchEnvRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\*?$`)
)

// Validate checks every entry's spelling.
func (m Match) Validate() error {
	check := func(field string, list []string, re *regexp.Regexp) error {
		for _, e := range list {
			if !re.MatchString(e) {
				return fmt.Errorf("match.%s: invalid entry %q", field, e)
			}
		}
		return nil
	}
	if err := check("executables", m.Executables, matchExeRe); err != nil {
		return err
	}
	for exe, subs := range m.Subcommands {
		if !matchExeRe.MatchString(exe) {
			return fmt.Errorf("match.subcommands: invalid executable %q", exe)
		}
		if err := check("subcommands."+exe, subs, matchSubRe); err != nil {
			return err
		}
	}
	return check("env", m.Env, matchEnvRe)
}

// Union is m widened by o: a user provider file can only add to what a built-in hides.
func (m Match) Union(o Match) Match {
	u := func(a, b []string) []string {
		seen := map[string]bool{}
		out := make([]string, 0, len(a)+len(b))
		for _, x := range append(append([]string{}, a...), b...) {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	var subs map[string][]string
	if len(m.Subcommands)+len(o.Subcommands) > 0 {
		subs = map[string][]string{}
		for k, v := range m.Subcommands {
			subs[k] = append([]string{}, v...)
		}
		for k, v := range o.Subcommands {
			subs[k] = u(subs[k], v)
		}
	}
	return Match{Executables: u(m.Executables, o.Executables), Subcommands: subs, Env: u(m.Env, o.Env)}
}
