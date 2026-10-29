package data

import (
	"context"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// data/pipeline-pii-output (spec C §3.4): a personal-data column from a
// classified input reaches an output that is not classified with that class,
// unmasked. A heuristic: the pipeline-reviewer (D5) judges the rest. Findings
// name columns and tables only, never values.
const rulePIIOutput = "pii-to-output"

// personalClasses are the manifest classes this check follows.
var personalClasses = []string{"pii", "sensitive", "financial", "children"}

// commonPersonal are column names treated as personal when the file reads a
// classified table.
var commonPersonal = []string{"email", "cpf", "phone", "birth_date", "address", "name"}

// defaultMasking are the masking functions recognised without config;
// packs.data.pipelines.masking adds project macros.
var defaultMasking = []string{"sha2", "sha256", "sha1", "md5", "hash", "farm_fingerprint", "xxhash64", "mask", "hashbytes"}

const maxContracts = 200

var (
	pyReadRe   = regexp.MustCompile(`\.(?:table|read_table|read_sql_table|read_delta|load)\(\s*["']([\w.\-/]+)["']`)
	pyWriteRe  = regexp.MustCompile(`\.(?:saveAsTable|insertInto|to_sql|to_csv|to_parquet|to_json|to_excel|write_csv|write_parquet|write_delta|write_database|save|csv|parquet|json)\(\s*(?:\w+\s*=\s*)?["']([^"']+)["']`)
	sqlWriteRe = regexp.MustCompile("(?i)\\b(?:insert\\s+(?:into|overwrite(?:\\s+table)?)|create\\s+(?:or\\s+replace\\s+)?(?:temp(?:orary)?\\s+)?(?:table|view)(?:\\s+if\\s+not\\s+exists)?|merge\\s+into)\\s+([\\w.`\"\\[\\]]+)")
	jinjaRelRe = regexp.MustCompile(`(ref|source)\(\s*['"]([\w.\-]+)['"](?:\s*,\s*['"]([\w.\-]+)['"])?`)
	selectRe   = regexp.MustCompile(`(?i)\bselect\b`)
)

type piiCtx struct {
	tables  map[string][]string // lower-cased table -> classes (manifest data.paths)
	data    manifest.Data
	columns map[string][]string // contract-classified column -> classes
	masking map[string]bool
}

func runPipelinePII(_ context.Context, in *packs.Input) packs.Result {
	sel := loadUnits(in)
	pc := piiCtx{tables: map[string][]string{}, columns: contractColumns(in), masking: map[string]bool{}}
	if in.Manifest != nil {
		pc.data = in.Manifest.Data
		pc.tables = in.Manifest.Data.Tables()
	}
	for _, m := range append(append([]string{}, defaultMasking...), in.Settings().Strings("pipelines.masking")...) {
		pc.masking[strings.ToLower(m)] = true
	}
	var fs findings
	n := 0
	for _, u := range sel.units {
		if u.kind != "python" && u.kind != "sql" {
			continue
		}
		n++
		pc.check(u, &fs)
	}
	return result(n, "pipeline file", sel.skipped, &fs)
}

// occurrence is one place a column is used.
type occurrence struct {
	line   int // src line
	masked bool
}

func (pc piiCtx) check(u *unit, fs *findings) {
	sqlTexts := u.sqlTexts()
	// Inputs and their classes.
	inClasses := map[string]bool{}
	for _, name := range u.inputs(sqlTexts) {
		for _, c := range pc.classesOf(name) {
			if containsString(personalClasses, c) {
				inClasses[c] = true
			}
		}
	}
	// Columns to follow, with the classes they carry.
	cols := map[string]map[string]bool{}
	if len(inClasses) > 0 {
		for _, c := range commonPersonal {
			cols[c] = inClasses
		}
		for c := range pc.columns {
			cols[c] = inClasses
		}
	}
	for c, classes := range pc.columns {
		if cols[c] == nil {
			cols[c] = map[string]bool{}
		}
		merged := map[string]bool{}
		for k := range cols[c] {
			merged[k] = true
		}
		for _, k := range classes {
			merged[k] = true
		}
		cols[c] = merged
	}
	if len(cols) == 0 {
		return
	}
	outs := u.outputs(sqlTexts)
	if len(outs) == 0 {
		return
	}
	names := make([]string, 0, len(cols))
	for c := range cols {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, col := range names {
		occ := pc.occurrences(u, sqlTexts, col)
		first := 0
		for _, o := range occ {
			if !o.masked {
				first = o.line
				break
			}
		}
		if first == 0 {
			continue
		}
		classes := sortedKeys(cols[col])
		for _, out := range outs {
			oc := pc.outputClasses(u, out)
			var missing []string
			for _, k := range classes {
				if !containsString(oc, k) {
					missing = append(missing, k)
				}
			}
			if len(missing) == 0 {
				continue
			}
			fs.add(u, first, packs.LevelError, rulePIIOutput,
				"column "+col+" ("+strings.Join(missing, ",")+") from a classified input reaches output "+out+" unmasked; mask it (sha2, md5, hash, farm_fingerprint or a pipelines.masking function) or classify the output in the manifest data.paths")
			break
		}
	}
}

// sqlText is one piece of SQL: a .sql file, or a Python string literal that
// reads like SQL, with its src line.
type sqlText struct {
	line int
	text string
}

func (u *unit) sqlTexts() []sqlText {
	if u.kind == "sql" {
		return []sqlText{{line: 1, text: string(u.src)}}
	}
	var out []sqlText
	for _, l := range scan.PythonLines(u.src) {
		for _, s := range l.Strings {
			if selectRe.MatchString(s) || sqlWriteRe.MatchString(s) {
				out = append(out, sqlText{line: l.Line, text: s})
			}
		}
	}
	return out
}

func (u *unit) inputs(texts []sqlText) []string {
	var out []string
	for _, t := range texts {
		for _, st := range scan.SQLStatements([]byte(t.text)) {
			for _, r := range st.Relations() {
				if !r.Jinja {
					out = append(out, strings.ToLower(r.Name))
				}
			}
		}
		for _, m := range jinjaRelRe.FindAllStringSubmatch(t.text, -1) {
			out = append(out, jinjaNames(m)...)
		}
	}
	if u.kind == "python" {
		for _, m := range pyReadRe.FindAllStringSubmatch(string(u.src), -1) {
			out = append(out, strings.ToLower(m[1]))
		}
	}
	return out
}

func jinjaNames(m []string) []string {
	if m[1] == "source" && m[3] != "" {
		return []string{strings.ToLower(m[2] + "." + m[3]), strings.ToLower(m[3])}
	}
	return []string{strings.ToLower(m[2])}
}

// outputs lists the write targets of u. A dbt model (.sql with Jinja and no
// write statement) writes to the model named after the file.
func (u *unit) outputs(texts []sqlText) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.Trim(s, "`\"[]")
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, t := range texts {
		for _, m := range sqlWriteRe.FindAllStringSubmatch(t.text, -1) {
			add(strings.NewReplacer("`", "", `"`, "", "[", "", "]", "").Replace(m[1]))
		}
	}
	if u.kind == "python" {
		for _, l := range u.codeLines() {
			for _, m := range pyWriteRe.FindAllStringSubmatch(l, -1) {
				add(m[1])
			}
		}
	}
	if u.kind == "sql" && len(out) == 0 && strings.Contains(string(u.src), "{{") {
		add(strings.TrimSuffix(path.Base(u.path), path.Ext(u.path)))
	}
	return out
}

func (pc piiCtx) classesOf(name string) []string {
	name = strings.ToLower(strings.Trim(name, "`\"[]"))
	cs := append([]string{}, pc.tables[name]...)
	if i := strings.LastIndex(name, "."); i >= 0 {
		cs = append(cs, pc.tables[name[i+1:]]...)
	}
	return cs
}

func (pc piiCtx) outputClasses(u *unit, out string) []string {
	cs := pc.classesOf(out)
	cs = append(cs, pc.data.Classified(out)...)
	if u.kind == "sql" && strings.TrimSuffix(path.Base(u.path), path.Ext(u.path)) == out {
		cs = append(cs, pc.data.Classified(u.path)...)
	}
	return cs
}

// occurrences finds the uses of col: quoted names in Python code
// (col("email"), df["email"]), words in SQL text.
func (pc piiCtx) occurrences(u *unit, texts []sqlText, col string) []occurrence {
	var out []occurrence
	wordRe := regexp.MustCompile(`(?i)(^|[^\w.])` + regexp.QuoteMeta(col) + `\b`)
	if u.kind == "python" {
		quoted := regexp.MustCompile(`["']` + regexp.QuoteMeta(col) + `["']`)
		for i, l := range u.codeLines() {
			for _, m := range quoted.FindAllStringIndex(l, -1) {
				out = append(out, occurrence{line: i + 1, masked: pc.maskedAt(l, m[0])})
			}
		}
		for _, t := range texts {
			for _, m := range wordRe.FindAllStringSubmatchIndex(t.text, -1) {
				at := m[3]
				if aliasTarget(t.text, at) {
					continue
				}
				out = append(out, occurrence{line: t.line, masked: pc.maskedAt(t.text, at)})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].line < out[j].line })
		return out
	}
	for i, l := range u.codeLines() {
		if j := strings.Index(l, "--"); j >= 0 {
			l = l[:j]
		}
		for _, m := range wordRe.FindAllStringSubmatchIndex(l, -1) {
			at := m[3]
			if aliasTarget(l, at) {
				continue
			}
			out = append(out, occurrence{line: i + 1, masked: pc.maskedAt(l, at)})
		}
	}
	return out
}

var aliasRe = regexp.MustCompile(`(?i)\bas\s*$`)

// aliasTarget reports whether the word at i is the target of `AS`.
func aliasTarget(s string, i int) bool { return aliasRe.MatchString(s[:i]) }

// maskedAt reports whether position i of s sits inside a call to a masking
// function (at any depth of the enclosing parentheses).
func (pc piiCtx) maskedAt(s string, i int) bool {
	depth := 0
	for k := i - 1; k >= 0; k-- {
		switch s[k] {
		case ')':
			depth++
		case '(':
			if depth > 0 {
				depth--
				continue
			}
			j := k
			for j > 0 && (isWordByte(s[j-1]) || s[j-1] == '.') {
				j--
			}
			if pc.maskingCallee(strings.ToLower(s[j:k])) {
				return true
			}
		}
	}
	return false
}

// maskingCallee reports whether a dotted callee (`udfs.acme.mask_email`)
// names a masking function: the whole name or any dotted suffix of it
// (`acme.mask_email`, `mask_email`), so `F.sha2` and a configured
// `acme.mask_email` both match.
func (pc piiCtx) maskingCallee(name string) bool {
	for {
		if pc.masking[name] {
			return true
		}
		d := strings.IndexByte(name, '.')
		if d < 0 {
			return false
		}
		name = name[d+1:]
	}
}

// contractColumns reads the ODCS contracts (packs.data.contracts.paths) and
// returns the classified property names with their tq classes. Bounded to
// maxContracts files of 1 MiB.
func contractColumns(in *packs.Input) map[string][]string {
	out := map[string][]string{}
	if in.Git == nil {
		return out
	}
	globs := in.Settings().Strings("contracts.paths")
	if len(globs) == 0 {
		globs = defaultContractPaths
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		return out
	}
	n := 0
	for _, f := range files {
		if !matchAny(globs, f) {
			continue
		}
		if n++; n > maxContracts {
			break
		}
		b, err := in.ReadNew(f, 1<<20)
		if err != nil {
			continue
		}
		root, err := scan.ParseYAML(b)
		if err != nil {
			continue
		}
		scan.Walk(root, func(_ []string, nd *yaml.Node) bool {
			name := scan.Scalar(scan.Get(nd, "name"))
			cls := strings.ToLower(scan.Scalar(scan.Get(nd, "classification")))
			if name != "" && containsString(personalClasses, cls) {
				k := strings.ToLower(name)
				if !containsString(out[k], cls) {
					out[k] = append(out[k], cls)
				}
			}
			return true
		})
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
