package guardcfg

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func TestNeverDisableIgnoresEveryLayer(t *testing.T) {
	m := &manifest.Manifest{Guard: manifest.Guard{Disable: []string{"tq/hub-store-write", "tq/iac-apply"}}}
	e := Merge(File{Disable: []string{"tq/hub-store-write"}}, nil, m, "acme")
	if e.Off("tq/hub-store-write") {
		t.Fatal("tq/hub-store-write must stay on")
	}
	for _, id := range e.Disable {
		if id == "tq/hub-store-write" {
			t.Fatal("listed as disabled")
		}
	}
	if !e.Off("tq/iac-apply") {
		t.Fatal("other ids still switch off")
	}
	if !NeverDisable("tq/hub-store-write") || NeverDisable("tq/iac-apply") {
		t.Fatal("NeverDisable")
	}
}

// M4: never-disable ids ignore guard.actions too, are always asked and
// cannot be superseded.
func TestNeverDisableIgnoresActions(t *testing.T) {
	m := &manifest.Manifest{Guard: manifest.Guard{Actions: map[string]policy.Action{"tq/hub-store-write": policy.Ask, "tq/iac-apply": policy.Deny}}}
	e := Merge(File{Actions: map[string]policy.Action{"tq/pack-config-change": policy.Deny}}, nil, m, "acme")
	if _, ok := e.Actions["tq/hub-store-write"]; ok {
		t.Fatal("guard.actions softened tq/hub-store-write")
	}
	if _, ok := e.Actions["tq/pack-config-change"]; ok {
		t.Fatal("the global file changed tq/pack-config-change")
	}
	if e.Actions["tq/iac-apply"] != policy.Deny {
		t.Fatal("other ids keep their actions")
	}
	if Skippable(&manifest.Manifest{}, []string{"tq/pack-config-change"}) || Skippable(&manifest.Manifest{}, []string{"tq/hub-store-write"}) {
		t.Fatal("unattended mode must never skip them")
	}
	for _, id := range []string{"tq/hub-store-write", "tq/secret-store-read", "tq/env-file-shell", "tq/guard-file-write", "tq/settings-widen"} {
		if !Unsupersedable(id) {
			t.Fatalf("%s supersedable", id)
		}
	}
	if Unsupersedable("tq/iac-apply") || Unsupersedable("tq/remote-migration") {
		t.Fatal("the spec D supersedes must stay possible")
	}
}

// Final review, security m4: tq/secret-store-shell guards the pack ledger
// signing key, the root of every pack approval, so no layer switches it off
// or softens it, and unattended mode always asks it.
func TestSecretStoreShellIsNeverDisable(t *testing.T) {
	const id = "tq/secret-store-shell"
	m := &manifest.Manifest{Guard: manifest.Guard{Disable: []string{id}, Actions: map[string]policy.Action{id: policy.Ask}}}
	e := Merge(File{Disable: []string{id}}, nil, m, "acme")
	if !NeverDisable(id) || e.Off(id) {
		t.Fatal("switched off")
	}
	if _, ok := e.Actions[id]; ok {
		t.Fatal("softened")
	}
	found := false
	for _, a := range AlwaysAsk {
		found = found || a == id
	}
	if !found {
		t.Fatal("not in AlwaysAsk")
	}
}
