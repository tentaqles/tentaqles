package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func runModeCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := NewRoot()
	root.SetArgs(args)
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func loadGuard(t *testing.T, root string) manifest.Guard {
	t.Helper()
	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return m.Guard
}

func TestModeShowDefault(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Chdir(root)
	out, err := runModeCmd(t, "", "mode")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"guard mode for acme: default", "ceiling: default (no grant)", "privacy", "rule  locked", "git-history", "skipped in 24h: 0 confirms and 0 blocks", "locked: identity, secrets, self-protection, privacy"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestModeShowIsReadOnlyForAgents(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Setenv("CLAUDECODE", "1")
	if out, err := runModeCmd(t, "", "mode", "--ws", "acme"); err != nil {
		t.Fatalf("plain tq mode is read-only, agents may run it: %v\n%s", err, out)
	}
	_ = root
}

func TestModeShowJSONIsStable(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	out, err := runModeCmd(t, "", "mode", "--ws", "acme", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"workspace", "mode", "label", "categories", "overrides", "ceiling", "granted", "looser_than_default", "needs_grant", "clamped", "ignored", "skipped_confirms_24h", "skipped_blocks_24h", "locked", "projects"} {
		if _, ok := v[k]; !ok {
			t.Errorf("JSON lacks %q", k)
		}
	}
	if cats, _ := v["categories"].([]any); len(cats) != 13 {
		t.Errorf("categories = %v", v["categories"])
	}
}

func TestModeShowCodexAsks(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	out, err := runModeCmd(t, "", "mode", "--ws", "acme", "--agent", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "asks that become blocks in Codex") || !strings.Contains(out, "tq/cloud-delete") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestModeWritesRefuseAgents(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	for _, k := range agentMarkers {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, "1")
			for _, args := range [][]string{
				{"mode", "set", "relaxed", "--ws", "acme"},
				{"mode", "toggle", "system", "skip", "--ws", "acme"},
				{"mode", "override", "tq/iac-apply", "skip", "--ws", "acme"},
				{"mode", "edit", "--ws", "acme"},
				{"unattended", "on", "--ws", "acme"},
				{"unattended", "off", "--ws", "acme"},
			} {
				_, err := runModeCmd(t, "y\n", args...)
				if !errors.Is(err, runner.ErrHumanOnly) || !strings.Contains(err.Error(), k) {
					t.Errorf("%v under %s: %v", args, k, err)
				}
			}
		})
	}
	if loadGuard(t, root).HasMode() {
		t.Fatal("a refused write must leave the manifest alone")
	}
}

func TestModeWritesNeedATerminal(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	for _, k := range agentMarkers {
		t.Setenv(k, "")
	}
	prev := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return false }
	t.Cleanup(func() { stdinIsTerminal = prev })
	_, err := runModeCmd(t, "y\n", "mode", "set", "strict", "--ws", "acme")
	if !errors.Is(err, runner.ErrHumanOnly) || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("err = %v", err)
	}
}

func TestModeSetRelaxedGrantsOnYes(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	out, err := runModeCmd(t, "y\n", "mode", "set", "relaxed", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if loadGuard(t, root).Mode != "relaxed" {
		t.Fatal("mode not written")
	}
	if !strings.Contains(out, "grant relaxed for acme? [y/N]") || !strings.Contains(out, "granted relaxed for acme") {
		t.Fatalf("out:\n%s", out)
	}
	h := manifestHash(t, root)
	if !trust.IsTrusted(h) {
		t.Fatal("manifest must be re-trusted")
	}
	if g, _ := trust.LoadGrant(h); g == nil || g.Mode != "relaxed" {
		t.Fatalf("grant = %+v", g)
	}
}

func TestModeSetRelaxedWithoutYesPrintsAllowLine(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	out, err := runModeCmd(t, "\n", "mode", "set", "relaxed", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "runs at default until you run: tq allow --mode acme relaxed") {
		t.Fatalf("out:\n%s", out)
	}
	if g, _ := trust.LoadGrant(manifestHash(t, root)); g != nil {
		t.Fatal("no grant without a yes")
	}
}

func TestModeSetStrictNeedsNoGrant(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	out, err := runModeCmd(t, "", "mode", "set", "strict", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "grant") || loadGuard(t, root).Mode != "strict" {
		t.Fatalf("out:\n%s", out)
	}
	if !strings.Contains(out, "mode strict for acme (workspace); manifest re-trusted") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestModeSetRefusesOff(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	if _, err := runModeCmd(t, "", "mode", "set", "off", "--ws", "acme"); err == nil || !strings.Contains(err.Error(), "never stored") {
		t.Fatalf("err = %v", err)
	}
}

func TestModeToggleAndOverride(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "guard:\n  mode: strict\n")
	asHuman(t)
	if _, err := runModeCmd(t, "", "mode", "toggle", "privacy", "skip", "--ws", "acme"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("privacy is locked: %v", err)
	}
	if _, err := runModeCmd(t, "", "mode", "override", "tq/text-pii", "skip", "--ws", "acme"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("a PII id is locked: %v", err)
	}
	out, err := runModeCmd(t, "", "mode", "toggle", "db-destructive", "deny", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	g := loadGuard(t, root)
	if g.Mode != "custom" || g.Base != "strict" || g.Categories["db-destructive"] != "deny" {
		t.Fatalf("guard = %+v", g)
	}
	if out, err = runModeCmd(t, "", "mode", "override", "tq/iac-apply", "deny", "--ws", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if loadGuard(t, root).RulesOverride["tq/iac-apply"] != "deny" {
		t.Fatal("override not written")
	}
	if out, err = runModeCmd(t, "", "mode", "override", "tq/iac-apply", "clear", "--ws", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(loadGuard(t, root).RulesOverride) != 0 {
		t.Fatal("clear must remove it")
	}
}

func TestModeSetProject(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if err := os.MkdirAll(filepath.Join(root, "demo-todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	asHuman(t)
	t.Chdir(root)
	out, err := runModeCmd(t, "n\n", "mode", "set", "relaxed", "--project", "demo-todo")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if loadGuard(t, root).Projects["demo-todo"].Mode != "relaxed" {
		t.Fatalf("guard = %+v", loadGuard(t, root))
	}
	for _, p := range []string{"missing", "..", root, t.TempDir()} {
		if _, err := runModeCmd(t, "", "mode", "set", "strict", "--project", p); err == nil {
			t.Errorf("project %q must be refused", p)
		}
	}
	if _, err := runModeCmd(t, "", "mode", "set", "strict", "--project", "demo-todo", "--global"); err == nil {
		t.Error("--project and --global together must be refused")
	}
}

func TestModeSetGlobal(t *testing.T) {
	isolateHome(t)
	asHuman(t)
	out, err := runModeCmd(t, "", "mode", "set", "relaxed", "--global")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	g, err := guardcfg.Load()
	if err != nil || g.Mode != "relaxed" {
		t.Fatalf("guard.yaml: %+v %v", g, err)
	}
	if !strings.Contains(out, "tq allow --mode --global relaxed") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestModeSetCarriesGrantWhenTightening(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	if _, err := runModeCmd(t, "y\n", "mode", "set", "relaxed", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
	out, err := runModeCmd(t, "", "mode", "toggle", "db-destructive", "ask", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "relaxed grant carried") {
		t.Fatalf("out:\n%s", out)
	}
	if g, _ := trust.LoadGrant(manifestHash(t, root)); g == nil {
		t.Fatal("grant not carried")
	}
	out, err = runModeCmd(t, "n\n", "mode", "toggle", "system", "skip", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "mode grant dropped (was relaxed)") || !strings.Contains(out, "tq allow --mode acme custom") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestModeEditAppliesTheForm(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "guard:\n  mode: relaxed\n")
	asHuman(t)
	prev := modeEditForm
	var seen modeEditState
	modeEditForm = func(c modeEditContext, s modeEditState) (modeEditState, bool, error) {
		seen = s
		s.Mode = "custom"
		s.Base = "relaxed"
		s.Values["db-destructive"] = "ask"
		s.Values["privacy"] = "skip" // locked rows are ignored, whatever the form says
		return s, true, nil
	}
	t.Cleanup(func() { modeEditForm = prev })
	out, err := runModeCmd(t, "n\n", "mode", "edit", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if seen.Mode != "relaxed" || seen.Values["system"] != "ask" {
		t.Fatalf("the form starts from the layer: %+v", seen)
	}
	g := loadGuard(t, root)
	if g.Mode != "custom" || g.Base != "relaxed" || len(g.Categories) != 1 || g.Categories["db-destructive"] != "ask" {
		t.Fatalf("guard = %+v", g)
	}
}

func TestModeEditCancelChangesNothing(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	prev := modeEditForm
	modeEditForm = func(c modeEditContext, s modeEditState) (modeEditState, bool, error) {
		s.Mode = "strict"
		return s, false, nil
	}
	t.Cleanup(func() { modeEditForm = prev })
	out, err := runModeCmd(t, "", "mode", "edit", "--ws", "acme")
	if err != nil || !strings.Contains(out, "nothing changed") {
		t.Fatalf("%v\n%s", err, out)
	}
	if loadGuard(t, root).HasMode() {
		t.Fatal("cancel must not write")
	}
}

func TestModeEditDiff(t *testing.T) {
	before := modeEditState{Mode: "relaxed", Values: map[string]string{"system": "ask", "hygiene": "skip"}}
	after := modeEditState{Mode: "custom", Base: "relaxed", Values: map[string]string{"system": "deny", "hygiene": "skip"}}
	d := modeEditDiff(before, after)
	if !strings.Contains(d, "mode: relaxed -> custom (relaxed)") || !strings.Contains(d, "system: ask -> deny") || strings.Contains(d, "hygiene") {
		t.Fatalf("diff:\n%s", d)
	}
}

func TestModeLog(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	now := time.Now().UTC()
	guardlog.Append(guardlog.Record{Time: now.Add(-time.Minute), WS: "acme", Scope: "workspace", Mode: "relaxed", ClaudeMode: "bypassPermissions", Action: "deny", To: "ask", Rules: []string{"tq/force-push-main"}, Tool: "Bash", Command: "git push -f origin main"}, nil)
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Scope: "workspace", Mode: "relaxed", ClaudeMode: "auto", Action: "ask", Rules: []string{"tq/cloud-delete"}, Tool: "Bash", Command: "az group delete"}, nil)
	guardlog.Append(guardlog.Record{Time: now.Add(-48 * time.Hour), WS: "acme", Rules: []string{"tq/old"}}, nil)
	t.Setenv("CLAUDECODE", "1") // read-only: agents may list it
	out, err := runModeCmd(t, "", "mode", "log", "--ws", "acme")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "TIME") {
		t.Fatalf("log:\n%s", out)
	}
	if !strings.Contains(lines[1], "ask -> allow") || !strings.Contains(lines[1], "tq/cloud-delete") {
		t.Fatalf("newest first, a skip shows allow:\n%s", out)
	}
	if !strings.Contains(lines[2], "deny -> ask") || !strings.Contains(lines[2], "relaxed") {
		t.Fatalf("a lowered deny shows its new action:\n%s", out)
	}
	if _, err := runModeCmd(t, "", "mode", "log", "--ws", "acme", "--since", "nope"); err == nil {
		t.Error("bad --since must be refused")
	}
}

func TestUnattendedAliases(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if err := os.MkdirAll(filepath.Join(root, "demo-todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	asHuman(t)
	t.Chdir(root)
	out, err := runModeCmd(t, "n\n", "unattended", "on", "--project", "demo-todo", "--codex-sandbox", "read-only")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "deprecated: use tq mode set relaxed --project demo-todo") {
		t.Fatalf("out:\n%s", out)
	}
	g := loadGuard(t, root)
	if g.Projects["demo-todo"].Mode != "relaxed" || g.Codex == nil || g.Codex.Sandbox != "read-only" || g.Unattended != nil {
		t.Fatalf("guard = %+v", g)
	}
	if out, err = runModeCmd(t, "", "unattended", "off", "--project", "demo-todo"); err != nil || !strings.Contains(out, "deprecated: use tq mode set default --project demo-todo") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, ok := loadGuard(t, root).Projects["demo-todo"]; ok {
		t.Fatalf("off clears the project: %+v", loadGuard(t, root))
	}
	if _, err = runModeCmd(t, "n\n", "unattended", "on"); err != nil {
		t.Fatal(err)
	}
	if loadGuard(t, root).Mode != "relaxed" {
		t.Fatal("on is relaxed for the workspace")
	}
	if out, err = runModeCmd(t, "", "unattended", "off"); err != nil || !strings.Contains(out, "deprecated: use tq mode set default") {
		t.Fatalf("%v\n%s", err, out)
	}
	if loadGuard(t, root).Mode != "default" {
		t.Fatal("off is default for the workspace")
	}
	if out, err = runModeCmd(t, "", "unattended", "status"); err != nil || !strings.Contains(out, "deprecated: use tq mode") || !strings.Contains(out, "guard mode for acme") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err = runModeCmd(t, "", "unattended", "log"); err != nil || !strings.Contains(out, "deprecated: use tq mode log") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err = runModeCmd(t, "", "unattended", "on", "--codex-sandbox", "nope"); err == nil {
		t.Error("bad sandbox must be refused")
	}
}

// A toggle on a default workspace asks for and grants the custom table, never
// relaxed (review r0 M1, spec §6.3).
func TestModeToggleGrantsCustomNotRelaxed(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	asHuman(t)
	out, err := runModeCmd(t, "y\n", "mode", "toggle", "db-destructive", "skip", "--ws", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "grant custom for acme? [y/N]") || strings.Contains(out, "grant relaxed") {
		t.Fatalf("out:\n%s", out)
	}
	if g, _ := trust.LoadGrant(manifestHash(t, root)); g == nil || g.Mode != "custom" || g.Table["db-destructive"] != "skip" || g.Table["prod-changes"] == "skip" {
		t.Fatalf("grant = %+v", g)
	}
}
