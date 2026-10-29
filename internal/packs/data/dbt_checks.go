package data

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// Check ids of spec C §3.3.
const (
	idNewModelTests    = "data/dbt-new-model-tests"
	idContractEnforced = "data/dbt-contract-enforced"
	idIncremental      = "data/dbt-incremental"
	idSelectStar       = "data/sql-select-star"
	idHardcodedRel     = "data/sql-hardcoded-relation"
	idDBTParse         = "data/dbt-parse"
	idDBTBuildModified = "data/dbt-build-modified"
	idSQLFluff         = "data/sqlfluff"
)

// dbtRun is what every dbt gate needs: the config and the projects.
type dbtRun struct {
	in       *packs.Input
	cfg      dbtConfig
	projects []*dbtProject
	idx      map[*dbtProject]*modelIndex
}

func newDBTRun(in *packs.Input) (*dbtRun, error) {
	cfg := dbtSettings(in.Settings())
	ps, err := findProjects(in, cfg)
	if err != nil {
		return nil, err
	}
	return &dbtRun{in: in, cfg: cfg, projects: ps, idx: map[*dbtProject]*modelIndex{}}, nil
}

func (r *dbtRun) index(p *dbtProject) *modelIndex {
	if x, ok := r.idx[p]; ok {
		return x
	}
	x := indexModels(r.in, p)
	r.idx[p] = x
	return x
}

func failResult(err error) packs.Result {
	return packs.Result{Status: packs.Fail, Detail: err.Error()}
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// anyDBTFile is the cheap Applies of the dbt gates: a changed model,
// properties or project file.
func anyDBTFile(in *packs.Input) bool {
	return in.AnyChanged("*.sql", "*.py", "*.yml", "*.yaml")
}

func isPython(p string) bool { return strings.EqualFold(path.Ext(p), ".py") }

// ---- data/dbt-new-model-tests

func runNewModelTests(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	var contractPKs map[string][]string
	var fs []packs.Finding
	n := 0
	for _, c := range in.Changed {
		if c.Status != 'A' || ctx.Err() != nil {
			continue
		}
		p := projectOf(r.projects, c.Path)
		if p == nil {
			continue
		}
		if _, ok := p.modelFile(c.Path); !ok {
			continue
		}
		n++
		name := modelName(c.Path)
		m := r.index(p).byName[name]
		if m == nil {
			fs = append(fs, packs.Finding{File: c.Path, Line: 1, Level: packs.LevelError, Rule: "missing-yaml",
				Message: fmt.Sprintf("new model %s has no YAML entry: add it to a properties file with its columns and key tests", name)})
			continue
		}
		if m.Description == "" {
			fs = append(fs, packs.Finding{File: m.File, Line: m.Line, Level: packs.LevelWarn, Rule: "missing-description",
				Message: fmt.Sprintf("new model %s has no description", name)})
		}
		if hasTest(m.Tests, "unique_combination_of_columns") {
			continue
		}
		src, _ := in.ReadNew(c.Path, maxFileBytes)
		rel, _ := p.modelFile(c.Path)
		enforced := effectiveConfig(p, rel, m, src, isPython(c.Path)).enforced()
		pk, constrained := primaryKey(m)
		if len(pk) == 0 {
			if contractPKs == nil {
				contractPKs = contractPrimaryKeys(in)
			}
			pk = contractPKs[name]
		}
		if len(pk) == 0 {
			pk = idColumn(m)
		}
		if len(pk) == 0 {
			fs = append(fs, packs.Finding{File: m.File, Line: m.Line, Level: packs.LevelError, Rule: "missing-primary-key",
				Message: fmt.Sprintf("new model %s has no identifiable primary key: mark it with constraints: [{type: primary_key}], or name it id or %s_id, and test it with %s", name, name, strings.Join(r.cfg.NewModelTests, " + "))})
			continue
		}
		if constrained && enforced {
			continue // a primary_key constraint under an enforced contract
		}
		if len(pk) > 1 {
			fs = append(fs, packs.Finding{File: m.File, Line: m.Line, Level: packs.LevelError, Rule: "missing-key-tests",
				Message: fmt.Sprintf("new model %s has the composite key %s: add dbt_utils.unique_combination_of_columns", name, strings.Join(pk, ", "))})
			continue
		}
		col := column(m, pk[0])
		line, file := m.Line, m.File
		var have []string
		if col != nil {
			line, have = col.Line, col.Tests
			if enforced {
				for _, k := range col.Constraints {
					if k == "not_null" || k == "unique" {
						have = append(have, k)
					}
				}
			}
		}
		var missing []string
		for _, want := range r.cfg.NewModelTests {
			if !hasTest(have, want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			fs = append(fs, packs.Finding{File: file, Line: line, Level: packs.LevelError, Rule: "missing-key-tests",
				Message: fmt.Sprintf("new model %s: primary key %s has no %s test", name, pk[0], strings.Join(missing, " or "))})
		}
	}
	fs = newSuppressor(in, idNewModelTests).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(n, "new model"), Findings: fs}
}

// hasTest matches a generic test name, also package-qualified
// (dbt_utils.unique_combination_of_columns matches unique_combination_of_columns).
func hasTest(tests []string, want string) bool {
	for _, t := range tests {
		if t == want || strings.HasSuffix(t, "."+want) {
			return true
		}
	}
	return false
}

// primaryKey is the key from constraints: a column with a primary_key
// constraint, or the model-level primary_key constraint's columns.
func primaryKey(m *dbtModel) ([]string, bool) {
	for _, c := range m.Columns {
		for _, k := range c.Constraints {
			if k == "primary_key" {
				return []string{c.Name}, true
			}
		}
	}
	if len(m.PKColumns) > 0 {
		return m.PKColumns, true
	}
	return nil, false
}

// idColumn is the first column named id or <model>_id. It also takes the
// usual dbt key name <entity>_id, where entity is the model name without its
// layer prefix (stg_, base_, int_, fct_, dim_, mart_) in the singular:
// stg_orders -> order_id, dim_countries -> country_id.
func idColumn(m *dbtModel) []string {
	names := []string{"id", m.Name + "_id"}
	entity := m.Name
	for _, pre := range []string{"stg_", "base_", "int_", "fct_", "fact_", "dim_", "mart_"} {
		if strings.HasPrefix(strings.ToLower(entity), pre) {
			entity = entity[len(pre):]
			break
		}
	}
	if entity != "" {
		names = append(names, entity+"_id")
		switch {
		case strings.HasSuffix(entity, "ies"):
			names = append(names, strings.TrimSuffix(entity, "ies")+"y_id")
		case strings.HasSuffix(entity, "s"):
			names = append(names, strings.TrimSuffix(entity, "s")+"_id")
		}
	}
	for _, c := range m.Columns {
		for _, n := range names {
			if strings.EqualFold(c.Name, n) {
				return []string{c.Name}
			}
		}
	}
	return nil
}

func column(m *dbtModel, name string) *dbtColumn {
	for i := range m.Columns {
		if strings.EqualFold(m.Columns[i].Name, name) {
			return &m.Columns[i]
		}
	}
	return nil
}

// defaultContractPaths is packs.data.contracts.paths when unset (spec C §3.1).
var defaultContractPaths = []string{"contracts/**/*.odcs.yaml", "**/*.odcs.yml"}

// contractPrimaryKeys reads the ODCS contracts (contracts.paths) and returns
// model -> primaryKey properties (by primaryKeyPosition). An object maps to a
// model through customProperties dbtModel (on the object, else the contract),
// else its physicalName, else its name. Unreadable contracts are skipped
// (data/contract-valid reports them).
func contractPrimaryKeys(in *packs.Input) map[string][]string {
	globs := in.Settings().Strings("contracts.paths")
	if len(globs) == 0 {
		globs = defaultContractPaths
	}
	out := map[string][]string{}
	for _, f := range packs.WalkFiles(in.Root, packs.SuggestMaxFiles) {
		match := false
		for _, g := range globs {
			if packs.MatchGlob(g, f) {
				match = true
			}
		}
		if !match {
			continue
		}
		raw, err := in.ReadNew(f, maxFileBytes)
		if err != nil {
			continue
		}
		root, err := scan.ParseYAML(raw)
		if err != nil || !strings.EqualFold(scan.Scalar(scan.Get(root, "kind")), "DataContract") {
			continue
		}
		top := dbtModelProp(scan.Get(root, "customProperties"))
		for _, obj := range scan.Items(scan.Get(root, "schema")) {
			model := dbtModelProp(scan.Get(obj, "customProperties"))
			if model == "" {
				model = top
			}
			if model == "" {
				model = scan.Scalar(scan.Get(obj, "physicalName"))
			}
			if model == "" {
				model = scan.Scalar(scan.Get(obj, "name"))
			}
			type pkProp struct {
				name string
				pos  int
			}
			var pks []pkProp
			for _, prop := range scan.Items(scan.Get(obj, "properties")) {
				if strings.EqualFold(scan.Scalar(scan.Get(prop, "primaryKey")), "true") {
					pos, _ := strconv.Atoi(scan.Scalar(scan.Get(prop, "primaryKeyPosition")))
					pks = append(pks, pkProp{scan.Scalar(scan.Get(prop, "name")), pos})
				}
			}
			sort.SliceStable(pks, func(i, j int) bool { return pks[i].pos < pks[j].pos })
			if model != "" && len(pks) > 0 {
				if _, dup := out[model]; !dup {
					for _, k := range pks {
						out[model] = append(out[model], k.name)
					}
				}
			}
		}
	}
	return out
}

func dbtModelProp(n *yaml.Node) string {
	for _, it := range scan.Items(n) {
		if scan.Scalar(scan.Get(it, "property")) == "dbtModel" {
			return scan.Scalar(scan.Get(it, "value"))
		}
	}
	return ""
}

// ---- data/dbt-contract-enforced

// contractModel is one model the contract gate looks at.
type contractModel struct {
	project *dbtProject
	name    string
	file    string // .sql/.py repo path ("" when only a YAML entry exists)
}

func contractApplies(in *packs.Input) bool {
	return len(in.Settings().Strings("dbt.contract_paths")) > 0 && anyDBTFile(in)
}

func runContractEnforced(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	globs := r.cfg.ContractPaths
	todo := map[string]contractModel{}
	add := func(p *dbtProject, name, file string) {
		key := p.Dir + "\x00" + name
		if _, ok := todo[key]; !ok || todo[key].file == "" {
			todo[key] = contractModel{p, name, file}
		}
	}
	for _, c := range in.Changed {
		if c.Status == 'D' {
			continue
		}
		p := projectOf(r.projects, c.Path)
		if p == nil {
			continue
		}
		idx := r.index(p)
		switch {
		case c.Path == p.File():
			for name, f := range idx.modelFiles {
				if p.matchContract(globs, f) {
					add(p, name, f)
				}
			}
		case isModelPath(p, c.Path):
			if p.matchContract(globs, c.Path) {
				add(p, modelName(c.Path), c.Path)
			}
		case isYAML(c.Path):
			for _, m := range idx.files[c.Path] {
				f := idx.modelFiles[m.Name]
				if (f != "" && p.matchContract(globs, f)) || (f == "" && p.matchContract(globs, c.Path)) {
					add(p, m.Name, f)
				}
			}
		}
	}
	keys := make([]string, 0, len(todo))
	for k := range todo {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var fs []packs.Finding
	for _, k := range keys {
		if ctx.Err() != nil {
			break
		}
		cm := todo[k]
		m := r.index(cm.project).byName[cm.name]
		var src []byte
		rel := ""
		if cm.file != "" {
			src, _ = in.ReadNew(cm.file, maxFileBytes)
			rel, _ = cm.project.underModels(cm.file)
		} else if m != nil {
			rel, _ = cm.project.underModels(m.File)
		}
		cfg := effectiveConfig(cm.project, rel, m, src, isPython(cm.file))
		file, line := cm.file, 1
		if m != nil {
			file, line = m.File, m.Line
		}
		if !cfg.enforced() {
			fs = append(fs, packs.Finding{File: file, Line: line, Level: packs.LevelError, Rule: "contract-not-enforced",
				Message: fmt.Sprintf("model %s is under dbt.contract_paths but its contract is not enforced: set contract: {enforced: true} in its YAML config, config() or +contract in dbt_project.yml", cm.name)})
			continue
		}
		if m == nil || len(m.Columns) == 0 {
			fs = append(fs, packs.Finding{File: file, Line: line, Level: packs.LevelError, Rule: "enforced-no-columns",
				Message: fmt.Sprintf("model %s enforces a contract but lists no columns with data_type", cm.name)})
			continue
		}
		for _, col := range m.Columns {
			if col.DataType == "" {
				fs = append(fs, packs.Finding{File: m.File, Line: col.Line, Level: packs.LevelError, Rule: "column-no-data-type",
					Message: fmt.Sprintf("model %s enforces a contract but column %s has no data_type", cm.name, col.Name)})
			}
		}
	}
	fs = newSuppressor(in, idContractEnforced).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(len(todo), "contracted model"), Findings: fs}
}

func isModelPath(p *dbtProject, repoPath string) bool {
	_, ok := p.modelFile(repoPath)
	return ok
}

func isYAML(p string) bool {
	e := strings.ToLower(path.Ext(p))
	return e == ".yml" || e == ".yaml"
}

// ---- data/dbt-incremental

var (
	reLookback = regexp.MustCompile(`(?i)\binterval\b|dateadd|date_add|datetime_add|timestampadd|timestamp_add|timestamp_sub|date_sub|datetime_sub|datediff|timedelta|\blookback`)
	reIsIncr   = regexp.MustCompile(`^\{%-?\s*if\s+(?:not\s+)?is_incremental\s*\(\s*\)`)
	reEndIf    = regexp.MustCompile(`^\{%-?\s*endif\b`)
	reIfOpen   = regexp.MustCompile(`^\{%-?\s*if\b`)
)

func runIncremental(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	type target struct {
		p    *dbtProject
		file string
		m    *dbtModel
	}
	seen := map[string]bool{}
	var todo []target
	for _, c := range in.Changed {
		if c.Status == 'D' {
			continue
		}
		p := projectOf(r.projects, c.Path)
		if p == nil {
			continue
		}
		idx := r.index(p)
		if isModelPath(p, c.Path) {
			if !seen[c.Path] {
				seen[c.Path] = true
				todo = append(todo, target{p, c.Path, idx.byName[modelName(c.Path)]})
			}
		} else if isYAML(c.Path) {
			for _, m := range idx.files[c.Path] {
				if f := idx.modelFiles[m.Name]; f != "" && !seen[f] {
					seen[f] = true
					todo = append(todo, target{p, f, m})
				}
			}
		}
	}
	var fs []packs.Finding
	n := 0
	for _, t := range todo {
		if ctx.Err() != nil {
			break
		}
		src, err := in.ReadNew(t.file, maxFileBytes)
		if err != nil {
			continue
		}
		rel, _ := t.p.underModels(t.file)
		py := isPython(t.file)
		cfg := effectiveConfig(t.p, rel, t.m, src, py)
		if cfg.Materialized != "incremental" {
			continue
		}
		n++
		name := modelName(t.file)
		if !cfg.UniqueKey && cfg.Strategy != "append" {
			line := sqlConfig(src, py).Line
			if line == 0 {
				line = 1
			}
			fs = append(fs, packs.Finding{File: t.file, Line: line, Level: packs.LevelError, Rule: "incremental-no-unique-key",
				Message: fmt.Sprintf("incremental model %s has no unique_key and no incremental_strategy: append; a rerun duplicates rows", name)})
		}
		if !py {
			for _, l := range maxWithoutLookback(src) {
				fs = append(fs, packs.Finding{File: t.file, Line: l, Level: packs.LevelWarn, Rule: "incremental-no-lookback",
					Message: fmt.Sprintf("incremental model %s filters on max() with no lookback; late rows are dropped", name)})
			}
		}
	}
	fs = newSuppressor(in, idIncremental).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(n, "incremental model"), Findings: fs}
}

// maxWithoutLookback returns the lines of max( inside an is_incremental()
// block that has no lookback (interval, dateadd, timestamp_sub, ...).
func maxWithoutLookback(src []byte) []int {
	toks := scan.SQLTokens(src)
	var out []int
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Kind != scan.TokJinja || !reIsIncr.MatchString(t.Text) {
			continue
		}
		depth := 1
		j := i + 1
		var text strings.Builder
		var maxLines []int
		for ; j < len(toks) && depth > 0; j++ {
			u := toks[j]
			if u.Kind == scan.TokJinja {
				if reIfOpen.MatchString(u.Text) {
					depth++
				} else if reEndIf.MatchString(u.Text) {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			text.WriteString(u.Text)
			text.WriteByte(' ')
			if u.IsWord("max") && j+1 < len(toks) && toks[j+1].Kind == scan.TokPunct && strings.HasPrefix(toks[j+1].Text, "(") {
				maxLines = append(maxLines, u.Line)
			}
		}
		if len(maxLines) > 0 && !reLookback.MatchString(text.String()) {
			out = append(out, maxLines[0])
		}
		i = j
	}
	return out
}
