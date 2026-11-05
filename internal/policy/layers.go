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

// ScopeFor compiles identity's scope from the catalog with no path facts (no protected path
// set, no other identities): what the PATH, the environment and the MCP render need.
func ScopeFor(identity string, scope []string, provs []providers.Provider) *Scope {
	return NewScope(ScopeInputs{Identity: identity, Scope: scope, Providers: provs})
}

// Identity is the workspace name the scope was compiled for.
func (s *Scope) Identity() string { return s.identity }

// IDs are the sorted catalog ids in scope.
func (s *Scope) IDs() []string { return append([]string{}, s.scope...) }

// OutOfScopeExecutables returns the executable names (lower case, no Windows suffix) of the
// providers outside the scope that no in-scope provider also claims, each mapped to its
// provider's catalog id: what out-of-scope-cli denies by name.
func (s *Scope) OutOfScopeExecutables() map[string]string {
	out := make(map[string]string, len(s.outExe))
	for n, sp := range s.outExe {
		out[n] = sp.id
	}
	return out
}

// OutOfScopeExecutable reports whether a file or command name (a path, a Windows suffix and
// the case are ignored, as the hook reads a command word) is an out-of-scope executable, and
// whose.
func (s *Scope) OutOfScopeExecutable(name string) (id string, ok bool) {
	if sp := s.outExe[exeName(name)]; sp != nil {
		return sp.id, true
	}
	return "", false
}

// OutOfScopeEnv returns the names, out of names (the variables an environment holds), that
// are an out-of-scope provider's variables no in-scope provider claims, sorted and spelled as
// given. Names compare case-insensitively, as the catalog spells them upper case; a trailing
// * of a match entry is a prefix.
func (s *Scope) OutOfScopeEnv(names []string) []string {
	var out []string
	for _, n := range names {
		u := strings.ToUpper(n)
		if u == "" || s.inClaimsEnv(u) {
			continue
		}
		for _, e := range s.outEnv {
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
	h, hit := s.hostHit(strings.Trim(strings.ToLower(host), "."))
	if !hit {
		return "", false
	}
	return h.what, true
}

// URLHost is the host of a URL with a scheme (http, https, ws, wss, ftp, ftps), lower case,
// or "".
func URLHost(u string) string { return urlHost(u) }
