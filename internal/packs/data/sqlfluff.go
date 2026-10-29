package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// sqlfluffBatch bounds the files per sqlfluff call (Windows argv is 32K chars).
const sqlfluffBatch = 100

// dialects maps a dbt adapter type (or a packs.data.engines entry) to the
// sqlfluff dialect.
var dialects = map[string]string{
	"bigquery": "bigquery", "snowflake": "snowflake", "databricks": "databricks", "spark": "sparksql",
	"postgres": "postgres", "postgresql": "postgres", "redshift": "redshift", "duckdb": "duckdb",
	"fabric": "tsql", "synapse": "tsql", "sqlserver": "tsql", "trino": "trino", "athena": "athena",
	"mysql": "mysql", "oracle": "oracle", "clickhouse": "clickhouse", "teradata": "teradata",
}

func sqlfluffApplies(in *packs.Input) bool { return in.AnyChanged("*.sql") }

// lintablePaths are the changed .sql files, minus build output and packages.
func lintablePaths(in *packs.Input) []string {
	var out []string
outer:
	for _, p := range in.ChangedPaths() {
		if !strings.EqualFold(filepath.Ext(p), ".sql") {
			continue
		}
		for _, seg := range strings.Split(p, "/") {
			switch seg {
			case "target", "dbt_packages", "logs", ".venv", "node_modules":
				continue outer
			}
		}
		out = append(out, p)
	}
	return out
}

// sqlfluffDialect returns the --dialect to pass ("" when a .sqlfluff sets
// it), or an error when nothing names one.
func sqlfluffDialect(in *packs.Input, projects []*dbtProject) (string, error) {
	dirs := []string{""}
	for _, p := range projects {
		if p.Dir != "" {
			dirs = append(dirs, p.Dir)
		}
	}
	for _, d := range dirs {
		if b, err := in.ReadNew(joinRel(d, ".sqlfluff"), maxFileBytes); err == nil && reDialect.Match(b) {
			return "", nil
		}
	}
	for _, p := range projects {
		if t := adapterType(in, p); t != "" {
			if d, ok := dialects[t]; ok {
				return d, nil
			}
		}
	}
	for _, e := range in.Settings().Strings("engines") {
		if d, ok := dialects[strings.ToLower(e)]; ok {
			return d, nil
		}
	}
	return "", fmt.Errorf("no SQL dialect for sqlfluff: add a .sqlfluff with [sqlfluff] dialect = <dialect> (tq runs sqlfluff with the jinja templater)")
}

// adapterType reads only the adapter type of the project's default target
// from a profiles.yml inside the repository (spec C §8: never another value,
// never a profiles.yml outside the repo).
func adapterType(in *packs.Input, p *dbtProject) string {
	raw, err := in.ReadNew(joinRel(p.Dir, "profiles.yml"), maxFileBytes)
	if err != nil {
		return ""
	}
	root, err := scan.ParseYAML(raw)
	if err != nil {
		return ""
	}
	prof := scan.Get(root, p.Profile)
	if prof == nil {
		for _, kv := range scan.Entries(root) {
			if kv.Key != "config" {
				prof = kv.Value
				break
			}
		}
	}
	target := scan.Scalar(scan.Get(prof, "target"))
	out := scan.Get(prof, "outputs", target)
	if out == nil {
		for _, kv := range scan.Entries(scan.Get(prof, "outputs")) {
			out = kv.Value
			break
		}
	}
	return strings.ToLower(scan.Scalar(scan.Get(out, "type")))
}

type sqlfluffFile struct {
	Filepath   string `json:"filepath"`
	Violations []struct {
		StartLine   int    `json:"start_line_no"`
		LineNo      int    `json:"line_no"`
		Code        string `json:"code"`
		Description string `json:"description"`
		Warning     bool   `json:"warning"`
	} `json:"violations"`
}

// reQuoted blanks quoted text in a sqlfluff message: it may echo a literal
// from the SQL, and findings never carry values.
var reDialect = regexp.MustCompile(`(?m)^\s*dialect\s*=\s*\S`)

var reQuoted = regexp.MustCompile("'[^']*'|\"[^\"]*\"|`[^`]*`")

func parseSqlfluff(stdout []byte, root string) ([]packs.Finding, error) {
	b := bytes.TrimSpace(stdout)
	if i := bytes.IndexByte(b, '['); i > 0 {
		b = b[i:]
	}
	var files []sqlfluffFile
	if err := json.Unmarshal(b, &files); err != nil {
		return nil, err
	}
	var out []packs.Finding
	for _, f := range files {
		p := filepath.ToSlash(f.Filepath)
		if filepath.IsAbs(f.Filepath) {
			if rel, err := filepath.Rel(root, f.Filepath); err == nil {
				p = filepath.ToSlash(rel)
			}
		}
		p = strings.TrimPrefix(p, "./")
		for _, v := range f.Violations {
			line := v.StartLine
			if line == 0 {
				line = v.LineNo
			}
			lvl := packs.LevelError
			if v.Warning {
				lvl = packs.LevelWarn
			}
			code := v.Code
			if code == "" {
				code = "sqlfluff"
			}
			out = append(out, packs.Finding{File: p, Line: line, Level: lvl, Rule: code,
				Message: reQuoted.ReplaceAllString(strings.TrimSpace(v.Description), "'…'")})
		}
	}
	return out, nil
}

func runSqlfluff(ctx context.Context, in *packs.Input) packs.Result {
	files := lintablePaths(in)
	if len(files) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no SQL files to lint"}
	}
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	dialect, err := sqlfluffDialect(in, r.projects)
	if err != nil {
		return failResult(err)
	}
	var fs []packs.Finding
	var tr *packs.ToolRun
	for start := 0; start < len(files); start += sqlfluffBatch {
		end := min(start+sqlfluffBatch, len(files))
		args := []string{"lint", "--format", "json", "--templater", "jinja"}
		if dialect != "" {
			args = append(args, "--dialect", dialect)
		}
		for _, f := range files[start:end] {
			args = append(args, filepath.FromSlash(f))
		}
		out, fail := runToolOnce(ctx, in, "sqlfluff", args, in.Root, sqlfluffTimeout)
		if fail != nil {
			return *fail
		}
		tr = toolResult(out)
		tr.Name = "sqlfluff"
		got, perr := parseSqlfluff(out.Stdout, in.Root)
		if perr != nil || (out.ExitCode > 1 && len(got) == 0) {
			return packs.Result{Status: packs.Fail, Tool: tr,
				Detail: fmt.Sprintf("sqlfluff lint failed (exit %d):\n%s", out.ExitCode, out.Tail(20, nil))}
		}
		fs = append(fs, got...)
	}
	return packs.Result{Status: packs.OK, Tool: tr, Detail: plural(len(files), "SQL file") + " linted", Findings: fs}
}
