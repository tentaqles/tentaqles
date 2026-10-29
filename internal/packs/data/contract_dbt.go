package data

// data/contract-dbt-sync (spec C §3.2): a contract that names a dbt model
// (customProperties: [{property: dbtModel, value: <model>}]) agrees with the
// model's YAML columns: every property is a column, data types match, and a
// required property has a not_null test or constraint.

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// contractDBTMaxFiles bounds the dbt YAML files read per project.
const contractDBTMaxFiles = 2000

type contractDBTColumn struct {
	Name, DataType string
	NotNull        bool
}

type contractDBTModel struct {
	File    string
	Columns []contractDBTColumn
}

// contractDBTApplies: a changed contract, or a changed YAML file (a dbt
// schema file can drift from a contract that did not change).
func contractDBTApplies(in *packs.Input) bool {
	return anyContractChange(in) || in.AnyChanged("*.yml", "*.yaml")
}

func runContractDBTSync(ctx context.Context, in *packs.Input) packs.Result {
	s := in.Settings()
	var targets []string
	dbtChanged := false
	for _, p := range in.ChangedPaths() {
		if !isContractPath(s, p) && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) {
			dbtChanged = true
		}
	}
	if in.All || dbtChanged {
		targets = allContractFiles(in)
	} else {
		targets = contractTargets(in)
	}
	files := contractRepoFiles(in)
	type pair struct {
		c     *odcsContract
		o     *odcsObject
		model string
		line  int
	}
	var pairs []pair
	for _, p := range targets {
		c, err := readContract(in, p)
		if err != nil || !c.isContractDoc() {
			continue
		}
		for _, o := range c.Objects {
			switch {
			case o.DBTModel != "":
				pairs = append(pairs, pair{c, o, o.DBTModel, o.DBTModelLine})
			case c.DBTModel != "" && len(c.Objects) == 1:
				pairs = append(pairs, pair{c, o, c.DBTModel, c.DBTModelLine})
			}
		}
		if c.DBTModel != "" && len(c.Objects) > 1 {
			pairs = append(pairs, pair{c, nil, c.DBTModel, c.DBTModelLine})
		}
	}
	if len(pairs) == 0 {
		return packs.Result{Status: packs.OK, Detail: "no contract names a dbt model"}
	}
	var fs []packs.Finding
	models := map[string]map[string]*contractDBTModel{} // project dir -> model name -> model
	for _, pr := range pairs {
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out"}
		}
		if pr.o == nil {
			fs = append(fs, packs.Finding{File: pr.c.Path, Line: pr.line, Level: packs.LevelError, Rule: "dbt-model-ambiguous",
				Message: "a contract-level dbtModel needs exactly one schema object; set dbtModel on each object"})
			continue
		}
		proj, ok := contractDBTProject(in, files, pr.c.Path)
		if !ok {
			fs = append(fs, packs.Finding{File: pr.c.Path, Line: pr.line, Level: packs.LevelError, Rule: "dbt-project-missing",
				Message: "the contract names dbt model " + pr.model + " but no dbt_project.yml was found (set packs.data.dbt.project)"})
			continue
		}
		if models[proj] == nil {
			models[proj] = contractDBTModels(in, files, proj)
		}
		m := models[proj][pr.model]
		if m == nil {
			fs = append(fs, packs.Finding{File: pr.c.Path, Line: pr.line, Level: packs.LevelError, Rule: "dbt-model-missing",
				Message: fmt.Sprintf("dbt model %s has no YAML entry under the model-paths of %s", pr.model, contractDir(proj))})
			continue
		}
		fs = append(fs, contractCompareModel(pr.c, pr.o, pr.model, m)...)
	}
	r := packs.Result{Status: packs.OK, Findings: fs}
	if errs, _ := r.Counts(); errs > 0 {
		r.Status = packs.Fail
		r.Detail = fmt.Sprintf("%s, %s", contractPlural(len(pairs), "contract object mapped to dbt"), contractPlural(errs, "mismatch"))
	} else {
		r.Detail = contractPlural(len(pairs), "contract object") + " in sync with dbt"
	}
	return r
}

func contractDir(d string) string {
	if d == "" || d == "." {
		return "the repo root"
	}
	return d
}

func contractCompareModel(c *odcsContract, o *odcsObject, model string, m *contractDBTModel) []packs.Finding {
	var fs []packs.Finding
	cols := map[string]contractDBTColumn{}
	for _, col := range m.Columns {
		cols[strings.ToLower(col.Name)] = col
	}
	seen := map[string]bool{}
	for _, p := range o.Props {
		name := p.column()
		key := strings.ToLower(name)
		seen[key] = true
		col, ok := cols[key]
		if !ok {
			fs = append(fs, packs.Finding{File: c.Path, Line: p.Line, Level: packs.LevelError, Rule: "dbt-column-missing",
				Message: fmt.Sprintf("property %s has no column in dbt model %s (%s)", name, model, m.File)})
			continue
		}
		if p.PhysicalType != "" && col.DataType != "" && !contractSameType(p.PhysicalType, col.DataType) {
			fs = append(fs, packs.Finding{File: c.Path, Line: p.lineOf("physicalType"), Level: packs.LevelError, Rule: "dbt-type-mismatch",
				Message: fmt.Sprintf("property %s is %s in the contract but %s in dbt model %s", name, p.PhysicalType, col.DataType, model)})
		}
		switch {
		case p.Required && !col.NotNull:
			fs = append(fs, packs.Finding{File: c.Path, Line: p.lineOf("required"), Level: packs.LevelError, Rule: "dbt-not-null-missing",
				Message: fmt.Sprintf("property %s is required but dbt model %s has no not_null test or constraint on it", name, model)})
		case !p.Required && col.NotNull:
			fs = append(fs, packs.Finding{File: c.Path, Line: p.Line, Level: packs.LevelWarn, Rule: "dbt-not-null-unstated",
				Message: fmt.Sprintf("dbt model %s tests %s not_null but the contract does not mark it required", model, name)})
		}
	}
	for _, col := range m.Columns {
		if !seen[strings.ToLower(col.Name)] {
			fs = append(fs, packs.Finding{File: c.Path, Line: o.Line, Level: packs.LevelWarn, Rule: "dbt-column-uncontracted",
				Message: fmt.Sprintf("dbt model %s has column %s that the contract does not list", model, col.Name)})
		}
	}
	return fs
}

// contractRepoFiles is the tracked file list plus changed paths, minus
// deletions.
func contractRepoFiles(in *packs.Input) []string {
	set := map[string]bool{}
	if in.Git != nil {
		if fl, err := in.Git.ListFiles(); err == nil {
			for _, f := range fl {
				set[f] = true
			}
		}
	}
	for _, c := range in.Changed {
		if c.Status == 'D' {
			delete(set, c.Path)
		} else {
			set[c.Path] = true
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// contractDBTProject finds the dbt project dir (slash, "" = root): the
// packs.data.dbt.project setting, else the dbt_project.yml nearest the
// contract (the deepest one whose dir holds it), else the shallowest one.
func contractDBTProject(in *packs.Input, files []string, contract string) (string, bool) {
	if p := strings.Trim(path.Clean(strings.ReplaceAll(in.Settings().String("dbt.project"), `\`, "/")), "/"); p != "" && p != "." {
		for _, f := range files {
			if f == p+"/dbt_project.yml" {
				return p, true
			}
		}
		return "", false
	}
	var dirs []string
	for _, f := range files {
		if path.Base(f) == "dbt_project.yml" {
			d := path.Dir(f)
			if d == "." {
				d = ""
			}
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		return "", false
	}
	best, bestLen := "", -1
	for _, d := range dirs {
		if d == "" || strings.HasPrefix(contract, d+"/") {
			if len(d) > bestLen {
				best, bestLen = d, len(d)
			}
		}
	}
	if bestLen >= 0 {
		return best, true
	}
	sort.Slice(dirs, func(i, j int) bool {
		a, b := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if a != b {
			return a < b
		}
		return dirs[i] < dirs[j]
	})
	return dirs[0], true
}

// contractDBTModels reads the models of every YAML file under the project's
// model-paths.
func contractDBTModels(in *packs.Input, files []string, proj string) map[string]*contractDBTModel {
	join := func(rel string) string {
		rel = strings.Trim(strings.ReplaceAll(rel, `\`, "/"), "/")
		if proj == "" {
			return rel
		}
		return proj + "/" + rel
	}
	modelPaths := []string{"models"}
	if b, err := in.ReadNew(join("dbt_project.yml"), 1<<20); err == nil {
		if n, err := scan.ParseYAML(b); err == nil {
			mp := scan.Strings(scan.Get(n, "model-paths"))
			if len(mp) == 0 {
				mp = scan.Strings(scan.Get(n, "source-paths")) // dbt < 1.0
			}
			if len(mp) > 0 {
				modelPaths = mp
			}
		}
	}
	out := map[string]*contractDBTModel{}
	read := 0
	for _, f := range files {
		if !(strings.HasSuffix(f, ".yml") || strings.HasSuffix(f, ".yaml")) {
			continue
		}
		under := false
		for _, mp := range modelPaths {
			if strings.HasPrefix(f, join(mp)+"/") {
				under = true
			}
		}
		if !under {
			continue
		}
		if read++; read > contractDBTMaxFiles {
			break
		}
		b, err := in.ReadNew(f, contractMaxBytes)
		if err != nil {
			continue
		}
		n, err := scan.ParseYAML(b)
		if err != nil {
			continue
		}
		for _, m := range scan.Items(scan.Get(n, "models")) {
			name := scan.Scalar(scan.Get(m, "name"))
			if name == "" || out[name] != nil {
				continue
			}
			mod := &contractDBTModel{File: f}
			modelNotNull := contractDBTModelNotNull(m)
			for _, col := range scan.Items(scan.Get(m, "columns")) {
				name := scan.Scalar(scan.Get(col, "name"))
				mod.Columns = append(mod.Columns, contractDBTColumn{
					Name:     name,
					DataType: scan.Scalar(scan.Get(col, "data_type")),
					NotNull:  contractDBTNotNull(col) || modelNotNull[strings.ToLower(name)],
				})
			}
			out[name] = mod
		}
	}
	return out
}

// contractDBTModelNotNull is the columns (lower case) a model-level
// constraint makes not null: constraints: [{type: not_null | primary_key,
// columns: [...]}].
func contractDBTModelNotNull(m *yaml.Node) map[string]bool {
	out := map[string]bool{}
	for _, c := range scan.Items(scan.Get(m, "constraints")) {
		if t := scan.Scalar(scan.Get(c, "type")); t == "not_null" || t == "primary_key" {
			for _, col := range scan.Strings(scan.Get(c, "columns")) {
				out[strings.ToLower(col)] = true
			}
		}
	}
	return out
}

// contractDBTNotNull: a not_null (or primary_key) constraint, or a not_null
// entry in data_tests: or tests:.
func contractDBTNotNull(col *yaml.Node) bool {
	for _, c := range scan.Items(scan.Get(col, "constraints")) {
		if t := scan.Scalar(scan.Get(c, "type")); t == "not_null" || t == "primary_key" {
			return true
		}
	}
	for _, key := range []string{"data_tests", "tests"} {
		for _, t := range scan.Items(scan.Get(col, key)) {
			if t.Kind == yaml.ScalarNode && t.Value == "not_null" {
				return true
			}
			if t.Kind == yaml.MappingNode && scan.Get(t, "not_null") != nil {
				return true
			}
		}
	}
	return false
}
