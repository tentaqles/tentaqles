package data

// Registration of the data-contract part of the data pack (spec C §3.2,
// workstream D1): config key contracts, the data_contract flag, detection
// hints, the contracts doctor probe and the contract checks.

import (
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func init() {
	packs.RegisterPack(packs.Pack{
		Name:       "data",
		ConfigKeys: map[string]func(yaml.Node) error{"contracts": validateContractsKey},
		Merge: map[string]packs.MergeRule{
			"contracts.paths":       packs.Union(),
			"contracts.consumers.*": packs.Union(),
			// warn | breaking: the stricter wins; strictest default.
			"contracts.enum_added": packs.Ordered("breaking", "warn", "breaking"),
		},
		Flags: []packs.Flag{
			{Name: "data_contract", Reviewers: []string{"pipeline-reviewer"}, Raise: anyContractChange},
		},
		Hints: []packs.Hint{
			{Glob: "*.odcs.yaml", Why: "ODCS data contracts"},
			{Glob: "*.odcs.yml", Why: "ODCS data contracts"},
		},
		Doctor: contractDoctor,
	})
	packs.Register(packs.Check{ID: "data/contract-valid", Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.All || anyContractChange(in) }, Run: runContractValid})
	packs.Register(packs.Check{ID: "data/contract-breaking", Pack: "data", Kind: packs.Builtin,
		Applies: anyContractChange, Run: runContractBreaking})
	packs.Register(packs.Check{ID: "data/contract-pii", Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.All || anyContractChange(in) }, Run: runContractPII})
	packs.Register(packs.Check{ID: "data/contract-dbt-sync", Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.All || contractDBTApplies(in) }, Run: runContractDBTSync})
	packs.Register(packs.Check{ID: "data/datacontract-lint", Pack: "data", Kind: packs.External, Tool: contractTool,
		Env: contractEnv, Timeout: contractToolTimeout, Applies: func(in *packs.Input) bool { return in.All || anyContractChange(in) }, Run: runDatacontractLint})
	packs.Register(packs.Check{ID: "data/datacontract-breaking", Pack: "data", Kind: packs.External, Tool: contractTool,
		Env: contractEnv, Timeout: contractToolTimeout, Applies: anyContractChange, Run: runDatacontractBreaking})
}
