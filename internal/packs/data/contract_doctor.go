package data

// The contracts part of the data pack's doctor (spec C §7): data-contracts-none
// when models under dbt.contract_paths exist but no contract file does.

import (
	"path"
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
	projs := contractDoctorProjects(in, files)
	models := 0
	for _, f := range files {
		if isContractPath(s, f) {
			return nil
		}
		if strings.HasSuffix(f, ".sql") || strings.HasSuffix(f, ".py") {
			if contractUnderProject(f, projs, globs) {
				models++
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

// contractDoctorProjects is the dirs (slash, "" = repo root) that
// dbt.contract_paths is read relative to: the dbt project (spec C §3.1:
// dbt.project when set, else every dir holding a dbt_project.yml), plus the
// repo root so a repo-relative glob still works.
func contractDoctorProjects(in *packs.Input, files []string) []string {
	if p := strings.Trim(path.Clean(strings.ReplaceAll(in.Settings().String("dbt.project"), `\`, "/")), "/"); p != "" && p != "." {
		return []string{p, ""}
	}
	out := []string{""}
	for _, f := range files {
		if path.Base(f) == "dbt_project.yml" {
			if d := path.Dir(f); d != "." {
				out = append(out, d)
			}
		}
	}
	return out
}

// contractUnderProject reports whether f matches a glob relative to one of
// the dirs.
func contractUnderProject(f string, dirs, globs []string) bool {
	for _, d := range dirs {
		rel := f
		if d != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(f, d+"/"); !ok {
				continue
			}
		}
		for _, g := range globs {
			if packs.MatchGlob(g, rel) {
				return true
			}
		}
	}
	return false
}
