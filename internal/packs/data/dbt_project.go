package data

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// Bounds of the dbt project walk (built-in checks run within 10 s).
const (
	maxYAMLFiles = 5000
	maxFileBytes = 1 << 20
)

// targetRe is a dbt target name.
var targetRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// dbtProject is one dbt project of the repository.
type dbtProject struct {
	Dir        string   // slash, repo relative; "" is the repo root
	Name       string   // name: in dbt_project.yml
	Profile    string   // profile: in dbt_project.yml
	ModelPaths []string // model-paths, relative to Dir (default models)
	Line       int      // line of the name: key (for findings)
	models     *yaml.Node
}

// File is dbt_project.yml's repo path.
func (p *dbtProject) File() string { return joinRel(p.Dir, "dbt_project.yml") }

func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// rel returns path relative to the project dir.
func (p *dbtProject) rel(repoPath string) (string, bool) {
	if p.Dir == "" {
		return repoPath, true
	}
	if strings.HasPrefix(repoPath, p.Dir+"/") {
		return repoPath[len(p.Dir)+1:], true
	}
	return "", false
}

// modelFile reports whether repoPath is a model (.sql or .py under a
// model path) and returns its path below that model path.
func (p *dbtProject) modelFile(repoPath string) (string, bool) {
	ext := strings.ToLower(path.Ext(repoPath))
	if ext != ".sql" && ext != ".py" {
		return "", false
	}
	return p.underModels(repoPath)
}

// underModels returns repoPath below its model path.
func (p *dbtProject) underModels(repoPath string) (string, bool) {
	r, ok := p.rel(repoPath)
	if !ok {
		return "", false
	}
	for _, mp := range p.ModelPaths {
		if strings.HasPrefix(r, mp+"/") {
			return r[len(mp)+1:], true
		}
	}
	return "", false
}

// matchContract reports whether repoPath matches one of the contract_paths
// globs, relative to the project dir or to the repo root.
func (p *dbtProject) matchContract(globs []string, repoPath string) bool {
	r, _ := p.rel(repoPath)
	for _, g := range globs {
		if packs.MatchGlob(g, repoPath) || (r != "" && packs.MatchGlob(g, r)) {
			return true
		}
	}
	return false
}

// loadProject reads dir/dbt_project.yml (the new side).
func loadProject(in *packs.Input, dir string) (*dbtProject, error) {
	p := &dbtProject{Dir: dir}
	raw, err := in.ReadNew(p.File(), maxFileBytes)
	if err != nil {
		return nil, err
	}
	root, err := scan.ParseYAML(raw)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %v", p.File(), err)
	}
	p.Name = scan.Scalar(scan.Get(root, "name"))
	p.Line = scan.Line(scan.Get(root, "name"))
	p.Profile = scan.Scalar(scan.Get(root, "profile"))
	for _, mp := range scan.Strings(scan.Get(root, "model-paths")) {
		mp = strings.Trim(strings.ReplaceAll(mp, `\`, "/"), "/")
		mp = strings.TrimPrefix(mp, "./")
		if mp != "" && !strings.Contains(mp, "..") {
			p.ModelPaths = append(p.ModelPaths, mp)
		}
	}
	if len(p.ModelPaths) == 0 {
		p.ModelPaths = []string{"models"}
	}
	p.models = scan.Get(root, "models")
	return p, nil
}

// findProjects returns the dbt projects the changed files belong to: the
// configured dbt.project, or else the nearest dbt_project.yml above each
// changed path.
func findProjects(in *packs.Input, cfg dbtConfig) ([]*dbtProject, error) {
	if cfg.Project != "" {
		p, err := loadProject(in, cfg.Project)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("packs.data.dbt.project %q has no dbt_project.yml", cfg.Project)
			}
			return nil, err
		}
		return []*dbtProject{p}, nil
	}
	exists := map[string]bool{}
	has := func(dir string) bool {
		if v, ok := exists[dir]; ok {
			return v
		}
		fi, err := os.Stat(filepath.Join(in.Root, filepath.FromSlash(joinRel(dir, "dbt_project.yml"))))
		v := err == nil && fi.Mode().IsRegular()
		exists[dir] = v
		return v
	}
	dirs := map[string]bool{}
	for _, c := range in.Changed {
		d := path.Dir(c.Path)
		for depth := 0; depth < 32; depth++ {
			if d == "." || d == "/" {
				d = ""
			}
			if has(d) {
				dirs[d] = true
				break
			}
			if d == "" {
				break
			}
			d = path.Dir(d)
		}
	}
	keys := make([]string, 0, len(dirs))
	for d := range dirs {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	var out []*dbtProject
	for _, d := range keys {
		p, err := loadProject(in, d)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// projectOf returns the project whose dir is the nearest ancestor of
// repoPath.
func projectOf(projects []*dbtProject, repoPath string) *dbtProject {
	var best *dbtProject
	for _, p := range projects {
		if _, ok := p.rel(repoPath); ok && (best == nil || len(p.Dir) > len(best.Dir)) {
			best = p
		}
	}
	return best
}

// modelConfig is the part of a model's config the gates read.
type modelConfig struct {
	Materialized string
	UniqueKey    bool
	Strategy     string
	PartitionBy  bool
	Enforced     *bool
	Line         int // where it was set, for findings (0: unknown)
}

// over returns c with the values set in o laid on top.
func (c modelConfig) over(o modelConfig) modelConfig {
	if o.Materialized != "" {
		c.Materialized = o.Materialized
	}
	if o.UniqueKey {
		c.UniqueKey = true
	}
	if o.Strategy != "" {
		c.Strategy = o.Strategy
	}
	if o.PartitionBy {
		c.PartitionBy = true
	}
	if o.Enforced != nil {
		c.Enforced = o.Enforced
	}
	return c
}

func (c modelConfig) enforced() bool { return c.Enforced != nil && *c.Enforced }

// dbtColumn is one column of a model's YAML entry.
type dbtColumn struct {
	Name        string
	Line        int
	DataType    string
	Tests       []string // data_tests: and tests: names
	Constraints []string // constraints[].type
}

// dbtModel is one entry of a models: list in a properties file.
type dbtModel struct {
	Name        string
	File        string // repo path of the YAML file
	Line        int
	Description string
	Config      modelConfig
	Columns     []dbtColumn
	Tests       []string // model-level test names
	PKColumns   []string // model-level primary_key constraint columns
}

// modelIndex is every model entry of a project's properties files.
type modelIndex struct {
	byName     map[string]*dbtModel
	files      map[string][]*dbtModel // yml repo path -> entries
	modelFiles map[string]string      // model name -> .sql/.py repo path
}

// indexModels reads the .yml/.yaml files under the project's model paths.
// A file that does not parse is skipped here (dbt-parse reports it).
func indexModels(in *packs.Input, p *dbtProject) *modelIndex {
	idx := &modelIndex{byName: map[string]*dbtModel{}, files: map[string][]*dbtModel{}, modelFiles: map[string]string{}}
	n := 0
	for _, mp := range p.ModelPaths {
		base := filepath.Join(in.Root, filepath.FromSlash(joinRel(p.Dir, mp)))
		_ = filepath.WalkDir(base, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case "target", "dbt_packages", "node_modules", ".git", ".venv", "logs":
					return filepath.SkipDir
				}
				return nil
			}
			if n++; n > maxYAMLFiles {
				return filepath.SkipAll
			}
			rel, err := filepath.Rel(in.Root, fp)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			ext := strings.ToLower(filepath.Ext(fp))
			if ext == ".sql" || ext == ".py" {
				if _, dup := idx.modelFiles[modelName(rel)]; !dup {
					idx.modelFiles[modelName(rel)] = rel
				}
				return nil
			}
			if ext != ".yml" && ext != ".yaml" {
				return nil
			}
			raw, err := in.ReadNew(rel, maxFileBytes)
			if err != nil {
				return nil
			}
			for _, m := range parseModelYAML(raw, rel) {
				idx.files[rel] = append(idx.files[rel], m)
				if _, dup := idx.byName[m.Name]; !dup {
					idx.byName[m.Name] = m
				}
			}
			return nil
		})
	}
	return idx
}

// parseModelYAML returns the models: entries of a properties file.
func parseModelYAML(raw []byte, file string) []*dbtModel {
	root, err := scan.ParseYAML(raw)
	if err != nil || root == nil {
		return nil
	}
	var out []*dbtModel
	for _, it := range scan.Items(scan.Get(root, "models")) {
		name := scan.Scalar(scan.Get(it, "name"))
		if name == "" {
			continue
		}
		m := &dbtModel{Name: name, File: file, Line: scan.Line(scan.Get(it, "name")),
			Description: strings.TrimSpace(scan.Scalar(scan.Get(it, "description")))}
		m.Config = yamlConfig(scan.Get(it, "config"))
		m.Tests = append(testNames(scan.Get(it, "data_tests")), testNames(scan.Get(it, "tests"))...)
		for _, c := range scan.Items(scan.Get(it, "constraints")) {
			if strings.EqualFold(scan.Scalar(scan.Get(c, "type")), "primary_key") {
				m.PKColumns = append(m.PKColumns, scan.Strings(scan.Get(c, "columns"))...)
			}
		}
		for _, c := range scan.Items(scan.Get(it, "columns")) {
			cn := scan.Scalar(scan.Get(c, "name"))
			if cn == "" {
				continue
			}
			col := dbtColumn{Name: cn, Line: scan.Line(scan.Get(c, "name")),
				DataType: strings.TrimSpace(scan.Scalar(scan.Get(c, "data_type")))}
			col.Tests = append(testNames(scan.Get(c, "data_tests")), testNames(scan.Get(c, "tests"))...)
			for _, k := range scan.Items(scan.Get(c, "constraints")) {
				if t := strings.ToLower(scan.Scalar(scan.Get(k, "type"))); t != "" {
					col.Constraints = append(col.Constraints, t)
				}
			}
			m.Columns = append(m.Columns, col)
		}
		out = append(out, m)
	}
	return out
}

// testNames returns the generic test names of a data_tests:/tests: list:
// a scalar item is its name, a mapping item its first key.
func testNames(n *yaml.Node) []string {
	var out []string
	for _, it := range scan.Items(n) {
		switch it.Kind {
		case yaml.ScalarNode:
			out = append(out, it.Value)
		case yaml.MappingNode:
			if len(it.Content) >= 2 {
				k := it.Content[0].Value
				if k == "name" || k == "test_name" {
					// the long form: {name: ..., test_name: unique}
					if tn := scan.Scalar(scan.Get(it, "test_name")); tn != "" {
						k = tn
					}
				}
				out = append(out, k)
			}
		}
	}
	return out
}

// yamlConfig reads a config: mapping (YAML properties or dbt_project.yml
// folder config, where keys may carry a + prefix).
func yamlConfig(n *yaml.Node) modelConfig {
	var c modelConfig
	for _, kv := range scan.Entries(n) {
		switch strings.TrimPrefix(kv.Key, "+") {
		case "materialized":
			c.Materialized = strings.ToLower(scan.Scalar(kv.Value))
			c.Line = kv.Line
		case "unique_key":
			c.UniqueKey = c.UniqueKey || yamlSet(kv.Value)
		case "partition_by":
			c.PartitionBy = c.PartitionBy || yamlSet(kv.Value)
		case "incremental_strategy":
			c.Strategy = strings.ToLower(scan.Scalar(kv.Value))
		case "contract":
			v := strings.ToLower(scan.Scalar(scan.Get(kv.Value, "enforced")))
			if v == "true" || v == "false" {
				b := v == "true"
				c.Enforced = &b
				c.Line = kv.Line
			}
		}
	}
	return c
}

// nullValue is a Jinja/Python/YAML null or an empty literal.
func nullValue(v string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(v), `'"`)) {
	case "", "none", "null", "~", "[]":
		return true
	}
	return false
}

// yamlSet reports a YAML config value that is set: a non-null scalar, or a
// non-empty list or mapping.
func yamlSet(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.ScalarNode {
		return !nullValue(n.Value) || n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 && n.Value != ""
	}
	return len(n.Content) > 0
}

// folderConfig is the dbt_project.yml models: config that applies to a
// model at rel (its path below the model path): models.<project>.<dir>...
func (p *dbtProject) folderConfig(rel string) modelConfig {
	var c modelConfig
	if p.models == nil {
		return c
	}
	node := scan.Get(p.models, p.Name)
	if node == nil {
		return c
	}
	c = c.over(yamlConfig(configKeys(node)))
	segs := strings.Split(path.Dir(rel), "/")
	for _, s := range segs {
		if s == "." || s == "" {
			continue
		}
		node = scan.Get(node, s)
		if node == nil {
			break
		}
		c = c.over(yamlConfig(configKeys(node)))
	}
	return c
}

// configKeys keeps the config keys of a dbt_project.yml folder node: the
// +keys, and the bare config names dbt also accepts there.
func configKeys(n *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.MappingNode}
	for _, kv := range scan.Entries(n) {
		k := kv.Key
		if strings.HasPrefix(k, "+") || ((k == "materialized" || k == "incremental_strategy" || k == "unique_key") && kv.Value.Kind == yaml.ScalarNode) {
			out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k, Line: kv.Line}, kv.Value)
		}
	}
	return out
}

var (
	reConfigCall  = regexp.MustCompile(`(?s)\bconfig\s*\(`)
	reMaterial    = regexp.MustCompile(`\bmaterialized\s*=\s*['"]([A-Za-z_]+)['"]`)
	reUniqueKey   = regexp.MustCompile(`\bunique_key\s*=\s*([^\s,)]+)`)
	rePartitionBy = regexp.MustCompile(`\bpartition_by\s*=\s*([^\s,)]+)`)
	reStrategy    = regexp.MustCompile(`\bincremental_strategy\s*=\s*['"]([A-Za-z_+]+)['"]`)
	reContractOn  = regexp.MustCompile(`(?s)\bcontract\s*=\s*(?:\{[^}]*['"]?enforced['"]?\s*:\s*(?i:true)|dict\s*\([^)]*enforced\s*=\s*(?i:true))`)
	reContractOff = regexp.MustCompile(`(?s)\bcontract\s*=\s*(?:\{[^}]*['"]?enforced['"]?\s*:\s*(?i:false)|dict\s*\([^)]*enforced\s*=\s*(?i:false))`)
)

// sqlConfig reads {{ config(...) }} blocks of a SQL model, or dbt.config(...)
// calls of a Python model.
func sqlConfig(src []byte, python bool) modelConfig {
	var c modelConfig
	var blocks []struct {
		text string
		line int
	}
	if python {
		for i, l := range strings.Split(string(src), "\n") {
			if strings.Contains(l, "dbt.config(") {
				blocks = append(blocks, struct {
					text string
					line int
				}{l, i + 1})
			}
		}
	} else {
		for _, t := range scan.SQLTokens(src) {
			if t.Kind == scan.TokJinja && strings.HasPrefix(t.Text, "{{") && reConfigCall.MatchString(t.Text) {
				blocks = append(blocks, struct {
					text string
					line int
				}{t.Text, t.Line})
			}
		}
	}
	for _, b := range blocks {
		if m := reMaterial.FindStringSubmatch(b.text); m != nil {
			c.Materialized = strings.ToLower(m[1])
			c.Line = b.line
		}
		// any value counts (a literal, a list, var('k'), a {% set %} name);
		// only an explicit none is no key
		if m := reUniqueKey.FindStringSubmatch(b.text); m != nil && !nullValue(m[1]) {
			c.UniqueKey = true
		}
		if m := rePartitionBy.FindStringSubmatch(b.text); m != nil && !nullValue(m[1]) {
			c.PartitionBy = true
		}
		if m := reStrategy.FindStringSubmatch(b.text); m != nil {
			c.Strategy = strings.ToLower(m[1])
		}
		if reContractOn.MatchString(b.text) {
			t := true
			c.Enforced = &t
		} else if reContractOff.MatchString(b.text) {
			f := false
			c.Enforced = &f
		}
	}
	return c
}

// modelName is a model file's name: its base name without the extension.
func modelName(repoPath string) string {
	b := path.Base(repoPath)
	return strings.TrimSuffix(b, path.Ext(b))
}

// effectiveConfig is the model's config: dbt_project.yml folder config,
// then the YAML config, then the file's own config() (dbt's precedence).
func effectiveConfig(p *dbtProject, rel string, m *dbtModel, src []byte, python bool) modelConfig {
	c := p.folderConfig(rel)
	if m != nil {
		c = c.over(m.Config)
	}
	return c.over(sqlConfig(src, python))
}
