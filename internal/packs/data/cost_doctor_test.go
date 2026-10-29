package data

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestCostDoctorCapMissing(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [bigquery, snowflake, postgres]\n    cost:\n      snowflake: {statement_timeout_s: 60}\n")
	fs := costDoctor((&packs.Input{Config: cfg}).ForPack("data"))
	got := map[string]bool{}
	for _, f := range fs {
		if f.Code != "data-cost-cap-missing" || f.Level != "warn" {
			t.Fatalf("finding %+v", f)
		}
		got[f.Msg] = true
	}
	if len(fs) != 2 || !got["engine bigquery is in packs.data.engines but has no cost cap"] || !got["engine postgres is in packs.data.engines but has no cost cap"] {
		t.Fatalf("findings %+v", fs)
	}
	if fs := costDoctor((&packs.Input{Config: costConfig(t, costFullConfig)}).ForPack("data")); len(fs) != 0 {
		t.Fatalf("full config: %+v", fs)
	}
}

// costGit is a GitReader over an in-memory old side.
type costGit map[string]string

func (g costGit) Show(ref, p string) ([]byte, error) {
	if s, ok := g[p]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}
func (g costGit) ListFiles() ([]string, error) { return nil, nil }

func TestWarehouseCostFlag(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	root := t.TempDir()
	write := func(p, s string) {
		full := filepath.Join(root, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("models/marts/orders.sql", "{{ config(materialized='table') }}\nselect 1")
	write("models/staging/stg.sql", "{{ config(materialized='view') }}\nselect 1")
	write("tq-packs.yaml", "packs:\n  data:\n    cost:\n      bigquery: {max_bytes: 10GB}\n")
	write("README.md", "acme")
	old := costGit{
		"models/marts/orders.sql": "{{ config(materialized='view') }}\nselect 1",
		"models/staging/stg.sql":  "{{ config(materialized='view') }}\nselect 2",
		"tq-packs.yaml":           "packs:\n  data:\n    cost:\n      bigquery: {max_bytes: 10GB}\n    contracts: {paths: [x]}\n",
	}
	raise := func(paths ...string) bool {
		in := &packs.Input{Root: root, Base: "HEAD", Git: old, Config: cfg}
		for _, p := range paths {
			in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: 'M'})
		}
		return raiseWarehouseCost(in.ForPack("data"))
	}
	if !raise("models/marts/orders.sql") {
		t.Error("view -> table not raised")
	}
	if raise("models/staging/stg.sql", "README.md") {
		t.Error("raised on a view model edit")
	}
	if raise("tq-packs.yaml") {
		t.Error("raised on a tq-packs.yaml edit outside cost")
	}
	write("tq-packs.yaml", "packs:\n  data:\n    cost:\n      bigquery: {max_bytes: 20GB}\n")
	if !raise("tq-packs.yaml") {
		t.Error("cost change in tq-packs.yaml not raised")
	}
	if !raise("databricks.yml") || !raise("synapse/bigDataPool/spark1.json") {
		t.Error("sizing file not raised")
	}
	var found bool
	for _, f := range packs.Flags("data") {
		if f.Name == flagWarehouseCost {
			found = f.Question != "" && len(f.Reviewers) == 1 && f.Reviewers[0] == "pipeline-reviewer"
		}
	}
	if !found {
		t.Fatal("warehouse_cost flag not registered as specified")
	}
}

func TestCostCommandMounted(t *testing.T) {
	var names []string
	for _, c := range packs.Commands("data") {
		names = append(names, c.Name())
		if c.Name() == "cost" {
			for _, s := range c.Commands() {
				if s.Name() == "estimate" {
					return
				}
			}
		}
	}
	t.Fatalf("tq pack data cost estimate not mounted: %v", names)
}
