package guardcfg

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
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
