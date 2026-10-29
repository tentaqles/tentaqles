package data

import (
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Workstream D4 (spec C §3.6): the warehouse cost guardrail.
func init() {
	packs.RegisterPack(packs.Pack{
		Name: "data",
		ConfigKeys: map[string]func(yaml.Node) error{
			"engines": validateEngines,
			"cost":    validateCost,
		},
		// An unset cap is no limit (data-cost-cap-missing), so the bounds
		// default to 0 with ZeroUnlimited: a repo file may add any cap, and
		// only tighten one the manifest sets.
		Merge: map[string]packs.MergeRule{
			"engines": packs.Union(),
			// Named per engine: a cost.*.<key> pattern would sit under
			// cost.estimate_in_dod too.
			"cost.bigquery.max_bytes":            {Kind: packs.MergeMin, Default: "0GB", ZeroUnlimited: true},
			"cost.snowflake.max_bytes":           {Kind: packs.MergeMin, Default: "0GB", ZeroUnlimited: true},
			"cost.databricks.max_bytes":          {Kind: packs.MergeMin, Default: "0GB", ZeroUnlimited: true},
			"cost.snowflake.statement_timeout_s": {Kind: packs.MergeMin, Default: "0", ZeroUnlimited: true},
			"cost.postgres.statement_timeout_s":  {Kind: packs.MergeMin, Default: "0", ZeroUnlimited: true},
			"cost.databricks.max_workers":        {Kind: packs.MergeMin, Default: "0", ZeroUnlimited: true},
			"cost.synapse.max_spark_nodes":       {Kind: packs.MergeMin, Default: "0", ZeroUnlimited: true},
			"cost.postgres.max_plan_cost":        {Kind: packs.MergeMin, Default: "0", ZeroUnlimited: true},
			"cost.estimate_in_dod":               packs.Or(),
			"cost.databricks.warehouse_id":       packs.TrustedAdd(),
			// Size scales the merge cannot order: added when the manifest
			// is silent (no limit), never changed.
			"cost.snowflake.max_warehouse_size": packs.TrustedAdd(),
			"cost.fabric.max_capacity_sku":      packs.TrustedAdd(),
			"cost.synapse.max_dwu":              packs.TrustedAdd(),
		},
		Flags: []packs.Flag{{
			Name:      flagWarehouseCost,
			Reviewers: []string{"pipeline-reviewer"},
			Raise:     raiseWarehouseCost,
			Question:  warehouseCostQuestion,
		}},
		Doctor:   costDoctor,
		Commands: func() []*cobra.Command { return []*cobra.Command{newCostCmd()} },
	})
	packs.Register(packs.Check{ID: checkBQJobMaxBytes, Pack: "data", Kind: packs.Builtin, Applies: bqLintApplies, Run: runBQJobMaxBytes})
	packs.Register(packs.Check{ID: checkCostEstimate, Pack: "data", Kind: packs.External, Tool: "dbt", Timeout: 900 * time.Second,
		Env: dbtEnv, Applies: costEstimateApplies, Run: runCostEstimate})
	shell := []string{"Bash", "PowerShell"}
	packs.RegisterGuard(packs.Guard{ID: GuardBQUnbounded, Category: guardcfg.CatPacks, Default: "deny", Pack: "data", Events: shell, Check: withScripts(checkBQUnbounded, bqToolRe, func(c costSettings) bool { return c.Engine(engBigQuery) }, "bq")})
	packs.RegisterGuard(packs.Guard{ID: GuardWarehouseSize, Category: guardcfg.CatPacks, Default: "deny or ask", Pack: "data", Events: []string{"Bash", "PowerShell", `mcp__.*`}, Check: withScripts(checkWarehouseResize, resizeToolRe, func(c costSettings) bool {
		return c.Engine(engSnowflake) || c.Engine(engDatabricks) || c.Engine(engSynapse) || c.Engine(engFabric)
	}, "a warehouse CLI")})
	packs.RegisterGuard(packs.Guard{ID: GuardWarehouseQuery, Category: guardcfg.CatPacks, Default: "ask", Pack: "data", Events: shell, Check: withScripts(checkWarehouseQuery, queryToolRe, nil, "a warehouse query CLI")})
}
