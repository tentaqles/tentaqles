package policy

import (
	"path"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// NOPLAN001 is a tracked plan, state, .terraform/ or dump file.
const NOPLAN001 = "NOPLAN001"

// PlanFileKind says why a tracked path must not be in git, or "".
// .terraform.lock.hcl is meant to be committed and is not matched.
func PlanFileKind(p string) string {
	p = strings.ToLower(CleanPath(p))
	base := path.Base(p)
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg == ".terraform" {
			return "a .terraform/ working directory file (providers, modules, backend state)"
		}
	}
	switch {
	case strings.HasSuffix(base, ".tfplan"):
		return "a saved Terraform/OpenTofu plan (it holds values in clear)"
	case strings.Contains(base, ".tfstate"):
		return "a Terraform/OpenTofu state file (it holds values in clear)"
	case strings.HasSuffix(base, ".bacpac"), strings.HasSuffix(base, ".dump"), strings.HasSuffix(base, ".pgdump"),
		strings.HasPrefix(base, "tq_drill_"):
		return "a database dump (it holds real data)"
	}
	return ""
}

// NoPlanFiles fails on every tracked plan, state, .terraform/ or dump file.
func NoPlanFiles(in *packs.Input) packs.Result {
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	if in.Git == nil {
		res.Status, res.Detail = packs.Fail, "no git reader: cannot list the tracked files"
		return res
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		res.Status, res.Detail = packs.Fail, "cannot list the tracked files"
		return res
	}
	for _, f := range files {
		if why := PlanFileKind(f); why != "" {
			res.Findings = append(res.Findings, packs.Finding{File: CleanPath(f), Level: packs.LevelError, Rule: NOPLAN001,
				Message: "tracked " + why + "; remove it with `git rm --cached` and add the pattern to .gitignore"})
		}
	}
	if n := len(res.Findings); n > 0 {
		res.Status = packs.Fail
		res.Detail = plural(n, "tracked plan, state or dump file")
	} else {
		res.Detail = plural(len(files), "tracked file") + " checked, none is a plan, state or dump"
	}
	return res
}

func plural(n int, what string) string {
	s := strconv.Itoa(n) + " " + what
	if n != 1 {
		s += "s"
	}
	return s
}
