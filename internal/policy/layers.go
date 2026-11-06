package policy

import (
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// What the layers before the hook read from a compiled Scope (PRD v2 §6.2; ADR-0007): the
// identity PATH (layer 2) masks the executables, the identity environment unsets the
// variables, and the harness config render (layer 3) drops the MCP servers. They read the
// same match lists the hook judges by, with the same in-scope-wins rule, so a name an
// earlier layer hides is a name the hook denies, and a name the hook allows is never hidden.

// CatalogInputs are the catalog facts every compiled scope reads, the hook's and the layers'
// alike, so the two share one definition (ADR-0007, ADR-0008): the merged catalog for what is
// denied and the embedded-only match per id for what an in-scope provider claims (a user file
// can never widen a claim). A user file that does not load is left out (scopeCatalogError).
func CatalogInputs() (provs []providers.Provider, embedded map[string]providers.Match) {
	cat, _ := providers.LoadAll()
	embedded = map[string]providers.Match{}
	if ec, err := providers.Embedded(); err == nil {
		for _, p := range ec.All() {
			embedded[p.ID] = p.Match
		}
	}
	if cat != nil {
		provs = cat.All()
	}
	return provs, embedded
}

// ScopeFor compiles identity's scope from the catalog (CatalogInputs) with no path facts (no
// protected path set, no other identities): what the PATH, the environment and the MCP render
// need. bundleMCP is the manifest's claude.bundle.mcp (the exact-name rule), mcpAllow its
// mcp.allow (the hybrid MCP list).
func ScopeFor(identity string, scope, bundleMCP, mcpAllow []string) *Scope {
	provs, embedded := CatalogInputs()
	return NewScope(ScopeInputs{Identity: identity, Scope: scope, Providers: provs, Embedded: embedded, BundleMCP: bundleMCP, MCPAllow: mcpAllow})
}

// Identity is the workspace name the scope was compiled for.
func (s *Scope) Identity() string { return s.identity }

// IDs are the sorted catalog ids in scope.
func (s *Scope) IDs() []string { return append([]string{}, s.scope...) }

// OutOfScopeExecutables returns the executable names (lower case, no Windows suffix) that an
// out-of-scope provider claims wholesale and no in-scope provider claims at all (wholesale or
// by a subcommand: az stays when azure-devops is in scope), each mapped to that provider's
// catalog id: the names out-of-scope-cli denies in every form.
func (s *Scope) OutOfScopeExecutables() map[string]string {
	out := map[string]string{}
	for n, claims := range s.exeClaims {
		if id, ok := wholeOut(claims); ok {
			out[n] = id
		}
	}
	return out
}

func wholeOut(claims []exeClaim) (string, bool) {
	id := ""
	for _, c := range claims {
		if c.p.inScope {
			return "", false
		}
		if c.wildcard && id == "" {
			id = c.p.id
		}
	}
	return id, id != ""
}

// OutOfScopeExecutable reports whether a file or command name (a path, a Windows suffix and
// the case are ignored, as the hook reads a command word) is an out-of-scope executable by
// OutOfScopeExecutables' rule, and whose.
func (s *Scope) OutOfScopeExecutable(name string) (id string, ok bool) {
	for _, n := range exeCandidates(name) {
		if claims, found := s.exeClaims[n]; found {
			return wholeOut(claims)
		}
	}
	return "", false
}

// OutOfScopeEnv returns the names, out of names (the variables an environment holds), that
// are an out-of-scope provider's variables no in-scope provider claims, sorted and spelled as
// given. Names compare case-insensitively, as the catalog spells them upper case; a trailing
// * of a match entry is a prefix. A generic tool's variables (providers.GenericTool: NPM_TOKEN,
// TF_TOKEN_*) are never returned: they belong to the project.
func (s *Scope) OutOfScopeEnv(names []string) []string {
	var out []string
	for _, n := range names {
		u := strings.ToUpper(n)
		if u == "" || s.inClaimsEnv(u) {
			continue
		}
		for _, e := range s.outEnv {
			if e.p != nil && e.p.generic {
				continue
			}
			if (!e.prefix && u == e.name) || (e.prefix && strings.HasPrefix(u, e.name) && len(u) > len(e.name)) {
				out = append(out, n)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// OutOfScopeMCPServer reports whether an MCP server name is an out-of-scope provider's, the
// rule out-of-scope-mcp applies to an mcp__<server>__<tool> call: one of its tokens (split
// on _ - .) is an out-of-scope provider's and none is an in-scope provider's. what names the
// provider.
func (s *Scope) OutOfScopeMCPServer(name string) (what string, ok bool) {
	h, hit := s.mcpServer(name)
	if !hit {
		return "", false
	}
	return h.what, true
}

// OutOfScopeHost reports whether host is an out-of-scope provider's API host (the host or a
// parent domain listed) that no in-scope provider lists. what names the provider.
func (s *Scope) OutOfScopeHost(host string) (what string, ok bool) {
	h, hit := s.hostHit(strings.Trim(strings.ToLower(host), "."), "")
	if !hit {
		return "", false
	}
	return h.what, true
}
