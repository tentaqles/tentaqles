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
	// The merged catalog (embedded + user files) is what an out-of-scope provider DENIES; the
	// embedded match is what an in-scope provider CLAIMS, so a user file can never widen a
	// claim (ADR-0007). A bad user file never reaches here: scopeCatalogError denied first.
	cat, _ := providers.LoadAll()
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
		OtherRoots: roots, OtherDirs: dirs,
	})
	scopeMemo.ws, scopeMemo.prot, scopeMemo.s = ws, prot, s
	return s
}

// scopeCatalogError is why a guard: v2 identity's provider catalog cannot be used (a user
// provider file that does not load), or "". pre-tool-use denies every call on it through the
// same fail-closed path as an unreadable manifest (resolve.Result.Unreadable): one
// mechanism, never a silent skip of the bad file (ADR-0007).
func scopeCatalogError(ws *resolve.Workspace) string {
	if ws == nil || ws.Manifest == nil || !ws.Manifest.GuardV2() {
		return ""
	}
	if _, errs := providers.LoadAll(); len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return "the provider catalog of identity " + ws.Name + " cannot be read (" + strings.Join(msgs, "; ") +
			"): tq cannot tell what this identity may touch, so it denies every call here until a human fixes or removes the file under ~/.tentaqles/providers"
	}
	return ""
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
