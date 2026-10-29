package data

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestCostConfigValid(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	cs := costOf(cfg.Pack("data"))
	if v, ok := cs.Cap(engBigQuery, "max_bytes"); !ok || v != 50e9 {
		t.Fatalf("bigquery max_bytes = %v %v", v, ok)
	}
	if v, ok := cs.Cap(engSnowflake, "max_warehouse_size"); !ok || v != 3 {
		t.Fatalf("snowflake size = %v %v", v, ok)
	}
	if v, ok := cs.Cap(engSynapse, "max_dwu"); !ok || v != 500 {
		t.Fatalf("synapse dwu = %v", v)
	}
	if v, ok := cs.Cap(engFabric, "max_capacity_sku"); !ok || v != 8 {
		t.Fatalf("fabric sku = %v", v)
	}
	if !cs.Engine(engFabric) || cs.EstimateInDoD() {
		t.Fatal("engines / estimate_in_dod")
	}
}

func TestCostConfigInvalid(t *testing.T) {
	costIsolate(t)
	cases := map[string]string{
		"unknown engine":      "packs:\n  data:\n    engines: [oracle]\n",
		"engines not a list":  "packs:\n  data:\n    engines: bigquery\n",
		"unknown cost engine": "packs:\n  data:\n    cost:\n      oracle: {max_bytes: 1GB}\n",
		"unknown cost key":    "packs:\n  data:\n    cost:\n      bigquery: {max_rows: 1}\n",
		"bad bytes":           "packs:\n  data:\n    cost:\n      bigquery: {max_bytes: lots}\n",
		"bad size":            "packs:\n  data:\n    cost:\n      snowflake: {max_warehouse_size: HUGE}\n",
		"bad dwu":             "packs:\n  data:\n    cost:\n      synapse: {max_dwu: DW123c}\n",
		"bad sku":             "packs:\n  data:\n    cost:\n      fabric: {max_capacity_sku: F3}\n",
		"bad workers":         "packs:\n  data:\n    cost:\n      databricks: {max_workers: -1}\n",
		"bad id":              "packs:\n  data:\n    cost:\n      databricks: {warehouse_id: \"a b\"}\n",
		"bad bool":            "packs:\n  data:\n    cost:\n      estimate_in_dod: maybe\n",
	}
	for name, raw := range cases {
		if _, err := manifest.ParseRepoPacks([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCostConfigMergeRepoTightens(t *testing.T) {
	costIsolate(t)
	trusted, err := manifest.ParseRepoPacks([]byte("packs:\n  data:\n    engines: [bigquery]\n    cost:\n      bigquery: {max_bytes: 50GB}\n"))
	if err != nil {
		t.Fatal(err)
	}
	tighter, _ := manifest.ParseRepoPacks([]byte("packs:\n  data:\n    engines: [snowflake]\n    cost:\n      bigquery: {max_bytes: 10GB}\n      estimate_in_dod: true\n"))
	c := packs.Merge(trusted, tighter)
	cs := costOf(c.Pack("data"))
	if v, _ := cs.Cap(engBigQuery, "max_bytes"); v != 10e9 {
		t.Fatalf("repo could not tighten: %v", v)
	}
	if !cs.Engine(engSnowflake) || !cs.Engine(engBigQuery) || !cs.EstimateInDoD() {
		t.Fatal("engines union / estimate_in_dod or")
	}
	looser, _ := manifest.ParseRepoPacks([]byte("packs:\n  data:\n    cost:\n      bigquery: {max_bytes: 5TB}\n"))
	c = packs.Merge(trusted, looser)
	if v, _ := costOf(c.Pack("data")).Cap(engBigQuery, "max_bytes"); v != 50e9 {
		t.Fatalf("repo loosened the cap: %v", v)
	}
	if len(c.Problems) == 0 || !strings.Contains(c.Problems[0].String(), "max_bytes") {
		t.Fatalf("loosening not reported: %v", c.Problems)
	}
}

func TestCostParseBytes(t *testing.T) {
	for in, want := range map[string]float64{"50GB": 50e9, "512MiB": 512 << 20, "1000": 1000, "1.5 TB": 1.5e12, "10b": 10} {
		if got, err := costParseBytes(in); err != nil || got != want {
			t.Errorf("costParseBytes(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := costParseBytes("1e9"); err == nil {
		t.Error("1e9 accepted")
	}
}
