package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The hook's side of the identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0007): a
// trusted workspace has its provider scope compiled once per
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
	cwd  string
	s    *policy.Scope
}

// hookScope is the compiled scope of ws, or nil for no workspace (a neutral cwd) or an
// untrusted one (judged by the untrusted and neutral rules). A patch judges many files with the same ws and prot: the scope is
// compiled once for them.
func hookScope(ws *resolve.Workspace, cfg *registry.Config, prot *protect.Set, cwd string) *policy.Scope {
	if ws == nil || ws.Manifest == nil || prot == nil {
		return nil
	}
	if scopeMemo.ws == ws && scopeMemo.prot == prot && scopeMemo.cwd == cwd {
		return scopeMemo.s
	}
	roots, dirs := otherIdentities(ws, cfg)
	home, _ := os.UserHomeDir()
	// The merged catalog (embedded + user files) is what an out-of-scope provider DENIES; the
	// embedded match is what an in-scope provider CLAIMS, so a user file can never widen a
	// claim (ADR-0007). A user file that does not load is left out: its built-in entry (an
	// override) stays in force, and a new id the scope names was denied by scopeCatalogError.
	// The PATH, environment and MCP layers read the same (policy.CatalogInputs, ADR-0008).
	provs, embedded := policy.CatalogInputs()
	s := policy.NewScope(policy.ScopeInputs{
		Identity: ws.Name, Scope: ws.Manifest.Scope(), Providers: provs, Embedded: embedded,
		BundleMCP: ws.Manifest.BundleMCP(), MCPAllow: ws.Manifest.MCPAllow(), Set: prot, UserHome: home, Env: os.Getenv,
		OtherRoots: roots, OtherDirs: dirs,
		// An mcp__ call is judged by its server's entry in the identity's harness config too
		// (ADR-0008); read only on the first MCP call of the run.
		MCPEntries: func() map[string][]map[string]any {
			return mcpscope.Entries(paths.IdentityDir(ws.Name, "claude"), cwd, ws.Root)
		},
	})
	scopeMemo.ws, scopeMemo.prot, scopeMemo.cwd, scopeMemo.s = ws, prot, cwd, s
	return s
}

// scopeCatalogError judges the user provider files for an identity (ADR-0007 round
// 3). deny is why every call must be refused, or "": a file tq did not load whose id the
// identity's scope names (its scope: list, an identity, the git provider), or a provider
// folder tq cannot read. warnings name every other file that did not load: an override of a
// built-in id (the built-in lists stay in force) or a new id the scope does not name; work
// goes on. tq doctor and tq providers list report every bad file too.
// The deny goes through the same fail-closed path as an unreadable manifest
// (resolve.Result.Unreadable): one mechanism, never a silent skip of the bad file.
func scopeCatalogError(ws *resolve.Workspace) (deny string, warnings []string) {
	if ws == nil || ws.Manifest == nil {
		return "", nil
	}
	_, errs := providers.LoadAll()
	if len(errs) == 0 {
		return "", nil
	}
	named := map[string]bool{}
	for _, id := range ws.Manifest.Scope() {
		named[id] = true
	}
	for _, id := range ws.Manifest.ExtraScope {
		named[id] = true
	}
	for id := range ws.Manifest.Identities {
		named[id] = true
	}
	if gp := strings.ToLower(strings.TrimSpace(ws.Manifest.Git.Provider)); gp != "" {
		named[gp] = true
	}
	for _, e := range errs {
		var fe *providers.FileError
		if !errors.As(e, &fe) {
			return "the provider catalog of identity " + ws.Name + " cannot be read (" + e.Error() +
				"): tq cannot tell what this identity may touch, so it denies every call here until a human fixes ~/.tentaqles/providers", nil
		}
		switch {
		case !fe.Builtin && named[fe.ID]:
			return "identity " + ws.Name + " names provider " + fe.ID + ", whose file does not load: " + fe.Error() +
				". tq cannot tell what this identity may touch, so it denies every call here", nil
		default:
			// an override (the built-in lists apply) or a new id this scope does not name:
			// work goes on, and the developer is told on every answer (QA round 3, item 8)
			warnings = append(warnings, "tq: warning: provider file "+fe.Error())
		}
	}
	return "", warnings
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
