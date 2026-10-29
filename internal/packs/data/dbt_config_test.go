package data

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestDBTConfigValidation(t *testing.T) {
	good := `data:
  dbt:
    project: analytics
    contract_paths: ["models/marts/**"]
    new_model_tests: [unique, not_null]
    build: off
    dev_target: dev
    prod_targets: [prod, production]
    state: artifacts/prod
`
	if _, err := manifest.ParseRepoPacks([]byte(good)); err != nil {
		t.Fatalf("valid block: %v", err)
	}
	bad := map[string]string{
		"unknown key":     "data:\n  dbt:\n    projekt: x\n",
		"build value":     "data:\n  dbt:\n    build: prod\n",
		"test name":       "data:\n  dbt:\n    new_model_tests: [accepted_values]\n",
		"list kind":       "data:\n  dbt:\n    prod_targets: prod\n",
		"dotdot":          "data:\n  dbt:\n    state: ../../outside\n",
		"target charset":  "data:\n  dbt:\n    dev_target: \"dev; rm\"\n",
		"not a mapping":   "data:\n  dbt: [a]\n",
		"empty list item": "data:\n  dbt:\n    contract_paths: [\"\"]\n",
	}
	for name, body := range bad {
		_, err := manifest.ParseRepoPacks([]byte(body))
		if err == nil {
			t.Errorf("%s: accepted %q", name, body)
			continue
		}
		if !strings.Contains(err.Error(), "line ") {
			t.Errorf("%s: the error names the line: %v", name, err)
		}
	}
}

func TestDBTConfigMergeIsAddOnly(t *testing.T) {
	trusted := packsOf(t, `
data:
  dbt:
    contract_paths: ["models/marts/**"]
    prod_targets: [prod]
    dev_target: dev
`)
	repo := packsOf(t, `
data:
  dbt:
    contract_paths: ["models/finance/**"]
    prod_targets: [live]
    dev_target: prod
    new_model_tests: [unique]
`)
	c := packs.Merge(trusted, repo)
	cfg := dbtSettings(c.Pack("data"))
	if strings.Join(cfg.ContractPaths, ",") != "models/marts/**,models/finance/**" {
		t.Errorf("contract_paths union: %v", cfg.ContractPaths)
	}
	if strings.Join(cfg.ProdTargets, ",") != "prod,live" {
		t.Errorf("prod_targets union: %v", cfg.ProdTargets)
	}
	if cfg.DevTarget != "dev" {
		t.Errorf("the repo file cannot move dev_target: %q", cfg.DevTarget)
	}
	if len(c.Problems) == 0 {
		t.Error("the ignored dev_target is reported")
	}
}

func TestDBTSettingsDefaults(t *testing.T) {
	cfg := dbtSettings(packs.Merge(packsOf(t, "data: {enabled: true}"), nil).Pack("data"))
	if cfg.Build != "off" || cfg.DevTarget != "dev" || strings.Join(cfg.ProdTargets, ",") != "prod,production" ||
		strings.Join(cfg.NewModelTests, ",") != "unique,not_null" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if !cfg.isProdTarget("Production") || cfg.isProdTarget("dev") {
		t.Fatal("prod targets compare case-insensitively")
	}
}

func TestDBTRegistration(t *testing.T) {
	want := []string{idContractEnforced, idDBTBuildModified, idIncremental, idNewModelTests, idDBTParse, idSQLFluff, idHardcodedRel, idSelectStar}
	have := map[string]packs.Check{}
	for _, c := range packs.Checks("data") {
		have[c.ID] = c
	}
	for _, id := range want {
		if _, ok := have[id]; !ok {
			t.Errorf("%s is not registered", id)
		}
	}
	if c := have[idDBTParse]; c.Kind != packs.External || c.Tool != "dbt" || c.MinVersion != "1.8.0" {
		t.Errorf("dbt-parse: %+v", c)
	}
	if c := have[idSQLFluff]; c.Kind != packs.External || c.Tool != "sqlfluff" {
		t.Errorf("sqlfluff: %+v", c)
	}
	guards := map[string]packs.Guard{}
	for _, g := range packs.Guards() {
		guards[g.ID] = g
	}
	for _, id := range []string{guardProdTarget, guardFullRefresh} {
		if g, ok := guards[id]; !ok || g.Pack != "data" {
			t.Errorf("guard %s: %+v", id, g)
		}
	}
	hint := false
	for _, h := range packs.Hints("data") {
		if h.Glob == "dbt_project.yml" {
			hint = true
		}
	}
	if !hint {
		t.Error("the dbt_project.yml hint is registered")
	}
}

func TestDBTDoctor(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	in := (&packs.Input{Root: root, Config: packs.Merge(packsOf(t, "data: {dbt: {build: dev, state: artifacts/prod}}"), nil)}).ForPack("data")
	fs := dbtDoctor(in)
	if len(fs) != 1 || fs[0].Code != "data-dbt-state-missing" || fs[0].Level != "warn" {
		t.Fatalf("missing state: %+v", fs)
	}
	writeFile(t, root, "artifacts/prod/manifest.json", "{}")
	if fs := dbtDoctor(in); len(fs) != 0 {
		t.Fatalf("state present: %+v", fs)
	}
	in = (&packs.Input{Root: root, Config: packs.Merge(packsOf(t, "data: {enabled: true}"), nil)}).ForPack("data")
	if fs := dbtDoctor(in); len(fs) != 0 {
		t.Fatalf("build off: %+v", fs)
	}
}
