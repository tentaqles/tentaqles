package guardcfg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func TestMergePrecedence(t *testing.T) {
	g := File{
		Disable: []string{"tq/cloud-delete", "tq/force-push"},
		Actions: map[string]policy.Action{"tq/gh-admin-merge": policy.Deny, "tq/registry-write": policy.Deny},
	}
	m := &manifest.Manifest{}
	m.Guard.Enable = []string{"tq/force-push", "jev/rls-weakened"}
	m.Guard.Disable = []string{"tq/iac-destroy"}
	m.Guard.Actions = map[string]policy.Action{"tq/registry-write": policy.Ask}
	m.Decision.Disable = []string{"jev/rls-weakened", "jev/destructive-sql"}

	e := Merge(g, nil, m, "acme")
	for id, off := range map[string]bool{
		"tq/cloud-delete":     true,  // global
		"tq/force-push":       false, // global off, manifest enable wins
		"tq/iac-destroy":      true,  // manifest
		"jev/destructive-sql": true,  // decision.disable
		"jev/rls-weakened":    false, // decision.disable, but enable wins
		"tq/env-dump":         false,
	} {
		if e.Off(id) != off {
			t.Errorf("%s off=%v, want %v", id, e.Off(id), off)
		}
	}
	if e.DisableOrigin["tq/cloud-delete"] != "global" || e.DisableOrigin["tq/iac-destroy"] != "acme" {
		t.Errorf("origins = %v", e.DisableOrigin)
	}
	if e.ActionFor("tq/gh-admin-merge", policy.Ask) != policy.Deny || e.ActionFor("tq/registry-write", policy.Deny) != policy.Ask {
		t.Errorf("actions = %v (manifest must win over global)", e.Actions)
	}
	if e.ActionFor("tq/env-dump", policy.Deny) != policy.Deny {
		t.Error("default action must pass through")
	}
}

func TestBrokenGlobalFileAppliesNothing(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	os.WriteFile(Path(), []byte("disable: [tq/cloud-delete]\nactions: {tq/env-dump: allow}\n"), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("an invalid action must not load")
	}
	e := Resolve(nil, "")
	if e.GlobalErr == nil || e.Off("tq/cloud-delete") {
		t.Fatalf("a broken global file must apply nothing: %+v", e)
	}
	os.WriteFile(Path(), []byte("disable: [unterminated\n"), 0o600)
	if e := Resolve(nil, ""); e.GlobalErr == nil || len(e.Disable) != 0 {
		t.Fatalf("unparseable file must apply nothing: %+v", e)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if f, err := Load(); err != nil || len(f.Disable) != 0 {
		t.Fatalf("missing file: %+v %v", f, err)
	}
	in := File{Disable: []string{"tq/force-push", "tq/cloud-delete", "tq/force-push"}, Actions: map[string]policy.Action{"tq/gh-admin-merge": policy.Deny}}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil || len(out.Disable) != 2 || out.Disable[0] != "tq/cloud-delete" || out.Actions["tq/gh-admin-merge"] != policy.Deny {
		t.Fatalf("round trip: %+v %v", out, err)
	}
	if err := Save(File{Actions: map[string]policy.Action{"x": "allow"}}); err == nil {
		t.Fatal("allow is not a valid action")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(Path()), FileName+".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file left behind")
	}
}

func TestKnownCoversEveryKind(t *testing.T) {
	m := &manifest.Manifest{}
	m.Guard.Rules = []policy.Rule{{ID: "acme/x", Action: policy.Deny}}
	known := Known(m, File{Rules: []policy.Rule{{ID: "me/y", Action: policy.Ask}}})
	for _, id := range []string{"tq/cloud-delete", "tq/commit-secret", "tq/guard-change", "tq/guard-file-write", "jev/destructive-sql", "acme/x", "me/y"} {
		if _, ok := Lookup(id, known); !ok {
			t.Errorf("%s not known", id)
		}
	}
	seen := map[string]int{}
	for _, r := range known {
		seen[r.ID]++
	}
	if seen["tq/guard-file-write"] != 1 {
		t.Error("an id spanning several rules must be listed once")
	}
}
