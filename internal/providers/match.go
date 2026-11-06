package providers

import (
	"fmt"
	"regexp"
	"strings"
)

// Match is the data the guard's identity category matches a provider by when the provider is
// outside an identity's scope (PRD v2 §6.3 "ship the match lists as data files per provider";
// ADR-0007). Every list is optional; an empty Match means the guard never recognises the
// provider, which is what a generic tool (docker, kubectl, terraform, npm, psql) wants for its
// executable: it talks to whatever the project configures, not to one vendor.
//
// The lists are literal spellings. The guard is a text filter: it matches what a tool call
// writes, never what a command would compute at run time.
type Match struct {
	// Executables are command names, without a Windows suffix: the guard strips .exe, .cmd,
	// .bat, .ps1 and .com from the word it reads, so `aws` matches aws, aws.exe, aws.cmd and
	// C:\Program Files\Amazon\AWSCLIV2\aws.exe. The provider owns the whole command. Also
	// matched as the package of a runner (npx, bunx, pnpm dlx, pipx run, uvx), a docker-run
	// image's basename, and a find -exec / Get-Command / Start-Process target.
	Executables []string `yaml:"executables,omitempty"`
	// Subcommands claims part of a CLI another provider owns wholesale: exe -> the first
	// subcommands this provider owns (azure-devops owns `az devops|repos|pipelines|boards|
	// artifacts|login|extension`, not `az vm`). A command matches a provider when its exe is
	// in Executables (any subcommand) or its exe+first-subcommand is in Subcommands. The most
	// precise in-scope claim wins (ADR-0007, round 2).
	Subcommands map[string][]string `yaml:"subcommands,omitempty"`
	// Images are docker/podman image names (basename, no tag) whose container is the
	// provider's CLI: amazon/aws-cli, cloudsdktool/google-cloud-cli.
	Images []string `yaml:"images,omitempty"`
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
	// matches ec2.amazonaws.com. An entry may carry a path prefix, for a vendor whose API
	// shares a host with public content: gitlab.com/api/ matches https://gitlab.com/api/v4/x
	// but not https://gitlab.com/group/repo.git (ADR-0007 round 3). List API hosts, never a
	// whole registrable domain that also serves public downloads (githubusercontent.com,
	// googleapis.com).
	Hosts []string `yaml:"hosts,omitempty"`
	// Public are exceptions to Hosts: public content the vendor serves under its API domain
	// (fonts.googleapis.com, a public GCS object storage.googleapis.com/<bucket>/<object>, an
	// installer CDN such as awscli.amazonaws.com). An entry is an exact host (no subdomains),
	// optionally with a path prefix; it wins over a hosts entry it matches at least as
	// precisely (a longer host, then a longer path), so storage.googleapis.com/storage/ stays
	// an API path under the public storage.googleapis.com/ (ADR-0007 round 4).
	Public []string `yaml:"public,omitempty"`
	// GitHosts are the provider's git transport hosts (github.com, gitlab.com): a git command
	// over ssh (git@host:, ssh://) or a git push over https to one, and an ssh connection to
	// one, use the provider's credentials. An anonymous https clone, fetch, pull or ls-remote
	// does not (ADR-0007 round 4).
	GitHosts []string `yaml:"git_hosts,omitempty"`
	// PSModules are PowerShell module names (AWS.Tools, AWSPowerShell): matched as the
	// argument of Import-Module, Install-Module and the other *-Module / *-PSResource verbs and
	// `using module`; a dotted child (AWS.Tools.S3) is covered by its parent.
	PSModules []string `yaml:"psmodules,omitempty"`
	// Cmdlets are PowerShell command names of the provider's modules, matched as a command
	// verb: an exact name (Set-AWSCredential), or `*-<NounPrefix>*` for any verb whose noun
	// starts with the prefix (`*-S3*` matches Get-S3Bucket). Case-insensitive.
	Cmdlets []string `yaml:"cmdlets,omitempty"`
	// MCP are tokens of an MCP server name. The server segment of mcp__<server>__<tool> is
	// split on _ - . and lower-cased; the server matches when one of its tokens is listed
	// (aws matches aws, awslabs-aws-api and plugin_x_aws).
	MCP []string `yaml:"mcp,omitempty"`
	// MCPPackages are prefixes of the package, image or module an MCP server's config runs
	// (awslabs., @aws/, public.ecr.aws/, @modelcontextprotocol/server-github): a server whose
	// command or one of its args starts with one is the provider's, whatever its name
	// (ADR-0007). Matched case-insensitively, a version suffix included.
	MCPPackages []string `yaml:"mcp_packages,omitempty"`
	// MCPHosts are hosts of a vendor's own remote MCP endpoints (mcp.vercel.com,
	// mcp.cloudflare.com and its subdomains): an MCP server whose url, or a URL among its args
	// (mcp-remote <url>), is on one is the provider's (ADR-0007). Kept apart from Hosts, which
	// the hook's network rules read.
	MCPHosts []string `yaml:"mcp_hosts,omitempty"`
}

// Empty reports whether m matches nothing.
func (m Match) Empty() bool {
	return len(m.Executables)+len(m.Subcommands)+len(m.Images)+len(m.Modules)+len(m.PSModules)+len(m.Cmdlets)+
		len(m.Env)+len(m.Paths)+len(m.Hosts)+len(m.GitHosts)+len(m.MCP)+len(m.MCPPackages)+len(m.MCPHosts) == 0
}

var (
	matchExeRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	matchSubRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	matchImageRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	matchModuleRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	matchPSModuleRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*$`)
	matchCmdletRe   = regexp.MustCompile(`^(\*|[A-Za-z]+)-[A-Za-z0-9]+\*?$`)
	matchEnvRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\*?$`)
	matchHostRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(/[a-z0-9._~-]+)*/?$`)
	matchMCPRe      = regexp.MustCompile(`^[a-z0-9]+$`)
	matchGitHostRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	matchPkgRe      = regexp.MustCompile(`^[@A-Za-z0-9][A-Za-z0-9@._/:+-]*$`)
)

// SplitHost splits a hosts entry into its host and its path prefix ("" when it has none):
// gitlab.com/api/ -> gitlab.com, /api/.
func SplitHost(entry string) (host, path string) {
	if i := strings.IndexByte(entry, '/'); i >= 0 {
		return entry[:i], entry[i:]
	}
	return entry, ""
}

// HostMatches reports whether a request to host with URL path path (lower-cased; "" or "/"
// for none) is covered by a hosts entry: the host or a subdomain of it, and, when the entry
// has a path prefix, a path under it.
func HostMatches(entry, host, path string) bool {
	eh, ep := SplitHost(entry)
	if host != eh && !strings.HasSuffix(host, "."+eh) {
		return false
	}
	if ep == "" {
		return true
	}
	if path == "" {
		path = "/"
	}
	return strings.HasPrefix(path, ep) || path+"/" == ep
}

// NormalizeURLPath is a URL path as a server resolves it, for the prefix match of a hosts
// entry (QA round 3): lower-cased, percent-encoded unreserved characters decoded, repeated
// slashes collapsed, `.` and `..` segments resolved; a trailing slash is kept. "" stays "".
func NormalizeURLPath(p string) string {
	if p == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '%' && i+2 < len(p) {
			if c, ok := unhex(p[i+1], p[i+2]); ok && isUnreserved(c) {
				b.WriteByte(c)
				i += 2
				continue
			}
		}
		b.WriteByte(p[i])
	}
	raw := strings.ToLower(b.String())
	trailing := strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, "/.") || strings.HasSuffix(raw, "/..")
	var out []string
	for _, seg := range strings.Split(raw, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	n := "/" + strings.Join(out, "/")
	if trailing && n != "/" {
		n += "/"
	}
	return n
}

func unhex(a, b byte) (byte, bool) {
	h := func(c byte) (byte, bool) {
		switch {
		case c >= '0' && c <= '9':
			return c - '0', true
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10, true
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	x, ok1 := h(a)
	y, ok2 := h(b)
	return x<<4 | y, ok1 && ok2
}

func isUnreserved(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~'
}

// hostSpecificity ranks how precisely an entry matched: a longer host, then a longer path.
func hostSpecificity(entry string) int {
	h, p := SplitHost(entry)
	return len(h)*1000 + len(p)
}

// PublicMatches reports whether a request is covered by a public entry: the exact host (no
// subdomains) and, with a path prefix, a path under it.
func PublicMatches(entry, host, path string) bool {
	eh, ep := SplitHost(entry)
	if host != eh {
		return false
	}
	if ep == "" {
		return true
	}
	if path == "" {
		path = "/"
	}
	return strings.HasPrefix(path, ep) || path+"/" == ep
}

// APIHost reports whether a request to host at path (normalised) is one of m's API hosts: a
// hosts entry matches, and no public entry matches at least as precisely (public content of
// the vendor: fonts.googleapis.com, a public GCS object, an installer CDN).
func (m Match) APIHost(host, path string) bool {
	best := -1
	for _, e := range m.Hosts {
		if HostMatches(e, host, path) {
			if s := hostSpecificity(e); s > best {
				best = s
			}
		}
	}
	if best < 0 {
		return false
	}
	for _, e := range m.Public {
		if PublicMatches(e, host, path) && hostSpecificity(e) >= best {
			return false
		}
	}
	return true
}

// HostsOverlap reports whether two hosts entries can match one request: the same host, or
// one a subdomain of the other (path prefixes are ignored, so the answer errs towards yes).
func HostsOverlap(a, b string) bool {
	ah, _ := SplitHost(a)
	bh, _ := SplitHost(b)
	return ah == bh || strings.HasSuffix(ah, "."+bh) || strings.HasSuffix(bh, "."+ah)
}

// CmdletMatches reports whether a PowerShell command name matches a cmdlets entry: the exact
// name, or for `*-<Prefix>*` any Verb-Noun whose noun starts with the prefix. Case-insensitive.
func CmdletMatches(entry, name string) bool {
	e, n := strings.ToLower(entry), strings.ToLower(name)
	if !strings.HasPrefix(e, "*-") && !strings.HasSuffix(e, "*") {
		return e == n
	}
	ev, en, _ := strings.Cut(e, "-")
	nv, nn, ok := strings.Cut(n, "-")
	if !ok || nv == "" || nn == "" {
		return false
	}
	if ev != "*" && ev != nv {
		return false
	}
	if p, ok := strings.CutSuffix(en, "*"); ok {
		return strings.HasPrefix(nn, p)
	}
	return en == nn
}

// DottedCovers reports whether a dotted module entry covers name: the same module or a child
// of it (azure.cli covers azure.cli.core; AWS.Tools covers AWS.Tools.S3). Case-insensitive.
func DottedCovers(entry, name string) bool {
	e, n := strings.ToLower(entry), strings.ToLower(name)
	return n == e || strings.HasPrefix(n, e+".")
}

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
	for exe, subs := range m.Subcommands {
		if !matchExeRe.MatchString(exe) {
			return fmt.Errorf("subcommands: %q is not a valid executable name", exe)
		}
		if err := check("subcommands["+exe+"]", subs, matchSubRe); err != nil {
			return err
		}
	}
	if err := check("images", m.Images, matchImageRe); err != nil {
		return err
	}
	if err := check("modules", m.Modules, matchModuleRe); err != nil {
		return err
	}
	if err := check("psmodules", m.PSModules, matchPSModuleRe); err != nil {
		return err
	}
	if err := check("cmdlets", m.Cmdlets, matchCmdletRe); err != nil {
		return err
	}
	if err := check("env", m.Env, matchEnvRe); err != nil {
		return err
	}
	if err := check("hosts", m.Hosts, matchHostRe); err != nil {
		return err
	}
	if err := check("public", m.Public, matchHostRe); err != nil {
		return err
	}
	if err := check("git_hosts", m.GitHosts, matchGitHostRe); err != nil {
		return err
	}
	if err := check("mcp_hosts", m.MCPHosts, matchGitHostRe); err != nil {
		return err
	}
	if err := check("mcp_packages", m.MCPPackages, matchPkgRe); err != nil {
		return err
	}
	for _, p := range m.MCPPackages {
		if strings.Contains(p, "..") {
			return fmt.Errorf("mcp_packages: %q holds ..", p)
		}
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
// the embedded lists and never shortens them (ADR-0007). To stop the guard matching a
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
	return Match{
		Executables: u(m.Executables, o.Executables),
		Subcommands: subs,
		Images:      u(m.Images, o.Images),
		Modules:     u(m.Modules, o.Modules),
		PSModules:   u(m.PSModules, o.PSModules),
		Cmdlets:     u(m.Cmdlets, o.Cmdlets),
		Env:         u(m.Env, o.Env),
		Paths:       u(m.Paths, o.Paths),
		Hosts:       u(m.Hosts, o.Hosts),
		Public:      m.Public, // a user file never adds an exception: it only widens denial
		GitHosts:    u(m.GitHosts, o.GitHosts),
		MCP:         u(m.MCP, o.MCP),
		MCPPackages: u(m.MCPPackages, o.MCPPackages),
		MCPHosts:    u(m.MCPHosts, o.MCPHosts),
	}
}
