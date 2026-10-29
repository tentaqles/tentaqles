package data

import (
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// costIsolate points TQ_HOME, HOME and USERPROFILE at a temp dir.
func costIsolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// costConfig parses a packs: block (validated by the registered keys) and
// merges it as the trusted layer.
// costGuardEnv is the hook's view: the trusted manifest that enables the pack.
func costGuardEnv(t *testing.T, raw string) packs.GuardEnv {
	t.Helper()
	m, err := manifest.ParseRepoPacks([]byte(raw))
	if err != nil {
		t.Fatalf("parse packs: %v", err)
	}
	return packs.GuardEnv{Config: packs.Merge(m, nil), Manifest: &manifest.Manifest{Packs: m}}
}

func costConfig(t *testing.T, raw string) packs.Config {
	t.Helper()
	m, err := manifest.ParseRepoPacks([]byte(raw))
	if err != nil {
		t.Fatalf("parse packs: %v", err)
	}
	return packs.Merge(m, nil)
}

const costFullConfig = `packs:
  data:
    engines: [bigquery, snowflake, databricks, postgres, fabric, synapse]
    cost:
      bigquery:   { max_bytes: 50GB }
      snowflake:  { max_warehouse_size: MEDIUM, statement_timeout_s: 900 }
      databricks: { max_workers: 8, warehouse_id: 0123abcd }
      fabric:     { max_capacity_sku: F8 }
      synapse:    { max_dwu: DW500c, max_spark_nodes: 10 }
      postgres:   { max_plan_cost: 10000000, statement_timeout_s: 300 }
`

func costParseForBench(raw string) (map[string]manifest.PackConfig, error) {
	return manifest.ParseRepoPacks([]byte(raw))
}

// runCostGuard runs one registered guard of the data pack on a call.
func runCostGuard(t *testing.T, id string, cfg packs.Config, call policy.ToolCall) (policy.Action, string) {
	t.Helper()
	for _, g := range packs.Guards() {
		if g.ID == id && g.Pack == "data" {
			a, reason, _ := g.Check(call, packs.NewGuardCtx(packs.GuardEnv{Config: cfg}, "data"))
			return a, reason
		}
	}
	t.Fatalf("guard %s not registered", id)
	return "", ""
}
