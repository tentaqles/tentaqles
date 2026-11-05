package providers

import (
	"fmt"
	"regexp"
	"strings"
)

// Match is the data the guard's identity category matches a provider by when the provider is
// outside an identity's scope (PRD v2 §6.3 "ship the match lists as data files per provider";
// ADR-0006). Every list is optional; an empty Match means the guard never recognises the
// provider, which is what a generic tool (docker, kubectl, terraform, npm, psql) wants for its
// executable: it talks to whatever the project configures, not to one vendor.
//
// The lists are literal spellings. The guard is a text filter: it matches what a tool call
// writes, never what a command would compute at run time.
type Match struct {
	// Executables are command names, without a Windows suffix: the guard strips .exe, .cmd,
	// .bat, .ps1 and .com from the word it reads, so `aws` matches aws, aws.exe, aws.cmd and
	// C:\Program Files\Amazon\AWSCLIV2\aws.exe. Also matched as the package of a runner
	// (npx, bunx, pnpm dlx, pipx run, uvx).
	Executables []string `yaml:"executables,omitempty"`
	// Modules are Python modules run with `python -m <module>` (awscli, azure.cli).
	Modules []string `yaml:"modules,omitempty"`
	// Env are environment variable names (credentials, profiles, config homes). A trailing *
	// matches a prefix (TF_TOKEN_*). Matched as a reference: $NAME, ${NAME}, $env:NAME,
	// %NAME%, env:NAME, NAME=..., export/unset/set/setx/printenv NAME, os.environ['NAME'],
	// getenv("NAME"), process.env.NAME.
	Env []string `yaml:"env,omitempty"`
	// Paths are credential and config files or folders. Each starts with ~/ (the user's
	// home), %APPDATA%/ or %LOCALAPPDATA%/ (Windows; skipped where the variable is unset). A
	// folder covers everything under it.
	Paths []string `yaml:"paths,omitempty"`
	// Hosts are API hostnames. A host matches itself and every subdomain: amazonaws.com
	// matches ec2.amazonaws.com.
	Hosts []string `yaml:"hosts,omitempty"`
	// MCP are tokens of an MCP server name. The server segment of mcp__<server>__<tool> is
	// split on _ - . and lower-cased; the server matches when one of its tokens is listed
	// (aws matches aws, awslabs-aws-api and plugin_x_aws).
	MCP []string `yaml:"mcp,omitempty"`
}

// Empty reports whether m matches nothing.
func (m Match) Empty() bool {
	return len(m.Executables)+len(m.Modules)+len(m.Env)+len(m.Paths)+len(m.Hosts)+len(m.MCP) == 0
}

var (
	matchExeRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	matchModuleRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	matchEnvRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\*?$`)
	matchHostRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	matchMCPRe    = regexp.MustCompile(`^[a-z0-9]+$`)
)

// PathRoots are the prefixes a match path may start with.
var PathRoots = []string{"~/", "%APPDATA%/", "%LOCALAPPDATA%/"}

// Validate checks every list's spelling, so a bad entry fails the catalog load (embedded) or
// is skipped with the file (a user override), never silently matching nothing.
func (m Match) Validate() error {
	check := func(field string, list []string, re *regexp.Regexp) error {
		for _, v := range list {
			if !re.MatchString(v) {
				return fmt.Errorf("%s: %q is not a valid entry (must match %s)", field, v, re.String())
			}
		}
		return nil
	}
	if err := check("executables", m.Executables, matchExeRe); err != nil {
		return err
	}
	if err := check("modules", m.Modules, matchModuleRe); err != nil {
		return err
	}
	if err := check("env", m.Env, matchEnvRe); err != nil {
		return err
	}
	if err := check("hosts", m.Hosts, matchHostRe); err != nil {
		return err
	}
	if err := check("mcp", m.MCP, matchMCPRe); err != nil {
		return err
	}
	for _, p := range m.Paths {
		ok := false
		for _, r := range PathRoots {
			if strings.HasPrefix(p, r) && len(p) > len(r) {
				ok = true
			}
		}
		if !ok || hasControlChars(p) || strings.Contains(p, "..") || strings.ContainsAny(p, `\*?[`) {
			return fmt.Errorf("paths: %q must start with one of %v, use forward slashes, and hold no .., glob or control character", p, PathRoots)
		}
	}
	return nil
}

// Union returns m with every entry of o that m lacks appended: a user override file extends
// the embedded lists and never shortens them (ADR-0006). To stop the guard matching a
// provider, put it in the identity's scope.
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
	return Match{
		Executables: u(m.Executables, o.Executables),
		Modules:     u(m.Modules, o.Modules),
		Env:         u(m.Env, o.Env),
		Paths:       u(m.Paths, o.Paths),
		Hosts:       u(m.Hosts, o.Hosts),
		MCP:         u(m.MCP, o.MCP),
	}
}
