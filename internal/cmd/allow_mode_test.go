package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

var agentMarkers = []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"}

// asHuman clears the agent markers (the test may run inside Claude Code)
// and makes stdin and stdout look like a terminal.
func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range agentMarkers {
		t.Setenv(k, "")
	}
	in, out := stdinIsTerminal, stdoutIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return true }
	stdoutIsTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = in, out })
}

func manifestHash(t *testing.T, root string) string {
	t.Helper()
	h, err := trust.HashFile(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAllowModeIsHumanOnly(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", relaxedWorkspace)
	for _, k := range agentMarkers {
		t.Setenv(k, "")
	}
	for _, args := range [][]string{{"allow", "--mode", "acme", "relaxed"}, {"allow", "--bypass", "acme"}, {"allow", "--mode", "--global", "relaxed"}} {
		if _, err := runGuardCmd(t, args...); !errors.Is(err, runner.ErrHumanOnly) {
			t.Fatalf("%v without a terminal: %v", args, err)
		}
	}
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
	if _, err := runGuardCmd(t, "allow", "--mode", "acme", "relaxed"); !errors.Is(err, runner.ErrHumanOnly) {
		t.Fatalf("an agent session is refused: %v", err)
	}
}

func TestAllowModeGrantsAndRemoves(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", relaxedWorkspace)
	asHuman(t)
	h := manifestHash(t, root)
	out, err := runGuardCmd(t, "allow", "--mode", "acme", "relaxed")
	if err != nil || !strings.Contains(out, "Granted relaxed for acme") {
		t.Fatalf("grant: %v %q", err, out)
	}
	g, _ := trust.LoadGrant(h)
	if g == nil || g.Mode != "relaxed" || g.Workspace != "acme" || g.Table["system"] != "ask" || g.Table["db-destructive"] != "skip" {
		t.Fatalf("stored grant: %+v", g)
	}
	if res := resolveMode(wsFor(t, root), root, ""); res.Mode != guardcfg.ModeRelaxed {
		t.Fatalf("the granted workspace runs relaxed: %+v", res)
	}
	for _, bad := range []string{"strict", "off", "loose"} {
		if _, err := runGuardCmd(t, "allow", "--mode", "acme", bad); err == nil {
			t.Fatalf("%s is not grantable", bad)
		}
	}
	if out, err := runGuardCmd(t, "allow", "--mode", "acme", "default"); err != nil || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %v %q", err, out)
	}
	if g, _ := trust.LoadGrant(h); g != nil {
		t.Fatalf("grant must be gone: %+v", g)
	}
	out, err = runGuardCmd(t, "allow", "--bypass", "acme")
	if err != nil || !strings.Contains(out, "deprecated: use tq allow --mode acme relaxed") {
		t.Fatalf("--bypass alias: %v %q", err, out)
	}
	if g, _ := trust.LoadGrant(h); g == nil || g.Mode != "relaxed" {
		t.Fatalf("--bypass writes the relaxed grant: %+v", g)
	}
	if _, err := runGuardCmd(t, "deny", "acme"); err != nil {
		t.Fatal(err)
	}
	if g, _ := trust.LoadGrant(h); g != nil {
		t.Fatal("tq deny removes the grant")
	}
}

// A hand edit plus a reflex tq allow drops the grant, and tq says so.
func TestPlainAllowAfterEditDropsTheGrant(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", relaxedWorkspace)
	asHuman(t)
	if _, err := runGuardCmd(t, "allow", "--mode", "acme", "relaxed"); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	b, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, append(b, "language: en\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runGuardCmd(t, "allow", "acme")
	if err != nil || !strings.Contains(out, "mode grant dropped (was relaxed): run tq allow --mode acme relaxed") {
		t.Fatalf("allow after edit: %v %q", err, out)
	}
	if res := resolveMode(wsFor(t, root), root, ""); res.Mode != guardcfg.ModeDefault || res.Requested != guardcfg.ModeRelaxed {
		t.Fatalf("no grant: default: %+v", res)
	}
	if out, _ := runGuardCmd(t, "allow", "acme"); strings.Contains(out, "dropped") {
		t.Fatalf("said once: %q", out)
	}
}

func TestAllowModeGlobal(t *testing.T) {
	isolateHome(t)
	asHuman(t)
	if err := guardcfg.Save(guardcfg.File{Mode: guardcfg.ModeRelaxed}); err != nil {
		t.Fatal(err)
	}
	if out, err := runGuardCmd(t, "allow", "--mode", "--global", "relaxed"); err != nil || !strings.Contains(out, "Granted relaxed") {
		t.Fatalf("global: %v %q", err, out)
	}
	if res := resolveMode(nil, t.TempDir(), ""); res.Mode != guardcfg.ModeRelaxed {
		t.Fatalf("a neutral folder runs the granted global mode: %+v", res)
	}
	if _, err := runGuardCmd(t, "allow", "--global", "relaxed"); err == nil {
		t.Fatal("--global needs --mode")
	}
}

// tq's own manifest writers carry the grant when the result is no looser,
// and drop it when it is.
func TestRewriteCarriesTheGrantWhenNotLooser(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", relaxedWorkspace)
	h := manifestHash(t, root)
	if err := trust.SaveGrant(h, trust.Grant{Mode: "relaxed", Workspace: "acme"}); err != nil {
		t.Fatal(err)
	}
	if err := editManifestGuard(wsFor(t, root), []string{"tq/force-push"}, "off", ""); err != nil {
		t.Fatal(err)
	}
	h2 := manifestHash(t, root)
	if h2 == h {
		t.Fatal("the manifest changed")
	}
	if g, _ := trust.LoadGrant(h2); g == nil || g.Mode != "relaxed" {
		t.Fatalf("carried: %+v", g)
	}
	dropped, err := rewriteManifestGuardCarry(wsFor(t, root), func(gb *guardBlock) error {
		gb.Mode = "custom"
		gb.Base = "relaxed"
		gb.Categories = map[string]string{"git-history": "skip"}
		return nil
	})
	if err != nil || dropped == nil || dropped.Mode != "relaxed" {
		t.Fatalf("a looser table drops the grant: %+v %v", dropped, err)
	}
	if g, _ := trust.LoadGrant(manifestHash(t, root)); g != nil {
		t.Fatalf("no grant for the looser manifest: %+v", g)
	}
}

// A legacy .bypass carries to the new hash as a .mode grant.
func TestRewriteConvertsLegacyBypass(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", legacyProject)
	if err := trust.AllowBypass(manifestHash(t, root)); err != nil {
		t.Fatal(err)
	}
	if err := editManifestGuard(wsFor(t, root), []string{"tq/force-push"}, "off", ""); err != nil {
		t.Fatal(err)
	}
	g, err := trust.LoadGrant(manifestHash(t, root))
	if err != nil || g == nil || g.Mode != "relaxed" || g.FromBypass {
		t.Fatalf("converted grant: %+v %v", g, err)
	}
}
