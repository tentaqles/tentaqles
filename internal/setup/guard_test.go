package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// guardWorkspace makes a trusted workspace "acme" (manifest body appended)
// under a registered base in an isolated home and returns it.
func guardWorkspace(t *testing.T, body string, dirs ...string) *resolve.Workspace {
	t.Helper()
	isolateHome(t)
	base := testutil.TempDir(t)
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
	raw := "schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\n" + body
	if err := os.WriteFile(mp, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	return reload(t, root)
}

func reload(t *testing.T, root string) *resolve.Workspace {
	t.Helper()
	mp := filepath.Join(root, manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	return &resolve.Workspace{Name: filepath.Base(root), Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func TestGuardSetModeWritesWorkspaceAndRetrusts(t *testing.T) {
	w := guardWorkspace(t, "")
	res, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpMode, Mode: "relaxed"})
	if err != nil {
		t.Fatal(err)
	}
	w2 := reload(t, w.Root)
	if w2.Manifest.Guard.Mode != "relaxed" {
		t.Fatalf("mode = %q", w2.Manifest.Guard.Mode)
	}
	if !trust.IsTrusted(w2.Hash) || res.Hash != w2.Hash {
		t.Fatalf("edited manifest must be re-trusted (res %q, now %q)", res.Hash, w2.Hash)
	}
	if !res.Looser || res.Grant == nil || res.Grant.Mode != "relaxed" {
		t.Fatalf("relaxed with no grant is looser than the ceiling: %+v", res)
	}
	if res.Grant.Table["system"] != "ask" || res.Grant.Table["prod-changes"] != "skip" {
		t.Fatalf("a relaxed grant holds the relaxed preset: %v", res.Grant.Table)
	}
	g, _ := trust.LoadGrant(w2.Hash)
	if g != nil {
		t.Fatal("ApplyGuardOp must never grant by itself")
	}
}

func TestGuardRefusesOffLockedAndBadValues(t *testing.T) {
	w := guardWorkspace(t, "")
	for name, op := range map[string]GuardOp{
		"off":            {Kind: GuardOpMode, Mode: "off"},
		"unknown mode":   {Kind: GuardOpMode, Mode: "loose"},
		"base on preset": {Kind: GuardOpMode, Mode: "relaxed", Base: "strict"},
		"bad base":       {Kind: GuardOpMode, Mode: "custom", Base: "custom"},
		"privacy":        {Kind: GuardOpCategory, Category: "privacy", Value: "skip"},
		"identity":       {Kind: GuardOpCategory, Category: "identity", Value: "ask"},
		"unknown cat":    {Kind: GuardOpCategory, Category: "nope", Value: "ask"},
		"bad value":      {Kind: GuardOpCategory, Category: "system", Value: "maybe"},
		"pii override":   {Kind: GuardOpOverride, RuleID: "tq/text-pii", Value: "skip"},
		"secret id":      {Kind: GuardOpOverride, RuleID: "tq/env-file-read", Value: "skip"},
		"guard id":       {Kind: GuardOpOverride, RuleID: "guard/wrong-cloud", Value: "skip"},
		"bad override":   {Kind: GuardOpOverride, RuleID: "tq/iac-apply", Value: "never"},
		"table locked":   {Kind: GuardOpTable, Mode: "custom", Base: "relaxed", Categories: map[string]string{"secrets": "skip"}},
	} {
		if _, err := ApplyGuardOp(w, GuardScope{}, op); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	if reload(t, w.Root).Manifest.Guard.HasMode() {
		t.Fatal("a refused op must leave the manifest alone")
	}
}

func TestGuardToggleTurnsPresetIntoCustom(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n")
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "db-destructive", Value: "ask"}); err != nil {
		t.Fatal(err)
	}
	g := reload(t, w.Root).Manifest.Guard
	if g.Mode != "custom" || g.Base != "relaxed" || g.Categories["db-destructive"] != "ask" || len(g.Categories) != 1 {
		t.Fatalf("guard = %+v", g)
	}
	// A second toggle keeps the base and adds to the table.
	w = reload(t, w.Root)
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "external-writes", Value: "rule"}); err != nil {
		t.Fatal(err)
	}
	g = reload(t, w.Root).Manifest.Guard
	if g.Base != "relaxed" || len(g.Categories) != 2 {
		t.Fatalf("guard = %+v", g)
	}
	// Setting a preset clears the custom table.
	w = reload(t, w.Root)
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpMode, Mode: "strict"}); err != nil {
		t.Fatal(err)
	}
	g = reload(t, w.Root).Manifest.Guard
	if g.Mode != "strict" || g.Base != "" || len(g.Categories) != 0 {
		t.Fatalf("guard = %+v", g)
	}
}

func TestGuardOverrideSetAndClear(t *testing.T) {
	w := guardWorkspace(t, "")
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpOverride, RuleID: "tq/iac-apply", Value: "deny"}); err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if w.Manifest.Guard.RulesOverride["tq/iac-apply"] != "deny" {
		t.Fatalf("overrides = %v", w.Manifest.Guard.RulesOverride)
	}
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpOverride, RuleID: "tq/iac-apply", Value: "clear"}); err != nil {
		t.Fatal(err)
	}
	if g := reload(t, w.Root).Manifest.Guard; g.HasMode() {
		t.Fatalf("clear must drop the key and the empty block: %+v", g)
	}
	raw, _ := os.ReadFile(w.ManifestPath)
	if strings.Contains(string(raw), "guard:") {
		t.Fatalf("empty guard block must go:\n%s", raw)
	}
}

func TestGuardOverrideUnknownIDWarns(t *testing.T) {
	w := guardWorkspace(t, "")
	res, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpOverride, RuleID: "tq/not-a-rule", Value: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "tq/not-a-rule") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestGuardProjectScope(t *testing.T) {
	w := guardWorkspace(t, "", "demo-todo")
	if _, err := ApplyGuardOp(w, GuardScope{Project: "demo-todo"}, GuardOp{Kind: GuardOpMode, Mode: "relaxed"}); err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if p := w.Manifest.Guard.Projects["demo-todo"]; p.Mode != "relaxed" || w.Manifest.Guard.Mode != "" {
		t.Fatalf("guard = %+v", w.Manifest.Guard)
	}
	// A toggle in a project with no mode of its own starts from the workspace's.
	if _, err := ApplyGuardOp(w, GuardScope{Project: "demo-todo"}, GuardOp{Kind: GuardOpCategory, Category: "hygiene", Value: "rule"}); err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if p := w.Manifest.Guard.Projects["demo-todo"]; p.Mode != "custom" || p.Base != "relaxed" || p.Categories["hygiene"] != "rule" {
		t.Fatalf("project = %+v", p)
	}
	for _, bad := range []string{"missing", "..", "../x", "/abs", "."} {
		if _, err := ApplyGuardOp(w, GuardScope{Project: bad}, GuardOp{Kind: GuardOpMode, Mode: "strict"}); err == nil {
			t.Errorf("project %q must be refused", bad)
		}
	}
}

func TestGuardGrantCarriedWhenNotLoosened(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n")
	g, err := GrantMode(w)
	if err != nil || g.Mode != "relaxed" {
		t.Fatalf("grant: %+v %v", g, err)
	}
	// Tightening keeps the grant at the new hash.
	res, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "db-destructive", Value: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if !res.Carried || res.Looser || res.Dropped != "" {
		t.Fatalf("tightening must carry the grant: %+v", res)
	}
	if got, _ := trust.LoadGrant(w.Hash); got == nil || got.Mode != "relaxed" {
		t.Fatalf("grant at the new hash = %+v", got)
	}
	// Loosening past the grant drops it.
	res, err = ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "system", Value: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if res.Carried || !res.Looser || res.Dropped != "relaxed" || res.Grant == nil || res.Grant.Mode != "custom" || res.Grant.Table["system"] != "skip" {
		t.Fatalf("loosening must drop the grant and propose a custom one: %+v", res)
	}
	if got, _ := trust.LoadGrant(w.Hash); got != nil {
		t.Fatalf("no grant at the loosened hash: %+v", got)
	}
}

func TestGuardLegacyBypassCountsAsRelaxedGrant(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  unattended:\n    workspace: true\n")
	if err := trust.AllowBypass(w.Hash); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpOverride, RuleID: "tq/force-push", Value: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	w = reload(t, w.Root)
	if !res.Carried {
		t.Fatalf("the bypass trust is a relaxed grant: %+v", res)
	}
	if got, _ := trust.LoadGrant(w.Hash); got == nil || got.Mode != "relaxed" {
		t.Fatalf("the carried grant is written as .mode: %+v", got)
	}
}

func TestGuardWriteMigratesUnattendedBlock(t *testing.T) {
	body := "guard:\n  disable: [tq/gha-unpinned]\n  unattended:\n    workspace: true\n    projects: [demo-todo]\n    keep_asking: [tq/force-push, tq/commit-pii]\n    codex_sandbox: read-only\n"
	w := guardWorkspace(t, body, "demo-todo")
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "hygiene", Value: "ask"}); err != nil {
		t.Fatal(err)
	}
	g := reload(t, w.Root).Manifest.Guard
	if g.Unattended != nil {
		t.Fatalf("the legacy block must be rewritten: %+v", g.Unattended)
	}
	if g.Mode != "custom" || g.Base != "relaxed" || g.Categories["hygiene"] != "ask" {
		t.Fatalf("workspace: true is relaxed: %+v", g)
	}
	if g.Projects["demo-todo"].Mode != "relaxed" {
		t.Fatalf("projects: %+v", g.Projects)
	}
	if g.RulesOverride["tq/force-push"] != "rule" || len(g.RulesOverride) != 1 {
		t.Fatalf("keep_asking becomes rule, a PII id is dropped: %v", g.RulesOverride)
	}
	if g.Codex == nil || g.Codex.Sandbox != "read-only" {
		t.Fatalf("codex_sandbox moves: %+v", g.Codex)
	}
	if len(g.Disable) != 1 {
		t.Fatalf("other guard keys stay: %+v", g.Disable)
	}
}

func TestGuardWriteMigratesDefaultKeepAsking(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  unattended:\n    workspace: true\n")
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpMode, Mode: "relaxed"}); err != nil {
		t.Fatal(err)
	}
	g := reload(t, w.Root).Manifest.Guard
	if g.RulesOverride["tq/iac-apply"] != "rule" || g.RulesOverride["tq/remote-migration"] != "rule" {
		t.Fatalf("an absent keep_asking keeps the old defaults asking: %v", g.RulesOverride)
	}
}

func TestGuardUnattendedOffClearsProjects(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n  projects:\n    demo-todo: { mode: relaxed, rules_override: { tq/force-push: deny } }\n    web: { mode: strict }\n", "demo-todo", "web")
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpUnattendedOff}); err != nil {
		t.Fatal(err)
	}
	g := reload(t, w.Root).Manifest.Guard
	if g.Mode != "default" {
		t.Fatalf("mode = %q", g.Mode)
	}
	if p, ok := g.Projects["demo-todo"]; !ok || p.Mode != "" || p.RulesOverride["tq/force-push"] != "deny" {
		t.Fatalf("relaxed project modes go, its overrides stay: %+v", g.Projects)
	}
	if g.Projects["web"].Mode != "strict" {
		t.Fatalf("a stricter project mode stays: %+v", g.Projects)
	}
}

func TestGuardRefusesUntrustedManifest(t *testing.T) {
	w := guardWorkspace(t, "")
	if err := os.WriteFile(w.ManifestPath, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\nstack: [go]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpMode, Mode: "strict"}); err == nil || !strings.Contains(err.Error(), "tq allow acme") {
		t.Fatalf("an edited, untrusted manifest must be refused: %v", err)
	}
}

func TestGuardGlobalScope(t *testing.T) {
	isolateHome(t)
	res, err := ApplyGuardOp(nil, GuardScope{Global: true}, GuardOp{Kind: GuardOpMode, Mode: "relaxed"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := guardcfg.Load()
	if err != nil || g.Mode != "relaxed" {
		t.Fatalf("guard.yaml: %+v %v", g, err)
	}
	if !res.Looser || res.Scope != "global" {
		t.Fatalf("res = %+v", res)
	}
	if _, err := ApplyGuardOp(nil, GuardScope{Global: true}, GuardOp{Kind: GuardOpCategory, Category: "system", Value: "deny"}); err != nil {
		t.Fatal(err)
	}
	g, _ = guardcfg.Load()
	if g.Mode != "custom" || g.Base != "relaxed" || g.Categories[guardcfg.CatSystem] != guardcfg.ValueDeny {
		t.Fatalf("guard.yaml: %+v", g)
	}
}

func TestGuardGrantNothingLooser(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: strict\n")
	if _, err := GrantMode(w); err == nil {
		t.Fatal("strict needs no grant")
	}
}

func TestGuardRevokeGrant(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n")
	if _, err := GrantMode(w); err != nil {
		t.Fatal(err)
	}
	_ = trust.AllowBypass(w.Hash)
	if err := RevokeGrant(w); err != nil {
		t.Fatal(err)
	}
	if g, _ := trust.LoadGrant(w.Hash); g != nil {
		t.Fatal("grant must go")
	}
	if trust.IsBypassAllowed(w.Hash) {
		t.Fatal("the legacy bypass file is a relaxed grant too: it must go")
	}
	if !trust.IsTrusted(w.Hash) {
		t.Fatal("revoking a grant keeps the manifest trusted")
	}
}

func TestGuardGrantRefusesStaleWorkspace(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n")
	if err := os.WriteFile(w.ManifestPath, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\nguard:\n  mode: custom\n  categories: { system: skip }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GrantMode(w); err == nil {
		t.Fatal("a grant must never cover an untrusted manifest")
	}
}

func TestGuardViewOf(t *testing.T) {
	relaxed, _ := guardcfg.Preset(guardcfg.ModeRelaxed)
	tbl := relaxed.Clone()
	tbl[guardcfg.CatDBDestructive] = guardcfg.ValueAsk
	tbl[guardcfg.CatSystem] = guardcfg.ValueDeny
	res := guardcfg.Resolved{
		Mode: guardcfg.ModeCustom, Base: guardcfg.ModeRelaxed, Requested: guardcfg.ModeCustom, ModeSource: "acme",
		Table:     tbl,
		Overrides: map[string]guardcfg.Value{"tq/iac-apply": guardcfg.ValueRule},
		Sources:   map[string]string{"db-destructive": "acme", "system": "project:web", "tq/iac-apply": "acme"},
		Ceiling:   &guardcfg.Grant{Mode: "custom"},
		Ignored:   []string{"system"},
	}
	v := GuardViewOf("acme", res, GuardFacts{Confirms: 3, Blocks: 1})
	if v.Label != "custom (relaxed + 2)" {
		t.Fatalf("label = %q", v.Label)
	}
	if len(v.Categories) != 13 {
		t.Fatalf("categories = %d", len(v.Categories))
	}
	locked := map[string]bool{}
	for _, c := range v.Categories {
		if c.Locked {
			locked[c.Name] = true
			if c.Value != "rule" {
				t.Errorf("locked %s shows %s", c.Name, c.Value)
			}
		}
		if c.Name == "system" && (c.Value != "deny" || c.Source != "project:web") {
			t.Errorf("system = %+v", c)
		}
	}
	for _, want := range []string{"identity", "secrets", "self-protection", "privacy"} {
		if !locked[want] {
			t.Errorf("%s must be locked", want)
		}
	}
	if len(locked) != 4 {
		t.Errorf("locked = %v", locked)
	}
	if v.Ceiling != "custom" || !v.Granted || !v.Looser {
		t.Errorf("ceiling = %q granted=%v looser=%v", v.Ceiling, v.Granted, v.Looser)
	}
	if len(v.Overrides) != 1 || v.Overrides[0].ID != "tq/iac-apply" || v.Overrides[0].Source != "acme" {
		t.Errorf("overrides = %+v", v.Overrides)
	}
	if v.Confirms != 3 || v.Blocks != 1 || strings.Join(v.Locked, ",") != "identity,secrets,self-protection,privacy" {
		t.Errorf("view = %+v", v)
	}
}

func TestGuardViewOfDefault(t *testing.T) {
	v := GuardViewOf("", guardcfg.DefaultResolved(), GuardFacts{})
	if v.Label != "default" || v.Ceiling != "default" || v.Granted || v.Looser || v.NeedsGrant {
		t.Fatalf("view = %+v", v)
	}
	if v.Categories[0].Source != "builtin" {
		t.Fatalf("source = %q", v.Categories[0].Source)
	}
}

func TestGuardWorkspacesListsTrusted(t *testing.T) {
	w := guardWorkspace(t, "guard:\n  mode: relaxed\n  projects:\n    demo-todo: { mode: strict }\n", "demo-todo")
	now := time.Now().UTC()
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "deny", Rules: []string{"tq/force-push-main"}, Tool: "Bash", Command: "x"}, nil)
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Action: "ask", Rules: []string{"tq/cloud-delete"}, Tool: "Bash", Command: "y"}, nil)
	guardlog.Append(guardlog.Record{Time: now.Add(-48 * time.Hour), WS: "acme", Action: "ask", Rules: []string{"tq/cloud-delete"}, Tool: "Bash", Command: "old"}, nil)
	views, err := GuardWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Workspace != "acme" {
		t.Fatalf("views = %+v", views)
	}
	v := views[0]
	if v.Confirms != 1 || v.Blocks != 1 {
		t.Fatalf("24h counts = %d/%d", v.Confirms, v.Blocks)
	}
	if !v.NeedsGrant || v.Proposal != "relaxed" {
		t.Fatalf("relaxed with no grant needs one: %+v", v)
	}
	if strings.Join(v.Projects, ",") != "demo-todo" {
		t.Fatalf("projects = %v", v.Projects)
	}
	if _, err := GrantMode(w); err != nil {
		t.Fatal(err)
	}
	views, _ = GuardWorkspaces()
	if views[0].NeedsGrant {
		t.Fatalf("granted: %+v", views[0])
	}
	// An untrusted workspace is not listed.
	if err := os.WriteFile(w.ManifestPath, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {} }\nstack: [x]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if views, _ = GuardWorkspaces(); len(views) != 0 {
		t.Fatalf("untrusted listed: %+v", views)
	}
}

func TestParseGuardScope(t *testing.T) {
	for in, want := range map[string]GuardScope{"": {}, "workspace": {}, "global": {Global: true}, "project:demo-todo": {Project: "demo-todo"}} {
		got, err := ParseGuardScope(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	if _, err := ParseGuardScope("project:"); err == nil {
		t.Error("empty project must be refused")
	}
	if _, err := ParseGuardScope("repo"); err == nil {
		t.Error("unknown scope must be refused")
	}
}

func TestGuardErrorsAreReadable(t *testing.T) {
	w := guardWorkspace(t, "")
	_, err := ApplyGuardOp(w, GuardScope{}, GuardOp{Kind: GuardOpCategory, Category: "privacy", Value: "skip"})
	if err == nil || !errors.Is(err, ErrLockedCategory) || !strings.Contains(err.Error(), "privacy") {
		t.Fatalf("err = %v", err)
	}
	_ = paths.Home()
}
