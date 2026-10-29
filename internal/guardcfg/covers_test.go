package guardcfg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func builtinIDs() map[string]bool {
	ids := map[string]bool{GuardErrorID: true}
	for _, r := range policy.Builtin() {
		ids[r.ID] = true
	}
	for id := range HookRules {
		ids[id] = true
	}
	for _, r := range decide.BuiltinJudgeRules() {
		ids[r.ID] = true
	}
	return ids
}

func TestUnknownOverrides(t *testing.T) {
	ka := []string{"tq/iac-apply", "tq/nope"}
	m := &manifest.Manifest{Client: "acme"}
	m.Guard.Unattended = &manifest.Unattended{Workspace: true, KeepAsking: &ka}
	if got := UnknownOverrides(m, File{RulesOverride: map[string]Value{"me/x": ValueAsk, "tq/iac-destroy": ValueAsk}}); !reflect.DeepEqual(got, []string{"me/x", "tq/nope"}) {
		t.Fatalf("UnknownOverrides: %v", got)
	}
}

// A grant is keyed by the manifest hash and the workspace root together: a
// byte copy of a granted manifest in another workspace hashes the same but
// gets no grant (security review m4). A grant with no root (made before
// roots were recorded) still needs the same workspace name.
func TestWorkspaceGrantNeedsTheSameRoot(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	acme := filepath.Join(t.TempDir(), "acme")
	beta := filepath.Join(t.TempDir(), "beta")
	m := &manifest.Manifest{Client: "acme"}
	gr, ok := GrantFor(m, "acme", acme, File{}, ModeRelaxed)
	if !ok || gr.Root != acme || gr.Workspace != "acme" {
		t.Fatalf("GrantFor records the workspace and root: %+v", gr)
	}
	if err := trust.SaveGrant("h1", gr); err != nil {
		t.Fatal(err)
	}
	if WorkspaceGrant("h1", "acme", acme) == nil {
		t.Fatal("the workspace the grant was made for has it")
	}
	if WorkspaceGrant("h1", "beta", beta) != nil || WorkspaceGrant("h1", "acme", beta) != nil {
		t.Fatal("a copied manifest at another root gets no grant")
	}
	if err := trust.SaveGrant("h2", trust.Grant{Mode: ModeRelaxed, Workspace: "acme"}); err != nil {
		t.Fatal(err)
	}
	if WorkspaceGrant("h2", "acme", acme) == nil || WorkspaceGrant("h2", "beta", beta) != nil {
		t.Fatal("a grant with no root is pinned to its workspace name")
	}
	// The carry keeps the root and names the workspace.
	m2 := &manifest.Manifest{Client: "acme"}
	if _, carried, err := CarryGrant("h1", "h3", m2, "acme", acme, File{}); err != nil || !carried {
		t.Fatalf("carry: %v %v", carried, err)
	}
	if g := WorkspaceGrant("h3", "acme", acme); g == nil || g.Root != acme {
		t.Fatalf("carried grant: %+v", g)
	}
	if _, carried, _ := CarryGrant("h1", "h4", m2, "beta", beta, File{}); carried {
		t.Fatal("another root's grant is never carried")
	}
}

// bypassWorkspaces registers a base with the named workspaces, each holding
// the same manifest bytes, and files a legacy .bypass for that hash.
func bypassWorkspaces(t *testing.T, names ...string) (hash string, roots []string) {
	t.Helper()
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n")
	for _, n := range names {
		root := filepath.Join(base, n)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		mp := filepath.Join(root, manifest.FileName)
		if err := os.WriteFile(mp, body, 0o600); err != nil {
			t.Fatal(err)
		}
		h, err := trust.HashFile(mp)
		if err != nil {
			t.Fatal(err)
		}
		hash = h
		roots = append(roots, root)
	}
	if err := trust.Allow(hash); err != nil {
		t.Fatal(err)
	}
	if err := trust.AllowBypass(hash); err != nil {
		t.Fatal(err)
	}
	return hash, roots
}

// A legacy .bypass grant is keyed by the manifest hash only, so it counts
// only when exactly one registered workspace has that hash: a byte copy of
// the manifest elsewhere voids it for both, and neither can re-pin it
// (security re-review r2, concern 1).
func TestLegacyBypassNeedsOneOwner(t *testing.T) {
	h, roots := bypassWorkspaces(t, "acme")
	if g := WorkspaceGrant(h, "acme", roots[0]); g == nil || !g.FromBypass {
		t.Fatalf("the one owner reads the legacy grant: %+v", g)
	}
	if !BypassAllowed(h, "acme", roots[0]) {
		t.Fatal("BypassAllowed follows WorkspaceGrant")
	}
	if o := BypassOwners(h); len(o) != 1 {
		t.Fatalf("owners = %v", o)
	}

	h, roots = bypassWorkspaces(t, "acme", "copy")
	for i, n := range []string{"acme", "copy"} {
		if g := WorkspaceGrant(h, n, roots[i]); g != nil {
			t.Fatalf("%s: a .bypass shared by two workspaces is no grant: %+v", n, g)
		}
		if BypassAllowed(h, n, roots[i]) {
			t.Fatalf("%s: no bypass for a shared hash", n)
		}
	}
	if o := BypassOwners(h); len(o) != 2 {
		t.Fatalf("owners = %v", o)
	}
}

// BypassAllowed is root-checked like every grant (security re-review r2,
// concern 2): a .mode grant made for another root allows nothing.
func TestBypassAllowedIsRootChecked(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	acme := filepath.Join(t.TempDir(), "acme")
	gr, _ := GrantFor(&manifest.Manifest{Client: "acme"}, "acme", acme, File{}, ModeRelaxed)
	if err := trust.SaveGrant("h9", gr); err != nil {
		t.Fatal(err)
	}
	if !BypassAllowed("h9", "acme", acme) || BypassAllowed("h9", "beta", filepath.Join(t.TempDir(), "beta")) {
		t.Fatal("BypassAllowed must check the root")
	}
}
