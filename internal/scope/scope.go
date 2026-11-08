// Package scope compiles an identity's provider scope into what its terminal hides: the CLIs of
// providers outside the scope (masked on the identity PATH) and their variables (unset in the
// identity environment). It reads the catalog's match lists: executables, subcommands and env.
// An in-scope provider's claim always wins, and only the embedded catalog's claim counts for an
// in-scope provider, so a user provider file can widen what is hidden but never narrow it.
package scope

import (
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/providers"
)

type provider struct {
	id      string
	inScope bool
	generic bool // providers.Provider.GenericTool: its variables belong to the project
}

// exeClaim is one provider's claim on an executable: wholesale, or only some subcommands.
type exeClaim struct {
	p        *provider
	wildcard bool
}

type envName struct {
	name   string
	prefix bool
	p      *provider
}

// Scope is an identity's compiled scope.
type Scope struct {
	identity  string
	ids       []string
	exeClaims map[string][]exeClaim
	inEnv     []envName
	outEnv    []envName
}

// For compiles identity's scope (the manifest's Scope()) from the catalog.
func For(identity string, ids []string) *Scope {
	cat, _ := providers.LoadAll()
	embedded := map[string]providers.Match{}
	if ec, err := providers.Embedded(); err == nil {
		for _, p := range ec.All() {
			embedded[p.ID] = p.Match
		}
	}
	var provs []providers.Provider
	if cat != nil {
		provs = cat.All()
	}
	return compile(identity, ids, provs, embedded)
}

func compile(identity string, ids []string, provs []providers.Provider, embedded map[string]providers.Match) *Scope {
	s := &Scope{identity: identity, ids: append([]string{}, ids...), exeClaims: map[string][]exeClaim{}}
	sort.Strings(s.ids)
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	for _, pv := range provs {
		p := &provider{id: pv.ID, inScope: in[pv.ID], generic: pv.GenericTool()}
		claim := pv.Match
		if p.inScope {
			if em, ok := embedded[pv.ID]; ok {
				claim = em
			}
		}
		for _, e := range claim.Executables {
			n := exeName(e)
			s.exeClaims[n] = append(s.exeClaims[n], exeClaim{p: p, wildcard: true})
		}
		for exe := range claim.Subcommands {
			n := exeName(exe)
			s.exeClaims[n] = append(s.exeClaims[n], exeClaim{p: p})
		}
		for _, e := range claim.Env {
			n, prefix := strings.CutSuffix(strings.ToUpper(e), "*")
			en := envName{name: n, prefix: prefix, p: p}
			if p.inScope {
				s.inEnv = append(s.inEnv, en)
			} else {
				s.outEnv = append(s.outEnv, en)
			}
		}
	}
	return s
}

// OutOfScopeExecutables returns the executable names (lower case, no Windows suffix) that an
// out-of-scope provider claims wholesale and no in-scope provider claims at all (wholesale or by
// a subcommand: az stays when azure-devops is in scope), each mapped to that provider's id.
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

// OutOfScopeExecutable reports whether a file or command name (a path, a Windows suffix and the
// case ignored) is one OutOfScopeExecutables lists, and whose.
func (s *Scope) OutOfScopeExecutable(name string) (id string, ok bool) {
	for _, n := range exeCandidates(name) {
		if claims, found := s.exeClaims[n]; found {
			return wholeOut(claims)
		}
	}
	return "", false
}

// OutOfScopeEnv returns the names, out of names, that are an out-of-scope provider's variables no
// in-scope provider claims, sorted and spelled as given. Names compare case-insensitively; a
// trailing * of a match entry is a prefix. A generic tool's variables are never returned.
func (s *Scope) OutOfScopeEnv(names []string) []string {
	var out []string
	for _, n := range names {
		u := strings.ToUpper(n)
		if u == "" || s.inClaims(u) {
			continue
		}
		for _, e := range s.outEnv {
			if e.p.generic {
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

func (s *Scope) inClaims(name string) bool {
	for _, e := range s.inEnv {
		if e.name == name || (e.prefix && strings.HasPrefix(name, e.name)) {
			return true
		}
	}
	return false
}

// exeName is the executable a command word names: its last path element, quotes removed,
// lower-cased, without a Windows suffix.
func exeName(w string) string {
	n := guard.BaseName(strings.ReplaceAll(strings.Trim(w, `"'`), `\`, "/"))
	n = n[strings.LastIndex(n, "/")+1:]
	for _, suf := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		n = strings.TrimSuffix(n, suf)
	}
	return n
}

// exeCandidates are the names a word may resolve to: the basename, and the backslash-collapsed
// form (a\ws -> aws).
func exeCandidates(word string) []string {
	out := []string{exeName(word)}
	if strings.Contains(word, `\`) {
		if c := exeName(strings.ReplaceAll(word, `\`, "")); c != out[0] {
			out = append(out, c)
		}
	}
	return out
}
