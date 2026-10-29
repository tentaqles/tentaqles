package cmd

import (
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// Packs merge with guard modes (spec E §14): every shipped pack guard
// declares the category guardcfg.Categories gives its id. Cloud apply and
// destroy are cloud-destructive, db migrate and restore db-destructive, the
// identity drift guard stays identity (locked), the rest are packs.
func TestPackGuardCategories(t *testing.T) {
	want := map[string]guardcfg.Category{
		"tq/iac-apply-env":      guardcfg.CatCloudDestructive,
		"tq/iac-identity-drift": guardcfg.CatIdentity,
		"tq/db-migrate-env":     guardcfg.CatDBDestructive,
		"tq/restore-target":     guardcfg.CatDBDestructive,
	}
	seen := map[string]bool{}
	for _, g := range packs.Guards() {
		if strings.HasPrefix(g.ID, "tq/example-") || strings.HasPrefix(g.ID, "tq/x-") {
			continue // test guards
		}
		c, ok := guardcfg.Categories[g.ID]
		if !ok || c != g.Category {
			t.Errorf("%s: Category %q, table %q", g.ID, g.Category, c)
		}
		if w, ok := want[g.ID]; ok {
			seen[g.ID] = true
			if g.Category != w {
				t.Errorf("%s: %q, want %q", g.ID, g.Category, w)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("guards missing: %v", seen)
	}
	// the cloud pack's static rules
	for id, c := range map[string]guardcfg.Category{"tq/plan-read": guardcfg.CatSecrets, "tq/plan-commit": guardcfg.CatSecrets, "tq/plan-secret-print": guardcfg.CatSecrets, "tq/plan-print": guardcfg.CatPacks} {
		if guardcfg.Categories[id] != c {
			t.Errorf("%s: %q, want %q", id, guardcfg.Categories[id], c)
		}
	}
}

// cloudModeBlock: the cloud pack with one environment, prod, whose apply
// asks; every target resolves to it.
const cloudModeBlock = "packs:\n  cloud:\n    enabled: true\n    default_environment: prod\n    environments:\n      prod:\n        match: {workspaces: [prod]}\n        apply: ask\n"

// A pack guard's verdict goes through the guard mode: default asks, strict
// denies (cloud-destructive), relaxed with a grant skips it; a locked pack
// rule (tq/plan-read, secrets) still denies in relaxed.
func TestPackGuardUnderModes(t *testing.T) {
	const apply = "cdk deploy" // the cmd test binary also has example guards on terraform, tofu and pulumi
	t.Run("default", func(t *testing.T) {
		_, proj := modeWS(t, cloudModeBlock, "")
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, apply, ""))
		if code != 0 || !strings.Contains(out, "CONFIRM [tq/iac-apply-env]") {
			t.Fatalf("default: want an ask by tq/iac-apply-env, got %d %q %q", code, out, errOut)
		}
	})
	t.Run("strict", func(t *testing.T) {
		_, proj := modeWS(t, cloudModeBlock+"guard:\n  mode: strict\n", "")
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, apply, ""))
		if code != 2 || !strings.Contains(errOut, "tq/iac-apply-env") {
			t.Fatalf("strict: want a deny by tq/iac-apply-env, got %d %q", code, errOut)
		}
	})
	t.Run("relaxed", func(t *testing.T) {
		_, proj := modeWS(t, cloudModeBlock+"guard:\n  mode: relaxed\n", "relaxed")
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, apply, ""))
		silentAllow(t, "relaxed apply", code, out)
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "cat infra/prod.tfstate", ""))
		if code != 2 || !strings.Contains(errOut, "tq/plan-read") {
			t.Fatalf("relaxed: the locked tq/plan-read must still deny, got %d %q", code, errOut)
		}
		// r2-M1: the secret-printing forms keep their ask (locked secrets);
		// plain terraform output (sensitive values masked) is skipped
		for _, c := range []string{"terraform state pull", "terraform output -json", "pulumi stack output --show-secrets"} {
			code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, c, ""))
			if code != 0 || !strings.Contains(out, "CONFIRM [tq/plan-secret-print]") {
				t.Fatalf("relaxed %q: want an ask by tq/plan-secret-print, got %d %q", c, code, out)
			}
		}
		code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "terraform output", ""))
		silentAllow(t, "relaxed terraform output", code, out)
	})
	t.Run("relaxed without a grant", func(t *testing.T) {
		_, proj := modeWS(t, cloudModeBlock+"guard:\n  mode: relaxed\n", "")
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, apply, ""))
		if code != 0 || !strings.Contains(out, "CONFIRM [tq/iac-apply-env]") {
			t.Fatalf("the ceiling clamps relaxed to default: %d %q", code, out)
		}
	})
}

// r2-m2: a pack guard that supersedes a core rule is mapped at the stricter
// of its own category and the superseded rule's under the session's table.
// A custom table that skips cloud-destructive (tq/iac-apply-env) but keeps
// prod-changes (tq/iac-apply, which the gate supersedes) at ask still asks
// for a prod apply.
func TestSupersedingGuardKeepsTheStricterCategory(t *testing.T) {
	const apply = "cdk deploy"
	table := map[string]string{"cloud-destructive": "skip", "prod-changes": "ask"}
	_, proj := modeWS(t, cloudModeBlock+"guard:\n  mode: custom\n  base: default\n  categories: {cloud-destructive: skip, prod-changes: ask}\n", "")
	h, err := trust.HashFile(filepath.Join(filepath.Dir(proj), manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.SaveGrant(h, trust.Grant{Mode: "custom", Workspace: "acme", Table: table}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, apply, ""))
	if code != 0 || !strings.Contains(out, "CONFIRM [tq/iac-apply-env]") {
		t.Fatalf("custom cloud-destructive skip, prod-changes ask: want an ask, got %d %q %q", code, out, errOut)
	}
	// both skipped: allowed (the same as relaxed)
	_, proj = modeWS(t, cloudModeBlock+"guard:\n  mode: custom\n  base: default\n  categories: {cloud-destructive: skip, prod-changes: skip}\n", "")
	h, _ = trust.HashFile(filepath.Join(filepath.Dir(proj), manifest.FileName))
	if err := trust.SaveGrant(h, trust.Grant{Mode: "custom", Workspace: "acme", Table: map[string]string{"cloud-destructive": "skip", "prod-changes": "skip"}}); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, apply, ""))
	silentAllow(t, "both skipped", code, out)
}

// r2-m2, presets unchanged: in strict, tq/db-migrate-env (db-destructive:
// rule) keeps its own ask, although it supersedes tq/remote-migration
// (prod-changes: deny in strict); the lift only restores a verdict the mode
// lowered.
func TestSupersedingGuardStrictPresetUnchanged(t *testing.T) {
	t.Setenv("SUPABASE_PROJECT_ID", "")
	block := "packs:\n  db:\n    enabled: true\n    migrations:\n      - {path: supabase/migrations, engine: postgres, migrator: supabase}\n    default_environment: prod\n    environments:\n      prod: {match: {project_refs: [aaaaaaaaaaaaaaaaaaaa]}, migrate: ask}\n"
	_, proj := modeWS(t, block+"guard:\n  mode: strict\n", "")
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, "supabase db push --linked", ""))
	if code != 0 || !strings.Contains(out, "CONFIRM [tq/db-migrate-env]") {
		t.Fatalf("strict db migrate: want the gate's own ask, got %d %q %q", code, out, errOut)
	}
}
