package ml

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"gopkg.in/yaml.v3"
)

func node(t *testing.T, src string) yaml.Node {
	t.Helper()
	var d yaml.Node
	if err := yaml.Unmarshal([]byte(src), &d); err != nil {
		t.Fatal(err)
	}
	return *d.Content[0]
}

func TestValidators(t *testing.T) {
	good := []struct {
		fn  func(yaml.Node) error
		src string
	}{
		{validateModels, `[{name: churn, card: models/churn/MODEL_CARD.md, metrics: reports/m.json, paths: ["src/**"], drift: {reference: a.json, current: b.json, current_command: "uv run python -m drift {model}"}}]`},
		{validatePromotion, `{protected: [production, champion], command: "uv run python scripts/promote.py {model} {version} {target}", valid_for: 7d, require: [model_card, drift, repro]}`},
		{validateDrift, `{reference: a.json, current: b.json, metric: js, warn: 0.1, fail: 0.25, max_warn_share: 0.3, features: {tenure_days: {fail: 0.2}}, metrics: {auc: {min: 0.78}}, in_dod: false}`},
	}
	for _, g := range good {
		if err := g.fn(node(t, g.src)); err != nil {
			t.Fatalf("%s: %v", g.src, err)
		}
	}
	bad := []struct {
		fn   func(yaml.Node) error
		src  string
		want string
	}{
		{validateModels, `{name: churn}`, "list"},
		{validateModels, `[{card: x.md}]`, "name"},
		{validateModels, `[{name: churn}]`, "card"},
		{validateModels, `[{name: churn, card: ../x.md}]`, "inside the repository"},
		{validateModels, `[{name: churn, card: a.md}, {name: churn, card: b.md}]`, "twice"},
		{validateModels, `[{name: churn, card: a.md, extra: 1}]`, "unknown key"},
		{validatePromotion, `{protected: ["prod uction"]}`, "bad entry"},
		{validatePromotion, `{command: "promote {model} {who}"}`, "placeholder"},
		{validatePromotion, `{command: "promote {model} | tee log"}`, "shell operator"},
		{validatePromotion, `{valid_for: 60d}`, "30d"},
		{validatePromotion, `{require: [vibes]}`, "unknown item"},
		{validateDrift, `{metric: kl}`, "psi or js"},
		{validateDrift, `{warn: 0.5, fail: 0.1}`, "above"},
		{validateDrift, `{max_warn_share: 2}`, "between"},
		{validateDrift, `{metrics: {auc: {}}}`, "needs min"},
		{validateDrift, `{current_command: "x {version}"}`, "placeholder"},
		{validateDrift, `{reference: C:/data/a.json}`, "inside the repository"},
	}
	for _, b := range bad {
		err := b.fn(node(t, b.src))
		if err == nil || !strings.Contains(err.Error(), b.want) {
			t.Fatalf("%s: %v, want %q", b.src, err, b.want)
		}
	}
}

func TestManifestValidatorRegistered(t *testing.T) {
	_, err := manifest.ParseRepoPacks([]byte("ml:\n  promotion: {valid_for: 90d}\n"))
	if err == nil || !strings.Contains(err.Error(), "30d") {
		t.Fatalf("the pack validator runs on tq-packs.yaml: %v", err)
	}
}

func TestRepoLayerCannotLoosen(t *testing.T) {
	trusted := `
ml:
  models: [{ name: churn, card: models/churn/MODEL_CARD.md }]
  promotion: { protected: [production], command: "promote {model} {version} {target}", valid_for: 7d }
  drift: { reference: r.json, current: c.json, warn: 0.1, fail: 0.25, metrics: { auc: { min: 0.78 } }, in_dod: true }
`
	repo := `
ml:
  models: [{ name: fraud, card: models/fraud/MODEL_CARD.md }]
  promotion: { protected: [champion], valid_for: 30d, command: "other {model} {version} {target}" }
  drift: { warn: 0.5, fail: 0.2, metrics: { auc: { min: 0.5 } }, in_dod: false, reference: other.json }
`
	cfg := configFrom(t, trusted, repo)
	st := load(cfg.Pack("ml"), manifestFrom(t, trusted))
	if !st.Protected("production") || !st.Protected("champion") {
		t.Fatalf("protected is a union: %v", st.Promotion.Protected)
	}
	if st.ValidFor().Hours() != 7*24 {
		t.Fatalf("valid_for keeps the minimum: %s", st.Promotion.ValidFor)
	}
	if fl(st.Drift.Warn, 0) != 0.1 || fl(st.Drift.Fail, 0) != 0.2 {
		t.Fatalf("thresholds keep the minimum: %v %v", *st.Drift.Warn, *st.Drift.Fail)
	}
	if fl(st.Drift.Metrics["auc"].Min, 0) != 0.78 || !st.Drift.InDoD || st.Drift.Reference != "r.json" {
		t.Fatalf("lower bounds keep the maximum, in_dod stays on, reference stays: %+v", st.Drift)
	}
	if _, ok := st.Model("fraud"); !ok {
		t.Fatal("the repo may add a model")
	}
	if st.Promotion.Command != "other {model} {version} {target}" {
		t.Fatal("commands may come from the repo (they run only after a typed yes)")
	}
	if len(cfg.Problems) == 0 {
		t.Fatal("loosening attempts are reported")
	}
}

// The manifest leaves every loosenable key out: the merge then takes the
// repo values as they are, so load clamps them to the tq defaults.
func TestRepoLayerCannotLoosenOmittedKeys(t *testing.T) {
	trusted := `
ml:
  models: [{ name: churn, card: models/churn/MODEL_CARD.md }]
  promotion: { protected: [production], command: "promote {model} {version} {target}" }
  drift: { reference: r.json, current: c.json }
`
	repo := `
ml:
  promotion: { require: [model_card], valid_for: 30d }
  drift: { warn: 99, fail: 100, max_warn_share: 1, features: { plan: { warn: 50, fail: 60 }, tenure: { warn: 0.01 } } }
`
	st := load(configFrom(t, trusted, repo).Pack("ml"), manifestFrom(t, trusted))
	if strings.Join(st.Promotion.Require, ",") != "model_card,drift,repro" {
		t.Fatalf("a repo-only require cannot drop items: %v", st.Promotion.Require)
	}
	if st.ValidFor() != 7*24*time.Hour {
		t.Fatalf("a repo-only valid_for is clamped to the 7d default: %s", st.ValidFor())
	}
	if fl(st.Drift.Warn, 0) != DefaultWarn || fl(st.Drift.Fail, 0) != DefaultFail || fl(st.Drift.MaxWarnShare, 0) != DefaultMaxWarnShare {
		t.Fatalf("repo-only thresholds are clamped: %v %v %v", fl(st.Drift.Warn, 0), fl(st.Drift.Fail, 0), fl(st.Drift.MaxWarnShare, 0))
	}
	if p := st.Drift.Features["plan"]; fl(p.Warn, 0) != DefaultWarn || fl(p.Fail, 0) != DefaultFail {
		t.Fatalf("a repo-only feature override is clamped to the global value: %+v", p)
	}
	if p := st.Drift.Features["tenure"]; fl(p.Warn, 0) != 0.01 {
		t.Fatalf("a tighter repo value stays: %+v", p)
	}
	if len(st.Clamped) != 7 {
		t.Fatalf("each clamp is reported: %q", st.Clamped)
	}
	// With no manifest at all every value is repo-only.
	st = load(configFrom(t, "", repo).Pack("ml"), nil)
	if strings.Join(st.Promotion.Require, ",") != "model_card,drift,repro" || fl(st.Drift.Warn, 0) != DefaultWarn {
		t.Fatalf("no manifest: %+v", st)
	}
	// The manifest may set looser values itself; a feature then clamps to the
	// manifest's global value, and the manifest's own feature value stays.
	trusted2 := strings.Replace(trusted, "drift: { reference: r.json, current: c.json }",
		"drift: { reference: r.json, current: c.json, warn: 0.4, fail: 0.6, features: { tenure: { fail: 0.9 } } }", 1)
	trusted2 = strings.Replace(trusted2, `command: "promote {model} {version} {target}" }`, `command: "promote {model} {version} {target}", require: [model_card], valid_for: 20d }`, 1)
	st = load(configFrom(t, trusted2, repo).Pack("ml"), manifestFrom(t, trusted2))
	if strings.Join(st.Promotion.Require, ",") != "model_card" || st.ValidFor() != 20*24*time.Hour {
		t.Fatalf("the manifest's own require and valid_for stay: %v %s", st.Promotion.Require, st.ValidFor())
	}
	if fl(st.Drift.Warn, 0) != 0.4 || fl(st.Drift.Fail, 0) != 0.6 {
		t.Fatalf("the manifest's thresholds stay: %v %v", fl(st.Drift.Warn, 0), fl(st.Drift.Fail, 0))
	}
	if p := st.Drift.Features["plan"]; fl(p.Warn, 0) != 0.4 || fl(p.Fail, 0) != 0.6 {
		t.Fatalf("a repo feature clamps to the manifest's global: %+v", p)
	}
	if p := st.Drift.Features["tenure"]; fl(p.Fail, 0) != 0.9 || fl(p.Warn, 0) != 0.01 {
		t.Fatalf("the manifest's own feature value stays, the repo's tighter warn too: %+v", p)
	}
}

func TestDefaults(t *testing.T) {
	src := "ml: {models: [{name: churn, card: c.md}, {name: churn, card: other.md}]}\n"
	st := load(configFrom(t, src, "").Pack("ml"), manifestFrom(t, src))
	if len(st.Models) != 1 || st.Models[0].Card != "c.md" {
		t.Fatalf("the first entry of a name wins: %+v", st.Models)
	}
	if strings.Join(st.Promotion.Require, ",") != "model_card,drift,repro" || st.Drift.Metric != "psi" {
		t.Fatalf("%+v", st)
	}
}

func TestModelPromotionFlag(t *testing.T) {
	cfg := configFrom(t, guardConfig, "")
	for _, c := range []struct {
		path string
		want bool
	}{
		{"scripts/promote.py", true}, {"models/churn/MODEL_CARD.md", true}, {"tq-packs.yaml", true}, {"src/app.py", false}, {"sub/tq-packs.yaml", false},
	} {
		in := (&packs.Input{Config: cfg, Changed: []packs.ChangedFile{{Path: c.path, Status: 'M'}}}).ForPack("ml")
		if got := raiseModelPromotion(in); got != c.want {
			t.Fatalf("%s: %v", c.path, got)
		}
	}
	var found bool
	for _, f := range packs.Flags("ml") {
		if f.Name == "model_promotion" && f.Reviewers[0] == "ml-reviewer" {
			found = true
		}
	}
	if !found {
		t.Fatal("model_promotion is registered")
	}
}

func TestDoctor(t *testing.T) {
	cfg := configFrom(t, `
ml:
  models: [{ name: churn, card: models/churn/MODEL_CARD.md }]
  promotion: { protected: [production] }
  drift: { reference: data/ref.json, current: c.json }
`, "")
	in := (&packs.Input{Root: t.TempDir(), Config: cfg}).ForPack("ml")
	var codes []string
	for _, f := range promoteDoctor(in) {
		codes = append(codes, f.Code)
	}
	if strings.Join(codes, ",") != "ml-card-missing,ml-drift-reference-missing,ml-promotion-command-missing" {
		t.Fatal(codes)
	}
	// A repo value the loader clamps is a doctor warning.
	trusted := "ml:\n  models: [{ name: churn, card: c.md }]\n"
	clampIn := mlInput(t, t.TempDir(), trusted, "ml:\n  drift: { warn: 0.9 }\n").ForPack("ml")
	n := 0
	for _, f := range promoteDoctor(clampIn) {
		if f.Code == "ml-config-clamped" && strings.Contains(f.Msg, "drift.warn") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("clamped warn: %d", n)
	}
}

func TestCommandsRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range packs.Commands("ml") {
		names[c.Name()] = true
		if c.Name() == "promote" {
			for _, s := range c.Commands() {
				names["promote "+s.Name()] = true
			}
		}
	}
	for _, want := range []string{"promote", "drift", "promote request", "promote apply", "promote status"} {
		if !names[want] {
			t.Fatalf("missing %s: %v", want, names)
		}
	}
	ids := map[string]bool{}
	for _, c := range packs.Checks("ml") {
		ids[c.ID] = true
	}
	for _, id := range []string{"ml/model-card", "ml/drift", "ml/promotion-in-code"} {
		if !ids[id] {
			t.Fatalf("check %s not registered", id)
		}
	}
}

func TestApplyCommandRefusesAgent(t *testing.T) {
	isolate(t)
	t.Setenv("CLAUDECODE", "1")
	called := false
	old := resolveWorkspace
	resolveWorkspace = func(string) *resolve.Workspace { called = true; return nil }
	t.Cleanup(func() { resolveWorkspace = old })
	c := newPromoteApplyCmd()
	c.SetArgs([]string{"dec_1"})
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	if err := c.Execute(); !errors.Is(err, packs.ErrHumanOnly) {
		t.Fatalf("%v", err)
	}
	if called {
		t.Fatal("the refusal comes before anything else")
	}
}

func TestStatusCommand(t *testing.T) {
	r := newPromoteRepo(t)
	res := r.request(t, "production")
	old := resolveWorkspace
	resolveWorkspace = func(string) *resolve.Workspace { return r.env.WS }
	t.Cleanup(func() { resolveWorkspace = old })
	t.Chdir(r.root)
	c := newPromoteStatusCmd()
	c.SetArgs([]string{"churn"})
	var out bytes.Buffer
	c.SetOut(&out)
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), res.Decision) || !strings.Contains(out.String(), "pending") {
		t.Fatal(out.String())
	}
	// drift command: JSON and the fail exit.
	code := -1
	oldExit := exitFunc
	exitFunc = func(c int) { code = c }
	t.Cleanup(func() { exitFunc = oldExit })
	d := newDriftCmd()
	d.SetArgs([]string{"churn", "--json"})
	out.Reset()
	d.SetOut(&out)
	if err := d.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"schema": "tq.drift-result/v1"`) || code != -1 {
		t.Fatalf("%d %s", code, out.String())
	}
}
