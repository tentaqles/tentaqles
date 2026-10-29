package data

import (
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
		Merge: map[string]packs.MergeKind{
			"engines":                      packs.MergeUnion,
			"cost.*.max_bytes":             packs.MergeMin,
			"cost.*.statement_timeout_s":   packs.MergeMin,
			"cost.databricks.max_workers":  packs.MergeMin,
			"cost.synapse.max_spark_nodes": packs.MergeMin,
			"cost.postgres.max_plan_cost":  packs.MergeMin,
			"cost.estimate_in_dod":         packs.MergeOr,
			"cost.databricks.warehouse_id": packs.MergeTrusted,
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
		Applies: costEstimateApplies, Run: runCostEstimate})
	shell := []string{"Bash", "PowerShell"}
	packs.RegisterGuard(packs.Guard{ID: GuardBQUnbounded, Pack: "data", Events: shell, Check: checkBQUnbounded})
	packs.RegisterGuard(packs.Guard{ID: GuardWarehouseSize, Pack: "data", Events: []string{"Bash", "PowerShell", `mcp__.*`}, Check: checkWarehouseResize})
	packs.RegisterGuard(packs.Guard{ID: GuardWarehouseQuery, Pack: "data", Events: shell, Check: checkWarehouseQuery})
}
