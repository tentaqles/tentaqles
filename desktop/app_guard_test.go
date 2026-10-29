package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// guardWS makes a trusted workspace acme under a registered base.
func guardWS(t *testing.T, body string, dirs ...string) string {
	t.Helper()
	isolateHome(t)
	base := t.TempDir()
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "acme")
	for _, d := range append([]string{""}, dirs...) {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	return root
}

func guardOf(t *testing.T, root string) manifest.Guard {
	t.Helper()
	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return m.Guard
}

func TestGuardWorkspacesBinding(t *testing.T) {
	guardWS(t, "guard:\n  mode: relaxed\n")
	a := NewApp()
	views, err := a.GuardWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Workspace != "acme" || len(views[0].Categories) != 13 {
		t.Fatalf("views = %+v", views)
	}
	locked := 0
	for _, c := range views[0].Categories {
		if c.Locked {
			locked++
		}
		if c.Name == "privacy" && !c.Locked {
			t.Fatal("privacy must be locked")
		}
	}
	if locked != 4 || !views[0].NeedsGrant || views[0].Proposal != "relaxed" {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestGuardSetModeToggleOverrideBindings(t *testing.T) {
	root := guardWS(t, "", "demo-todo")
	a := NewApp()
	ch, err := a.SetMode("acme", "workspace", "relaxed", "")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Looser || ch.Proposal != "relaxed" || guardOf(t, root).Mode != "relaxed" {
		t.Fatalf("change = %+v", ch)
	}
	if _, err := a.ToggleCategory("acme", "workspace", "privacy", "skip"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("privacy is locked: %v", err)
	}
	if _, err := a.ToggleCategory("acme", "workspace", "db-destructive", "ask"); err != nil {
		t.Fatal(err)
	}
	if g := guardOf(t, root); g.Mode != "custom" || g.Base != "relaxed" {
		t.Fatalf("guard = %+v", g)
	}
	if _, err := a.SetOverride("acme", "project:demo-todo", "tq/force-push", "deny"); err != nil {
		t.Fatal(err)
	}
	if guardOf(t, root).Projects["demo-todo"].RulesOverride["tq/force-push"] != "deny" {
		t.Fatalf("guard = %+v", guardOf(t, root))
	}
	if _, err := a.SetMode("acme", "workspace", "off", ""); err == nil {
		t.Fatal("off is never stored")
	}
	if _, err := a.SetMode("acme", "repo", "strict", ""); err == nil {
		t.Fatal("unknown scope")
	}
}

func TestGuardGrantAndRevokeBindings(t *testing.T) {
	root := guardWS(t, "guard:\n  mode: relaxed\n")
	a := NewApp()
	mode, err := a.GrantMode("acme")
	if err != nil || mode != "relaxed" {
		t.Fatalf("grant: %q %v", mode, err)
	}
	h, _ := trust.HashFile(filepath.Join(root, manifest.FileName))
	if g, _ := trust.LoadGrant(h); g == nil {
		t.Fatal("no grant written")
	}
	views, _ := a.GuardWorkspaces()
	if views[0].NeedsGrant {
		t.Fatal("granted")
	}
	if err := a.RevokeGrant("acme"); err != nil {
		t.Fatal(err)
	}
	if g, _ := trust.LoadGrant(h); g != nil {
		t.Fatal("grant must go")
	}
	if _, err := a.GrantMode("nope"); err == nil {
		t.Fatal("unknown workspace")
	}
}

func TestGuardGlobalScopeBinding(t *testing.T) {
	isolateHome(t)
	a := NewApp()
	ch, err := a.SetMode("", "global", "strict", "")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Scope != "global" || ch.Looser {
		t.Fatalf("change = %+v", ch)
	}
}
