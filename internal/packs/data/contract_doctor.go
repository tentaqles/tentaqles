package data

// The contracts part of the data pack's doctor (spec C §7): data-contracts-none
// when models under dbt.contract_paths exist but no contract file does.

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
)

func contractDoctor(in *packs.Input) []doctor.Finding {
	globs := in.Settings().Strings("dbt.contract_paths")
	if len(globs) == 0 {
		return nil
	}
	var files []string
	if in.Git != nil {
		files, _ = in.Git.ListFiles()
	}
	if files == nil && in.Root != "" {
		files = packs.WalkFiles(in.Root, packs.SuggestMaxFiles)
	}
	s := in.Settings()
	models := 0
	for _, f := range files {
		if isContractPath(s, f) {
			return nil
		}
		if strings.HasSuffix(f, ".sql") || strings.HasSuffix(f, ".py") {
			for _, g := range globs {
				if packs.MatchGlob(g, f) {
					models++
					break
				}
			}
		}
	}
	if models == 0 {
		return nil
	}
	return []doctor.Finding{{Level: "info", Code: "data-contracts-none",
		Msg: contractPlural(models, "model") + " under packs.data.dbt.contract_paths but no contract file matches packs.data.contracts.paths",
		Fix: "write ODCS contracts for those models with the data-contract skill (template plugin/templates/process/data-contract.odcs.yaml)"}}
}
