package guardcfg

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// pendingIDs are classified in Categories ahead of their rules: unmerged
// branches (packs, framework) and other workstreams (WS2's nested-agent
// rule), plus the plugin's Python guards. TestEveryRuleHasCategory lets the
// table name them; every other id must be a rule tq ships.
var pendingIDs = map[string]bool{
	NestedAgentID: true, PluginIdentityGuardID: true, PluginHeadlessManifestGuardID: true,
	"tq/hub-store-write": true, "tq/pack-config-change": true, "tq/human-answer": true,
	"tq/dbt-prod-target": true, "tq/model-promote": true, "tq/pipeline-run": true, "tq/iac-apply-env": true,
	"tq/db-migrate-env": true, "tq/restore-target": true, "tq/dbt-full-refresh": true, "tq/iac-identity-drift": true,
	"tq/warehouse-resize": true, "tq/bq-unbounded-query": true, "tq/warehouse-query-unbounded": true,
}

// Every rule tq ships has exactly one valid category, and the table names
// no rule tq does not know (spec E §2).
func TestEveryRuleHasCategory(t *testing.T) {
	ids := builtinIDs()
	for _, id := range IdentityGuardIDs {
		ids[id] = true
	}
	for id := range ids {
		c, ok := Categories[id]
		if !ok {
			t.Errorf("rule %s has no category in guardcfg.Categories", id)
			continue
		}
		if _, ok := ParseCategory(string(c)); !ok {
			t.Errorf("rule %s: unknown category %q", id, c)
		}
	}
	for id, c := range Categories {
		if !ids[id] && !pendingIDs[id] {
			t.Errorf("Categories names %s (%s), which is not a rule tq knows", id, c)
		}
	}
	for _, id := range []string{OutsidePathWriteID, SystemDirWriteID, ElevationID} {
		if !ids[id] || Categories[id] != CatSystem {
			t.Errorf("%s must ship in system", id)
		}
	}
	if !ids[ModeChangeID] || Categories[ModeChangeID] != CatSelfProtection {
		t.Errorf("%s must ship in self-protection", ModeChangeID)
	}
	// manifest keeps its own copy of the names (it cannot import guardcfg).
	var cats, locked []string
	for _, c := range AllCategories() {
		cats = append(cats, string(c))
	}
	for _, c := range LockedCategories() {
		locked = append(locked, string(c))
	}
	if !slices.Equal(cats, manifest.GuardCategories) || !slices.Equal(locked, manifest.GuardLockedCategories) ||
		!slices.Equal(StoredModes(), manifest.GuardStoredModes) || !slices.Equal(PresetNames(), manifest.GuardBases) {
		t.Fatal("manifest's guard-mode names drifted from guardcfg")
	}
	var vals []string
	for _, v := range Values() {
		vals = append(vals, string(v))
	}
	if !slices.Equal(vals, manifest.GuardValues) {
		t.Fatal("manifest's values drifted")
	}
	if !manifest.LockedRuleID("tq/text-pii") || manifest.LockedRuleID("tq/iac-apply") {
		t.Fatal("guardcfg must install manifest.LockedRuleID")
	}
}

// corpusRules returns one rule per built-in id and action.
func corpusRules() []*policy.Rule {
	loadCorpus()
	var out []*policy.Rule
	ids := make([]string, 0, len(corpusByID))
	for id := range corpusByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, a := range corpusByID[id] {
			out = append(out, &policy.Rule{ID: id, Action: a})
		}
	}
	return out
}

var relaxedGrant = &Grant{Mode: ModeRelaxed}

func launchResolve(t *testing.T, mode string, grant *Grant) Resolved {
	t.Helper()
	return Resolve(nil, "", "", "", "", File{}, RepoFile{}, mode, grant)
}

// Every built-in rule under each preset gets the preset's value for its
// category, and locked rules keep their own action (spec E §16, Presets).
func TestPresetsOverEveryRule(t *testing.T) {
	for _, p := range PresetNames() {
		res := launchResolve(t, p, relaxedGrant)
		if res.Mode != p {
			t.Fatalf("launch %s resolved to %s", p, res.Mode)
		}
		table, _ := Preset(p)
		for _, r := range corpusRules() {
			c := CategoryOf(r)
			want := table.Get(c).Apply(r.Action)
			if got := res.Effective(r); got != want {
				t.Errorf("%s: %s (%s, %s) = %q, want %q", p, r.ID, c, r.Action, got, want)
			}
		}
	}
	relaxed := launchResolve(t, ModeRelaxed, relaxedGrant)
	strict := launchResolve(t, ModeStrict, nil)
	for _, c := range []struct {
		res  Resolved
		id   string
		own  policy.Action
		want policy.Action
	}{
		{relaxed, "tq/force-push-main", policy.Deny, policy.Ask},
		{relaxed, "tq/gh-repo-delete", policy.Deny, policy.Ask},
		{relaxed, SystemDirWriteID, policy.Deny, policy.Ask},
		{relaxed, OutsidePathWriteID, policy.Ask, policy.Ask},
		{relaxed, ElevationID, policy.Ask, policy.Ask},
		{relaxed, "tq/registry-write", policy.Ask, policy.Ask},
		{relaxed, "tq/commit-pii", policy.Deny, policy.Deny},
		{relaxed, "tq/push-pii", policy.Deny, policy.Deny},
		{relaxed, "tq/text-pii", policy.Deny, policy.Deny},
		{relaxed, "tq/pii-fixture-dump", policy.Deny, policy.Deny},
		{relaxed, "tq/env-file-shell", policy.Deny, policy.Deny},
		{relaxed, "tq/trust-store-write", policy.Deny, policy.Deny},
		{relaxed, ModeChangeID, policy.Deny, policy.Deny},
		{relaxed, "tq/gha-branch-ref", policy.Deny, policy.Allow},
		{relaxed, "tq/iac-apply", policy.Ask, policy.Allow},
		{strict, "tq/force-push", policy.Ask, policy.Deny},
		{strict, "tq/iac-apply", policy.Ask, policy.Deny},
		{strict, "tq/cloud-delete", policy.Ask, policy.Deny},
		{strict, "tq/mcp-github-write", policy.Ask, policy.Ask},
		{strict, "tq/sql-destructive-shell", policy.Ask, policy.Ask},
	} {
		if got := c.res.Effective(&policy.Rule{ID: c.id, Action: c.own}); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.res.Mode, c.id, got, c.want)
		}
	}
	if !relaxed.LooserThanDefault() || strict.LooserThanDefault() || DefaultResolved().LooserThanDefault() {
		t.Fatal("LooserThanDefault: relaxed yes, strict and default no")
	}
	if strict.Mode != ModeStrict || len(strict.Clamped) != 0 {
		t.Fatalf("strict needs no grant: %+v", strict)
	}
	// Strict pins external-writes at ask, which the default ceiling keeps
	// for an ask rule but never lets lower a custom deny rule.
	if got := strict.Effective(&policy.Rule{ID: "acme/post", Action: policy.Deny, Category: "external-writes"}); got != policy.Deny {
		t.Fatalf("a deny in external-writes stays deny under strict: %q", got)
	}
}

// Locked rules keep their own action under every preset, table, override,
// launch, repo file and ceiling, even ones validation would reject.
func TestLockedNeverChange(t *testing.T) {
	var cases []Resolved
	allSkip := map[string]string{}
	for _, c := range AllCategories() {
		allSkip[string(c)] = string(ValueSkip)
	}
	loose := &Grant{Mode: ModeCustom, Table: allSkip, Overrides: map[string]string{}}
	for id := range Categories {
		loose.Overrides[id] = string(ValueSkip)
	}
	for _, launch := range []string{"", ModeDefault, ModeRelaxed, ModeStrict, ModeCustom, "off", "bogus"} {
		for _, v := range Values() {
			cats := map[Category]Value{}
			ov := map[string]Value{}
			for _, c := range AllCategories() {
				cats[c] = v
			}
			for id := range Categories {
				ov[id] = v
			}
			g := File{Mode: ModeCustom, Base: ModeRelaxed, Categories: cats, RulesOverride: ov}
			m := &manifest.Manifest{Client: "acme"}
			m.Guard.Mode = ModeCustom
			m.Guard.Categories = map[string]string{}
			m.Guard.RulesOverride = map[string]string{}
			for c := range cats {
				m.Guard.Categories[string(c)] = string(v)
			}
			for id := range ov {
				m.Guard.RulesOverride[id] = string(v)
			}
			repo := RepoFile{Mode: ModeRelaxed, Categories: cats, RulesOverride: ov}
			for _, grant := range []*Grant{nil, relaxedGrant, loose} {
				cases = append(cases, Resolve(m, "acme", "", "", "", g, repo, launch, grant))
				cases = append(cases, Resolve(nil, "", "", "", "", g, RepoFile{}, launch, grant))
			}
			cases = append(cases, Resolved{Mode: ModeCustom, Table: Table(cats), Overrides: ov, Ceiling: loose})
		}
	}
	for _, res := range cases {
		for _, r := range corpusRules() {
			if !Locked(CategoryOf(r)) {
				continue
			}
			if got := res.Effective(r); r.Action.StricterThan(got) {
				t.Fatalf("locked %s (%s) became %q under %+v", r.ID, r.Action, got, res)
			}
		}
		for _, c := range LockedCategories() {
			if res.Table.Get(c) != ValueRule && res.Table != nil && res.Mode != ModeCustom {
				t.Fatalf("locked category %s set to %s", c, res.Table.Get(c))
			}
		}
		// A custom rule declaring a locked category is always applied.
		r := &policy.Rule{ID: "acme/own", Action: policy.Deny, Category: "secrets"}
		if res.Effective(r) != policy.Deny {
			t.Fatal("a custom rule in a locked category is always applied")
		}
	}
}

func writeManifest(t *testing.T, body string) (*manifest.Manifest, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(p, []byte("schema: tentaqles-client-v2\nclient: acme\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return m, root
}

func ruleFor(id string) *policy.Rule {
	a, _ := BuiltinAction(id)
	return &policy.Rule{ID: id, Action: a}
}

func TestNoGrantClampsToDefault(t *testing.T) {
	m, root := writeManifest(t, "guard:\n  mode: relaxed\n")
	res := Resolve(m, "acme", root, root, "", File{}, RepoFile{}, "", nil)
	if res.Mode != ModeDefault || res.Requested != ModeRelaxed || res.ModeSource != "acme" || len(res.Clamped) == 0 {
		t.Fatalf("relaxed with no grant runs at default: %+v", res)
	}
	for _, r := range corpusRules() {
		if got := res.Effective(r); got != r.Action {
			t.Errorf("%s: %q, want its own %q", r.ID, got, r.Action)
		}
	}
	if !res.IsDefault() || res.LooserThanDefault() {
		t.Fatalf("clamped back to default: %+v", res)
	}
	g := Resolve(m, "acme", root, root, "", File{}, RepoFile{}, "", relaxedGrant)
	if g.Mode != ModeRelaxed || len(g.Clamped) != 0 || g.Effective(ruleFor("tq/iac-destroy")) != policy.Allow {
		t.Fatalf("with the grant: %+v", g)
	}
	// the legacy keep_asking defaults: relaxed from guard.unattended keeps
	// tq/iac-apply and tq/remote-migration asking.
	lm, lroot := writeManifest(t, "guard:\n  unattended:\n    workspace: true\n")
	l := Resolve(lm, "acme", lroot, lroot, "", File{}, RepoFile{}, "", relaxedGrant)
	if l.Mode != ModeRelaxed || l.Effective(ruleFor("tq/iac-apply")) != policy.Ask || l.Effective(ruleFor("tq/cloud-delete")) != policy.Allow {
		t.Fatalf("legacy unattended block: %+v", l)
	}
}

func TestRelaxedGrantClampsALooserCustomTable(t *testing.T) {
	m, root := writeManifest(t, "guard:\n  mode: custom\n  base: relaxed\n  categories: {system: skip, db-destructive: ask}\n  rules_override: {tq/force-push-main: skip}\n")
	res := Resolve(m, "acme", root, root, "", File{}, RepoFile{}, "", relaxedGrant)
	if res.Mode != ModeCustom || res.Base != ModeRelaxed || res.Table[CatSystem] != ValueAsk || res.Table[CatDBDestructive] != ValueAsk {
		t.Fatalf("system clamped to the grant's ask, db tightened stays: %+v", res)
	}
	if !slices.Contains(res.Clamped, string(CatSystem)) || !slices.Contains(res.Clamped, "tq/force-push-main") {
		t.Fatalf("clamped: %v", res.Clamped)
	}
	if got := res.Effective(ruleFor(SystemDirWriteID)); got != policy.Ask {
		t.Fatalf("system-dir-write: %q", got)
	}
	if got := res.Effective(ruleFor("tq/force-push-main")); got != policy.Ask {
		t.Fatalf("an override cannot pass the grant: %q", got)
	}
	// A custom grant that skips db-destructive does not allow skipping system.
	cg := &Grant{Mode: ModeCustom, Table: map[string]string{"db-destructive": "skip"}}
	m2, root2 := writeManifest(t, "guard:\n  mode: custom\n  categories: {db-destructive: skip, system: skip}\n")
	r2 := Resolve(m2, "acme", root2, root2, "", File{}, RepoFile{}, "", cg)
	if r2.Effective(ruleFor("tq/sql-destructive-shell")) != policy.Allow || r2.Effective(ruleFor(SystemDirWriteID)) != policy.Deny {
		t.Fatalf("custom grant: %+v", r2)
	}
}

func TestPrecedenceLayers(t *testing.T) {
	m, root := writeManifest(t, "guard:\n  mode: relaxed\n  projects:\n    app: {mode: strict}\n    app/web: {mode: custom, base: relaxed, categories: {db-destructive: ask}}\n  actions: {tq/gha-unpinned: deny}\n")
	for _, d := range []string{"app/web/src", "other"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	g := File{Mode: ModeStrict}
	at := func(cwd, target, launch string) Resolved {
		return Resolve(m, "acme", root, filepath.Join(root, cwd), target, g, RepoFile{}, launch, relaxedGrant)
	}
	if r := at("other", "", ""); r.Mode != ModeRelaxed || r.ModeSource != "acme" || r.Sources[string(CatSystem)] != "acme" {
		t.Fatalf("workspace beats global: %+v", r)
	}
	if r := at("app", "", ""); r.Mode != ModeStrict || r.ModeSource != "project:app" {
		t.Fatalf("project: %+v", r)
	}
	if r := at("app/web/src", "", ""); r.Mode != ModeCustom || r.ModeSource != "project:app/web" || r.Table[CatDBDestructive] != ValueAsk {
		t.Fatalf("the deepest project wins: %+v", r)
	}
	if r := at("app/web", filepath.Join(root, "other", "x.go"), ""); r.Mode != ModeRelaxed {
		t.Fatalf("a file target outside the project falls back to the workspace: %+v", r)
	}
	if r := at("app/web", filepath.Join(t.TempDir(), "x.go"), ""); r.Mode != ModeStrict || r.ModeSource != SourceGlobal {
		t.Fatalf("a file target outside the workspace falls back to the global layer: %+v", r)
	}
	if r := at("other", "", ModeStrict); r.Mode != ModeStrict || r.ModeSource != SourceLaunch || r.Requested != ModeStrict {
		t.Fatalf("launch: %+v", r)
	}
	// guard.actions changes the rule's own action, which is what rule means.
	r := at("app", "", "")
	gha := &policy.Rule{ID: "tq/gha-unpinned", Action: Merge(File{}, nil, m, "acme").ActionFor("tq/gha-unpinned", policy.Ask)}
	if r.Effective(gha) != policy.Deny {
		t.Fatal("guard.actions keeps working under a mode")
	}
	if r := Resolve(nil, "", "", t.TempDir(), "", g, RepoFile{}, "", nil); r.Mode != ModeStrict || r.ModeSource != SourceGlobal {
		t.Fatalf("a neutral folder takes the global mode: %+v", r)
	}
	if r := Resolve(nil, "", "", "", "", File{Mode: ModeRelaxed}, RepoFile{}, "", nil); r.Mode != ModeDefault {
		t.Fatalf("a global relaxed needs the global grant: %+v", r)
	}
}

func TestRepoFileOnlyTightens(t *testing.T) {
	m, root := writeManifest(t, "guard:\n  mode: relaxed\n")
	repo := MergeRepo(
		RepoFile{Categories: map[Category]Value{CatSystem: ValueSkip, CatDBDestructive: ValueAsk, CatSecrets: ValueSkip}},
		RepoFile{RulesOverride: map[string]Value{"tq/iac-apply": ValueAsk, "tq/force-push-main": ValueSkip, "tq/env-dump": ValueSkip}},
	)
	res := Resolve(m, "acme", root, root, "", File{}, repo, "", relaxedGrant)
	if res.Table[CatDBDestructive] != ValueAsk || res.Sources[string(CatDBDestructive)] != SourceRepo || res.Table[CatSystem] != ValueAsk {
		t.Fatalf("table: %+v", res.Table)
	}
	if res.Overrides["tq/iac-apply"] != ValueAsk || res.Effective(ruleFor("tq/force-push-main")) != policy.Ask {
		t.Fatalf("overrides: %+v", res.Overrides)
	}
	for _, want := range []string{"system", "tq/force-push-main", "secrets", "tq/env-dump"} {
		if !slices.Contains(res.Ignored, want) {
			t.Errorf("Ignored must name %s: %v", want, res.Ignored)
		}
	}
	// A repo mode tightens the whole table, even over a launch override.
	strict := Resolve(m, "acme", root, root, "", File{}, MergeRepo(RepoFile{Mode: ModeStrict}), ModeRelaxed, relaxedGrant)
	if strict.Effective(ruleFor("tq/force-push")) != policy.Deny || strict.Effective(ruleFor("tq/iac-destroy")) != policy.Deny ||
		strict.Effective(ruleFor("tq/sql-destructive-shell")) != policy.Ask {
		t.Fatalf("repo strict over launch relaxed: %+v", strict)
	}
}

func TestPrivacyMigration(t *testing.T) {
	m, root := writeManifest(t, "guard:\n  unattended:\n    workspace: true\n    keep_asking: [tq/commit-pii, tq/iac-apply]\n  disable: [tq/push-pii, tq/force-push]\n  actions: {tq/text-pii: ask, tq/commit-secret: deny}\n")
	if v := m.Guard.ModeView(); len(v.RulesOverride) != 1 || v.RulesOverride["tq/iac-apply"] != "rule" {
		t.Fatalf("the PII keep_asking entry is dropped: %v", v.RulesOverride)
	}
	e := Merge(File{}, nil, m, "acme")
	if e.Off("tq/push-pii") || !e.Off("tq/force-push") {
		t.Fatalf("a locked disable is ignored: %v", e.Disable)
	}
	if _, ok := e.Actions["tq/text-pii"]; ok || e.Actions["tq/commit-secret"] != policy.Deny {
		t.Fatalf("a looser action on a locked rule is ignored, a stricter one kept: %v", e.Actions)
	}
	if !slices.Equal(e.LockedIgnored, []string{"actions:tq/text-pii@acme", "disable:tq/push-pii@acme"}) {
		t.Fatalf("LockedIgnored: %v", e.LockedIgnored)
	}
	res := Resolve(m, "acme", root, root, "", File{}, RepoFile{}, "", relaxedGrant)
	if res.Effective(ruleFor("tq/commit-pii")) != policy.Deny {
		t.Fatal("PII stays denied in relaxed")
	}
	if _, _, err := func() (*manifest.Manifest, string, error) {
		p := filepath.Join(t.TempDir(), manifest.FileName)
		_ = os.WriteFile(p, []byte("schema: tentaqles-client-v2\nclient: acme\nguard:\n  rules_override: {tq/text-pii: skip}\n"), 0o600)
		mm, err := manifest.Load(p)
		return mm, p, err
	}(); err == nil {
		t.Fatal("rules_override of a PII rule is a load error")
	}
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.WriteFile(Path(), []byte("mode: custom\ncategories: {privacy: skip}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("a locked category in guard.yaml is a load error")
	}
	if err := os.WriteFile(Path(), []byte("mode: off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("mode: off in guard.yaml is a load error")
	}
}

func TestGrantsCarryAndDigest(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	m, root := writeManifest(t, "guard:\n  mode: custom\n  base: relaxed\n  categories: {db-destructive: ask}\n  projects:\n    app: {mode: custom, base: relaxed, categories: {system: skip}}\n")
	gr, ok := GrantFor(m, "acme", root, File{}, ModeCustom)
	if !ok || gr.Mode != ModeCustom || gr.Table["system"] != "skip" || gr.Table["db-destructive"] != "skip" || gr.Workspace != "acme" {
		t.Fatalf("a custom grant is the loosest of root and projects: %+v", gr)
	}
	if _, ok := gr.Table["privacy"]; ok {
		t.Fatal("a grant never names a locked category")
	}
	if r, ok := GrantFor(m, "acme", root, File{}, ModeRelaxed); !ok || r.Table["system"] != "ask" {
		t.Fatalf("relaxed grant: %+v", r)
	}
	if _, ok := GrantFor(m, "acme", root, File{}, ModeStrict); ok {
		t.Fatal("strict needs no grant")
	}
	if !WithinGrant(m, "acme", root, File{}, &gr) || WithinGrant(m, "acme", root, File{}, relaxedGrant) || WithinGrant(m, "acme", root, File{}, nil) {
		t.Fatal("WithinGrant")
	}
	if err := trust.SaveGrant("old", gr); err != nil {
		t.Fatal(err)
	}
	if _, carried, err := CarryGrant("old", "new", m, "acme", root, File{}); err != nil || !carried {
		t.Fatalf("an unchanged table carries: %v %v", carried, err)
	}
	looser, _ := writeManifest(t, "guard:\n  mode: custom\n  base: relaxed\n  categories: {git-history: skip}\n")
	if old, carried, _ := CarryGrant("new", "newer", looser, "acme", root, File{}); carried || old == nil {
		t.Fatal("a looser table drops the grant")
	}
	if g, _ := trust.LoadGrant("newer"); g != nil {
		t.Fatal("nothing written for a dropped grant")
	}
	// a legacy .bypass carries as a relaxed .mode grant
	rel, _ := writeManifest(t, "guard:\n  mode: relaxed\n")
	_ = trust.AllowBypass("b1")
	if _, carried, _ := CarryGrant("b1", "b2", rel, "acme", root, File{}); !carried {
		t.Fatal("bypass carries for relaxed")
	}
	if g, _ := trust.LoadGrant("b2"); g == nil || g.Mode != ModeRelaxed || g.FromBypass || g.Table["system"] != "ask" {
		t.Fatalf("converted to .mode: %+v", g)
	}
	// the global grant pins guard.yaml's mode keys
	f := File{Mode: ModeRelaxed}
	gg, ok := GlobalGrantFor(f, ModeRelaxed)
	if !ok || gg.Digest != GlobalDigest(f) {
		t.Fatalf("global grant: %+v", gg)
	}
	if err := trust.SaveGrant(trust.GlobalGrantKey, gg); err != nil {
		t.Fatal(err)
	}
	if GlobalGrant(f) == nil {
		t.Fatal("matching digest")
	}
	edited := File{Mode: ModeCustom, Base: ModeRelaxed, Categories: map[Category]Value{CatSystem: ValueSkip}}
	if GlobalGrant(edited) != nil {
		t.Fatal("an edit of guard.yaml drops the global grant")
	}
	if r := Resolve(nil, "", "", "", "", edited, RepoFile{}, "", GlobalGrant(edited)); r.Effective(ruleFor(SystemDirWriteID)) != policy.Deny {
		t.Fatal("digest mismatch clamps to default")
	}
	if r := Resolve(nil, "", "", "", "", f, RepoFile{}, "", GlobalGrant(f)); r.Mode != ModeRelaxed {
		t.Fatalf("matching global grant: %+v", r)
	}
}

func TestGrantTableFailsClosed(t *testing.T) {
	tb, ov := grantTable(&Grant{Mode: ModeCustom, Table: map[string]string{"system": "bogus", "privacy": "skip"}, Overrides: map[string]string{"tq/env-dump": "skip"}})
	if tb.Get(CatSystem) != ValueRule || tb.Get(CatPrivacy) != ValueRule || len(ov) != 0 {
		t.Fatalf("damaged grant: %v %v", tb, ov)
	}
	tb, _ = grantTable(&Grant{Mode: "off"})
	if !equalTables(tb, ModeDefault) {
		t.Fatal("an unknown grant mode is the default ceiling")
	}
}
