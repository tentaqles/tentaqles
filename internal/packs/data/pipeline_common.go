package data

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// Shared plumbing of the pipeline checks (spec C §3.4): which changed files
// are pipeline files, how each one is read as Python or SQL source with a
// line map, and inline suppressions.

// defaultPipelinePaths is packs.data.pipelines.paths when it is not set.
var defaultPipelinePaths = []string{"dags/**", "pipelines/**", "**/*.Notebook/**", "notebook/**", "pipeline/**"}

// fabricGlobs and synapseGlobs are the git-integration layouts (spec C §3.1).
var (
	fabricGlobs  = []string{"**/*.Notebook/**", "**/*.DataPipeline/**"}
	synapseGlobs = []string{"**/notebook/*.json", "**/pipeline/*.json", "**/linkedService/*.json", "**/sqlscript/*.json", "**/dataflow/*.json", "**/trigger/*.json", "**/sparkJobDefinition/*.json"}
)

// maxPipelineFile is the largest file a pipeline check reads (larger files
// are listed as skipped in the detail).
const maxPipelineFile = 4 << 20

// orchestratorRe finds an import of an orchestrator in Python source.
var orchestratorRe = regexp.MustCompile(`(?m)^[ \t]*(?:from|import)[ \t]+(airflow|dagster|prefect)\b`)

func pipelineGlobs(in *packs.Input) []string {
	if p := in.Settings().Strings("pipelines.paths"); len(p) > 0 {
		return p
	}
	return defaultPipelinePaths
}

func contractPaths(in *packs.Input) []string { return in.Settings().Strings("dbt.contract_paths") }

func matchAny(globs []string, p string) bool {
	for _, g := range globs {
		if packs.MatchGlob(g, p) {
			return true
		}
	}
	return false
}

func ext(p string) string { return strings.ToLower(path.Ext(p)) }

// pipelineApplies is the cheap Applies of the source checks: a changed path
// under pipelines.paths, or a changed .py/.ipynb that may import an
// orchestrator (read in Run).
func pipelineApplies(in *packs.Input) bool {
	return in.AnyChanged(pipelineGlobs(in)...) || in.AnyChanged("*.py", "*.ipynb")
}

// unit is one changed file read as source.
type unit struct {
	path string
	kind string // python | sql | json | other
	raw  []byte
	// src is the source the rules scan: the file itself, or the joined code
	// cells of a notebook. fileLine maps a src line to the file line.
	src      []byte
	fileLine func(int) int
	notebook string // scan.Format* or ""
	cells    []scan.Cell
	lines    []string // src split in lines (index 0 = line 1)
}

func (u *unit) line(n int) int {
	if u.fileLine == nil {
		return n
	}
	if l := u.fileLine(n); l > 0 {
		return l
	}
	return 1
}

// selection is what loadUnits read.
type selection struct {
	units   []*unit
	skipped int // too large or unreadable
}

// loadUnits reads the changed pipeline files: every changed path under
// pipelines.paths, plus changed .py/.ipynb files outside it that import
// airflow, dagster or prefect. Deleted files are left out.
func loadUnits(in *packs.Input) selection {
	globs := pipelineGlobs(in)
	var sel selection
	for _, p := range in.ChangedPaths() {
		inPaths := matchAny(globs, p)
		e := ext(p)
		if !inPaths && e != ".py" && e != ".ipynb" {
			continue
		}
		b, err := in.ReadNew(p, maxPipelineFile+1)
		if err != nil || len(b) > maxPipelineFile {
			sel.skipped++
			continue
		}
		u := newUnit(p, b)
		if !inPaths && !orchestratorRe.Match(u.src) {
			continue
		}
		sel.units = append(sel.units, u)
	}
	return sel
}

func newUnit(p string, b []byte) *unit {
	u := &unit{path: p, raw: b, src: b, kind: "other"}
	switch e := ext(p); {
	case e == ".sql":
		u.kind = "sql"
	case e == ".ipynb" || path.Base(p) == "notebook-content.py" || (e == ".json" && scan.DetectNotebook(p, b) != ""):
		u.notebook = scan.DetectNotebook(p, b)
		cells, err := scan.NotebookCells(p, b)
		if err == nil || len(cells) > 0 {
			u.cells = cells
			u.src, u.fileLine = scan.CodeSource(cells)
			u.kind = "python"
		} else if e == ".json" {
			u.kind = "json"
		}
	case e == ".py":
		u.kind = "python"
	case e == ".json":
		u.kind = "json"
	}
	u.lines = strings.Split(strings.ReplaceAll(string(u.src), "\r\n", "\n"), "\n")
	return u
}

// suppressed reports whether src line n (or the line above it) carries
// `tq:allow <rule>: <reason>` with a non-empty reason.
func (u *unit) suppressed(n int, rule string) bool {
	for _, k := range []int{n, n - 1} {
		if k >= 1 && k <= len(u.lines) && allowMarker(u.lines[k-1], rule) {
			return true
		}
	}
	return false
}

func allowMarker(line, rule string) bool {
	i := strings.Index(line, "tq:allow "+rule+":")
	if i < 0 {
		return false
	}
	return strings.TrimSpace(line[i+len("tq:allow "+rule+":"):]) != ""
}

// findings collects findings and drops suppressed ones.
type findings struct {
	list []packs.Finding
	seen map[string]bool
}

// add records a finding at src line n of u (mapped to the file line).
func (f *findings) add(u *unit, n int, level, rule, msg string) {
	if u.suppressed(n, rule) {
		return
	}
	f.addFile(u.path, u.line(n), level, rule, msg)
}

// addFile records a finding at a file line (JSON files, no suppression).
func (f *findings) addFile(file string, line int, level, rule, msg string) {
	key := fmt.Sprintf("%s:%d:%s:%s", file, line, rule, msg)
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.seen[key] {
		return
	}
	f.seen[key] = true
	f.list = append(f.list, packs.Finding{File: file, Line: line, Level: level, Rule: rule, Message: msg})
}

func (f *findings) sorted() []packs.Finding {
	sort.SliceStable(f.list, func(i, j int) bool {
		a, b := f.list[i], f.list[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	if f.list == nil {
		return []packs.Finding{}
	}
	return f.list
}

// result builds the check result with a detail naming what was scanned.
func result(n int, what string, skipped int, fs *findings) packs.Result {
	d := plural(n, what) + " scanned"
	if skipped > 0 {
		d += fmt.Sprintf(", %d unreadable or over 4 MiB skipped", skipped)
	}
	return packs.Result{Status: packs.OK, Detail: d, Findings: fs.sorted()}
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// codeLines returns src lines with Python comments and full-line SQL
// comments blanked, so code patterns don't match commented-out code. Line
// numbers are kept.
func (u *unit) codeLines() []string {
	out := make([]string, len(u.lines))
	for i, l := range u.lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "--") {
			continue
		}
		out[i] = l
	}
	return out
}

// lineOf is the 1-based line of byte offset off in b.
func lineOf(b []byte, off int) int {
	if off > len(b) {
		off = len(b)
	}
	return 1 + bytes.Count(b[:off], []byte("\n"))
}
