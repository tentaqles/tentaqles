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
	s := policy.NewScope(policy.ScopeInputs{
		Identity: ws.Name, Scope: ws.Manifest.Scope(), Providers: providers.MustLoad().All(),
		Set: prot, UserHome: home, Env: os.Getenv, OtherRoots: roots, OtherDirs: dirs,
	})
	scopeMemo.ws, scopeMemo.prot, scopeMemo.s = ws, prot, s
	return s
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
