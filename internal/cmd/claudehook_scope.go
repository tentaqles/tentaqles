package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The hook's side of the identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0007): a
// trusted workspace whose manifest sets guard: v2 has its provider scope compiled once per
// hook run from the catalog (loaded once per process) and judged before the secret rules.

// scopeNote is what an identity deny adds after its reason (PRD v2 §6.1 step 5, §7.3): the
// identity's provider scope (Manifest.Scope(), PRD v2 §6), so the model reports the mistake
// instead of retrying. It is the one place a deny message reads the scope.
func scopeNote(ws *resolve.Workspace) string {
	if ws == nil || ws.Manifest == nil {
		return "\nScope: no trusted identity here; report this to the developer instead of retrying."
	}
	return "\nScope of identity " + ws.Name + ": " + identityScope(ws.Manifest) +
		". Anything outside it is denied; report this to the developer instead of retrying."
}

// identityScope is the identity's provider scope as a deny prints it: the catalog ids of
// Manifest.Scope(), or "none".
func identityScope(m *manifest.Manifest) string {
	ids := m.Scope()
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

var scopeMemo struct {
	ws   *resolve.Workspace
	prot *protect.Set
	s    *policy.Scope
}

// hookScope is the compiled scope of ws, or nil when the v1 rules apply: no workspace (a
// neutral cwd), an untrusted one (judged by the untrusted and neutral rules), or a manifest
// without guard: v2. A patch judges many files with the same ws and prot: the scope is
// compiled once for them.
func hookScope(ws *resolve.Workspace, cfg *registry.Config, prot *protect.Set) *policy.Scope {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.GuardV2() || prot == nil {
		return nil
	}
	if scopeMemo.ws == ws && scopeMemo.prot == prot {
		return scopeMemo.s
	}
	roots, dirs := otherIdentities(ws, cfg)
	home, _ := os.UserHomeDir()
	// Merged catalog for out-of-scope denials; a bad user file fails closed (LoadErr). The
	// embedded-only match per id is what an in-scope provider CLAIMS, so a user file can
	// never widen a claim (ADR-0006 round 2).
	cat, cerrs := providers.LoadAll()
	loadErr := ""
	if len(cerrs) > 0 {
		loadErr = cerrs[0].Error()
	}
	embedded := map[string]providers.Match{}
	if ec, err := providers.Embedded(); err == nil {
		for _, p := range ec.All() {
			embedded[p.ID] = p.Match
		}
	}
	var bundleMCP []string
	if ws.Manifest.Claude.Bundle != nil {
		bundleMCP = ws.Manifest.Claude.Bundle.MCP
	}
	s := policy.NewScope(policy.ScopeInputs{
		Identity: ws.Name, Scope: ws.Manifest.Scope(), Providers: cat.All(), Embedded: embedded,
		BundleMCP: bundleMCP, Set: prot, UserHome: home, Env: os.Getenv,
		OtherRoots: roots, OtherDirs: dirs, LoadErr: loadErr,
	})
	scopeMemo.ws, scopeMemo.prot, scopeMemo.s = ws, prot, s
	return s
}

// unreadableManifest reports the workspace whose marker encloses cwd but whose protected
// manifest fails to load (guard: v3, guard: true, an unknown scope id, bad YAML). The hook
// then fails closed and denies every tool in that folder (ADR-0006 round 2; the sibling
// P2.2 branch owns the general unreadable-manifest rule). "" error means readable or no
// enclosing workspace.
func unreadableManifest(cwd string, cfg *registry.Config) (string, error) {
	if cfg == nil || strings.TrimSpace(cwd) == "" {
		return "", nil
	}
	cwdc := filepath.Clean(cwd)
	for _, base := range cfg.Bases {
		b := filepath.Clean(base)
		rel, err := filepath.Rel(b, cwdc)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		name := rel
		if i := strings.IndexAny(rel, `/\`); i >= 0 {
			name = rel[:i]
		}
		if name == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(b, name, manifest.FileName)); err != nil {
			continue // no marker: not a workspace folder
		}
		mp := paths.ManifestPath(name)
		if _, err := os.Stat(mp); err != nil {
			continue // no protected manifest to read (unmigrated/old layout): other rules judge it
		}
		if _, err := manifest.Load(mp); err != nil {
			return name, err
		}
	}
	return "", nil
}

// otherIdentities names every identity but ws: the folders under ~/.tentaqles/identities and
// the workspace folders (a child of a registered base holding the marker). It returns their
// workspace roots under every base and their identity folders. One directory read per base,
// one stat per base child.
func otherIdentities(ws *resolve.Workspace, cfg *registry.Config) (roots, dirs map[string]string) {
	names := map[string]bool{}
	if es, err := os.ReadDir(paths.IdentitiesRoot()); err == nil {
		for _, e := range es {
			if e.IsDir() {
				names[e.Name()] = true
			}
		}
	}
	var bases []string
	if cfg != nil {
		bases = cfg.Bases
	}
	for _, b := range bases {
		es, err := os.ReadDir(b)
		if err != nil {
			continue
		}
		for _, e := range es {
			if e.IsDir() {
				if _, err := os.Stat(filepath.Join(b, e.Name(), manifest.FileName)); err == nil {
					names[e.Name()] = true
				}
			}
		}
	}
	delete(names, ws.Name)
	roots, dirs = map[string]string{}, map[string]string{}
	for n := range names {
		for _, b := range bases {
			roots[filepath.Join(b, n)] = n
		}
		dirs[filepath.Join(paths.IdentitiesRoot(), n)] = n
	}
	return roots, dirs
}
