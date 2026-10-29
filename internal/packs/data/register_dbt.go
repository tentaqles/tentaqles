package data

import (
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// The dbt gates of the data pack (spec C §3.3) and the guards
// tq/dbt-prod-target and tq/dbt-full-refresh (§3.6, §5.3).
func init() {
	packs.RegisterPack(packs.Pack{
		Name:       "data",
		ConfigKeys: map[string]func(yaml.Node) error{"dbt": validateDBT},
		Merge: map[string]packs.MergeRule{
			// The repo file may add contracted folders, required key tests
			// and production targets; it can never drop one. The other dbt
			// keys are trusted (added when the manifest lacks them, never
			// changed).
			"dbt.contract_paths":  packs.Union(),
			"dbt.new_model_tests": packs.Union(),
			"dbt.prod_targets":    packs.Union(),
			"dbt.project":         packs.TrustedAdd(),
			"dbt.build":           packs.TrustedAdd(),
			"dbt.dev_target":      packs.TrustedAdd(),
			"dbt.state":           packs.TrustedAdd(),
		},
		Hints:  []packs.Hint{{Glob: "dbt_project.yml", Why: "a dbt project"}},
		Doctor: dbtDoctor,
	})

	packs.Register(packs.Check{ID: idNewModelTests, Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("*.sql", "*.py") }, Run: runNewModelTests})
	packs.Register(packs.Check{ID: idContractEnforced, Pack: "data", Kind: packs.Builtin,
		Applies: contractApplies, Run: runContractEnforced})
	packs.Register(packs.Check{ID: idIncremental, Pack: "data", Kind: packs.Builtin,
		Applies: anyDBTFile, Run: runIncremental})
	packs.Register(packs.Check{ID: idSelectStar, Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("*.sql") }, Run: runSelectStar})
	packs.Register(packs.Check{ID: idHardcodedRel, Pack: "data", Kind: packs.Builtin,
		Applies: func(in *packs.Input) bool { return in.AnyChanged("*.sql") }, Run: runHardcodedRelation})

	packs.Register(packs.Check{ID: idDBTParse, Pack: "data", Kind: packs.External, Tool: "dbt", Env: dbtEnv,
		MinVersion: dbtMinVersion, Timeout: dbtParseTimeout, Applies: anyDBTFile, Run: runDBTParse})
	packs.Register(packs.Check{ID: idDBTBuildModified, Pack: "data", Kind: packs.External, Tool: "dbt", Env: dbtEnv,
		MinVersion: dbtMinVersion, Timeout: dbtBuildTimeout, Applies: buildApplies, Run: runDBTBuildModified})
	packs.Register(packs.Check{ID: idSQLFluff, Pack: "data", Kind: packs.External, Tool: "sqlfluff", Env: sqlfluffEnv,
		Timeout: sqlfluffTimeout, Applies: sqlfluffApplies, Run: runSqlfluff})

	packs.RegisterGuard(packs.Guard{ID: guardProdTarget, Category: guardcfg.CatPacks, Default: "deny", Pack: "data", Events: []string{"Bash", "PowerShell"}, Check: withScripts(checkProdTarget, dbtToolRe, nil, "dbt")})
	packs.RegisterGuard(packs.Guard{ID: guardFullRefresh, Category: guardcfg.CatPacks, Default: "ask", Pack: "data", Events: []string{"Bash", "PowerShell"}, Check: withScripts(checkFullRefresh, dbtToolRe, nil, "dbt")})
}
