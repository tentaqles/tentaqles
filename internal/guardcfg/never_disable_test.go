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

// M4, since spec E: the never-disable ids are locked. guard.actions may only
// make them stricter, and they cannot be superseded.
func TestNeverDisableIgnoresActions(t *testing.T) {
	m := &manifest.Manifest{Guard: manifest.Guard{Actions: map[string]policy.Action{"tq/hub-store-write": policy.Ask, "tq/iac-apply": policy.Deny}}}
	e := Merge(File{Actions: map[string]policy.Action{"tq/human-answer": policy.Ask}}, nil, m, "acme")
	if _, ok := e.Actions["tq/hub-store-write"]; ok {
		t.Fatal("guard.actions softened tq/hub-store-write")
	}
	if _, ok := e.Actions["tq/human-answer"]; ok {
		t.Fatal("the global file softened tq/human-answer")
	}
	if e.Actions["tq/iac-apply"] != policy.Deny {
		t.Fatal("other ids keep their actions")
	}
	for _, id := range []string{"tq/hub-store-write", "tq/pack-config-change", "tq/human-answer"} {
		if Categories[id] != CatSelfProtection || !NeverDisable(id) {
			t.Fatalf("%s: %q, want locked self-protection", id, Categories[id])
		}
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
// or softens it: it is in the locked secrets group.
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
	if Categories[id] != CatSecrets {
		t.Fatalf("category %q, want secrets (locked)", Categories[id])
	}
}
