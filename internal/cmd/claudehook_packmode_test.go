package cmd

import (
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
	for id, c := range map[string]guardcfg.Category{"tq/plan-read": guardcfg.CatSecrets, "tq/plan-commit": guardcfg.CatSecrets, "tq/plan-print": guardcfg.CatPacks} {
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
	})
	t.Run("relaxed without a grant", func(t *testing.T) {
		_, proj := modeWS(t, cloudModeBlock+"guard:\n  mode: relaxed\n", "")
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, apply, ""))
		if code != 0 || !strings.Contains(out, "CONFIRM [tq/iac-apply-env]") {
			t.Fatalf("the ceiling clamps relaxed to default: %d %q", code, out)
		}
	})
}
