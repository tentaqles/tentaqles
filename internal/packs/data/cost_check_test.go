package data

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

const costDoDConfig = `packs:
  data:
    engines: [bigquery]
    cost:
      bigquery: { max_bytes: 1GB }
      estimate_in_dod: true
`

func costDBTInput(t *testing.T, cfg packs.Config) *packs.Input {
	t.Helper()
	root, _ := filepath.Abs(filepath.Join("testdata", "cost-estimate", "dbt", "repo"))
	in := &packs.Input{Root: root, Config: cfg, Tools: packs.NewToolRunner(root, cfg), Changed: []packs.ChangedFile{
		{Path: "models/marts/daily.sql", Status: 'M'}, {Path: "models/marts/orders.sql", Status: 'M'}, {Path: "README.md", Status: 'M'},
	}}
	return in.ForPack("data")
}

func TestCostEstimateCheck(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "dbt", "bq")
	costCopyFixture(t, filepath.Join("testdata", "cost-estimate", "dbt", "manifest.json"), filepath.Join(state, "dbt.manifest"))
	costCopyFixture(t, filepath.Join("testdata", "cost-estimate", "bigquery", "output.json"), filepath.Join(state, "bq.out"))
	cfg := costConfig(t, costDoDConfig)
	c := costCheck(t, "data/cost-estimate")
	if c.Kind != packs.External || c.Tool != "dbt" {
		t.Fatalf("check shape %+v", c)
	}
	in := costDBTInput(t, cfg)
	if !c.Applies(in) {
		t.Fatal("does not apply with estimate_in_dod")
	}
	r := c.Run(context.Background(), in)
	if r.Status != packs.Fail {
		t.Fatalf("status %s: %s %+v", r.Status, r.Detail, r.Findings)
	}
	var over, notCompiled bool
	for _, f := range r.Findings {
		switch {
		case f.Rule == "cost-over-cap" && f.File == "models/marts/orders.sql" && f.Level == packs.LevelError:
			over = true
		case f.Rule == "cost-not-compiled" && f.File == "models/marts/daily.sql":
			notCompiled = true
		}
		if strings.Contains(f.Message, "stg_orders") || strings.Contains(f.Message, "select") {
			t.Fatalf("finding carries SQL: %+v", f)
		}
	}
	if !over || !notCompiled {
		t.Fatalf("findings %+v", r.Findings)
	}
	log, _ := os.ReadFile(filepath.Join(state, "dbt.log"))
	l := string(log)
	if !strings.Contains(l, "compile\x1f--target\x1fdev\x1f--target-path\x1f") || !strings.Contains(l, "--select\x1fpath:models/marts/daily.sql\x1fpath:models/marts/orders.sql") || !strings.Contains(l, "--log-path") {
		t.Fatalf("dbt call:\n%s", l)
	}
	if strings.Contains(l, "README") {
		t.Fatal("selected a non-model")
	}
	// dbt's logs and the fake's log go to temp dirs, never into the tree.
	for _, p := range []string{"dbt.log", "logs", "target"} {
		if _, err := os.Stat(filepath.Join(in.Root, p)); err == nil {
			t.Fatalf("%s written into the testdata repo", p)
		}
	}
	bq, _ := os.ReadFile(filepath.Join(state, "bq.log"))
	if !strings.Contains(string(bq), "--dry_run") || strings.Count(string(bq), "argv:") != 1 {
		t.Fatalf("bq calls:\n%s", bq)
	}
	// The scratch target path is gone after the run.
	entries, _ := filepath.Glob(filepath.Join(os.Getenv("TQ_HOME"), "packs", "*", "scratch", "*"))
	if len(entries) != 0 {
		t.Fatalf("scratch left behind: %v", entries)
	}
}

func TestCostEstimateCheckOptIn(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n")
	if costCheck(t, "data/cost-estimate").Applies(costDBTInput(t, cfg)) {
		t.Fatal("applies without estimate_in_dod")
	}
}

func TestCostEstimateCheckCompileFails(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "dbt")
	if err := os.WriteFile(filepath.Join(state, "dbt.out"), []byte("Database Error: tester@acme.test has no access\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "dbt.exit"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := costCheck(t, "data/cost-estimate").Run(context.Background(), costDBTInput(t, costConfig(t, costDoDConfig)))
	if r.Status != packs.Fail || strings.Contains(r.Detail, "tester") {
		t.Fatalf("%+v", r)
	}
}
