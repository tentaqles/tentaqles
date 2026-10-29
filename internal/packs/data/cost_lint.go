package data

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// data/bq-job-max-bytes (built-in): BigQuery jobs written into scripts,
// pipelines, notebooks and CI files must carry a byte cap. The guard
// tq/bq-unbounded-query covers what an agent types; this check covers what a
// change commits.
//
// Rules (all error unless noted):
//   bq-cli-no-cap         `bq query` without --maximum_bytes_billed (not --dry_run)
//   bq-cap-above-max      a literal cap above packs.data.cost.bigquery.max_bytes
//   bq-job-config-no-cap  QueryJobConfig(...) without maximum_bytes_billed, never set later
//   bq-query-no-cap       <client>.query(sql) or .query_and_wait(sql) with no job_config,
//                         on a client without default_query_job_config (also chained
//                         bigquery.Client().query(sql))
//   bq-read-gbq-no-cap    read_gbq(...) without configuration maximumBytesBilled
//   bq-operator-no-cap    BigQueryInsertJobOperator(...) without maximumBytesBilled
//   bq-magic-no-cap       a %%bigquery cell without --maximum_bytes_billed, in a
//                         notebook that sets no context default
//   bq-config-unchecked   (warn) a configuration passed by name the lint cannot see
//
// Suppress one finding with a comment on the line or the line above:
// `# tq:allow <rule>: <reason>` (Python, shell, YAML) or `-- tq:allow …`.

const checkBQJobMaxBytes = "data/bq-job-max-bytes"

// lintMaxFiles and lintMaxBytes bound the check on a large change.
const (
	lintMaxFiles = 2000
	lintMaxBytes = 2 << 20
)

var (
	costShellExts = map[string]bool{".sh": true, ".bash": true, ".zsh": true, ".ps1": true, ".psm1": true, ".cmd": true, ".bat": true,
		".yml": true, ".yaml": true, ".mk": true, ".txt": false}
	costAllowRe = regexp.MustCompile(`tq:allow\s+([a-z0-9-]+)\s*:\s*\S`)
)

func costLintKind(p string) string {
	base := strings.ToLower(path.Base(p))
	ext := path.Ext(base)
	switch {
	case ext == ".ipynb", base == "notebook-content.py":
		return "notebook"
	case ext == ".py":
		return "python"
	case costShellExts[ext], base == "makefile", base == "dockerfile", base == "justfile":
		return "shell"
	case ext == ".json" && (strings.Contains(p, "notebook/") || strings.Contains(p, "Notebook")):
		return "notebook"
	}
	return ""
}

func bqLintApplies(in *packs.Input) bool {
	if !costOf(in.Settings()).Engine(engBigQuery) {
		return false
	}
	for _, p := range in.ChangedPaths() {
		if costLintKind(p) != "" {
			return true
		}
	}
	return false
}

type bqLint struct {
	file     string
	limit    float64
	hasCap   bool
	capRaw   string
	findings []packs.Finding
}

func (l *bqLint) add(line int, level, rule, msg string, allowed func(int, string) bool) {
	if allowed != nil && allowed(line, rule) {
		return
	}
	l.findings = append(l.findings, packs.Finding{File: l.file, Line: line, Level: level, Rule: rule, Message: msg})
}

func (l *bqLint) suggest() string {
	if l.hasCap {
		return fmt.Sprintf("%.0f", l.limit)
	}
	return "<bytes>"
}

// capValue checks one literal cap (an integer, 10**9, 1_000_000 or a quoted number).
func (l *bqLint) capValue(line int, raw string, allowed func(int, string) bool) {
	v, ok := costLiteralInt(raw)
	if !ok || !l.hasCap || v <= l.limit {
		return
	}
	l.add(line, packs.LevelError, "bq-cap-above-max", fmt.Sprintf("maximum bytes billed %.0f is above packs.data.cost.bigquery.max_bytes (%s)", v, l.capRaw), allowed)
}

var costPowRe = regexp.MustCompile(`^([0-9]+)\s*\*\*\s*([0-9]+)$`)

func costLiteralInt(s string) (float64, bool) {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.ReplaceAll(s, "_", "")
	if m := costPowRe.FindStringSubmatch(s); m != nil {
		b, _ := strconv.ParseFloat(m[1], 64)
		e, _ := strconv.Atoi(m[2])
		if e > 30 {
			return 0, false
		}
		v := 1.0
		for i := 0; i < e; i++ {
			v *= b
		}
		return v, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(n), true
}

// shellLine judges one logical shell line (continuations already joined).
func (l *bqLint) shellLine(line int, text string, allowed func(int, string) bool) {
	if !strings.Contains(text, "bq") && !costMayNest(strings.ToLower(text)) {
		return
	}
	segs, _, _ := costSegmentsDeep(text)
	for _, seg := range segs {
		i := seg.find("bq")
		if i < 0 {
			continue
		}
		args := seg.after(i)
		if !bqQuery(args) || bqDryRun(args) {
			continue
		}
		vals := bqFlag(args, "maximum_bytes_billed")
		if len(vals) == 0 {
			l.add(line, packs.LevelError, "bq-cli-no-cap", "bq query without --maximum_bytes_billed; add --maximum_bytes_billed="+l.suggest(), allowed)
			continue
		}
		for _, v := range vals {
			l.capValue(line, v, allowed)
		}
	}
}

// lintShell scans a shell-like file line by line.
func (l *bqLint) lintShell(src string) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	allowed := func(line int, rule string) bool {
		for _, n := range []int{line - 1, line - 2} {
			if n >= 0 && n < len(lines) {
				for _, m := range costAllowRe.FindAllStringSubmatch(lines[n], -1) {
					if m[1] == rule {
						return true
					}
				}
			}
		}
		return false
	}
	for i := 0; i < len(lines); i++ {
		start := i + 1
		text := lines[i]
		for (strings.HasSuffix(text, `\`) || strings.HasSuffix(text, "`")) && i+1 < len(lines) {
			text = text[:len(text)-1] + " " + lines[i+1]
			i++
		}
		if t := strings.TrimSpace(text); strings.HasPrefix(t, "#") || strings.HasPrefix(t, "::") || strings.HasPrefix(strings.ToLower(t), "rem ") {
			continue
		}
		l.shellLine(start, text, allowed)
	}
}

var (
	clientAssignRe = regexp.MustCompile(`^([A-Za-z_]\w*)\s*=\s*(?:[A-Za-z_][\w.]*\.)?Client\(`)
	configAssignRe = regexp.MustCompile(`^([A-Za-z_]\w*)\s*=\s*(?:[A-Za-z_][\w.]*\.)?QueryJobConfig\(`)
	attrCapRe      = regexp.MustCompile(`^([A-Za-z_]\w*)\.maximum_bytes_billed\s*=\s*(.+)$`)
	magicCtxRe     = regexp.MustCompile(`default_query_job_config\.maximum_bytes_billed\s*=`)
)

// costUsesBigQuery reports whether a Python source imports the BigQuery client,
// pandas-gbq or the Airflow Google provider.
func costUsesBigQuery(src []byte) bool {
	for _, im := range scan.PythonImports(src) {
		m := im.Module
		switch {
		case strings.HasPrefix(m, "google.cloud.bigquery"), m == "pandas_gbq", strings.HasPrefix(m, "airflow.providers.google.cloud"):
			return true
		case m == "google.cloud":
			for _, n := range im.Names {
				if n == "bigquery" {
					return true
				}
			}
		}
	}
	return false
}

func costHasCapString(strs []string) bool {
	for _, s := range strs {
		if s == "maximumBytesBilled" || s == "maximum_bytes_billed" {
			return true
		}
	}
	return false
}

// lintPython scans Python source; fileLine maps source lines to file lines.
func (l *bqLint) lintPython(src []byte, fileLine func(int) int) {
	if fileLine == nil {
		fileLine = func(n int) int { return n }
	}
	lines := scan.PythonLines(src)
	comments := map[int]string{}
	for _, pl := range lines {
		if pl.Comment != "" {
			comments[pl.Line] += " " + pl.Comment
			if pl.EndLine != pl.Line {
				comments[pl.EndLine] += " " + pl.Comment
			}
		}
	}
	allowedAt := func(srcLine int) func(int, string) bool {
		return func(_ int, rule string) bool {
			for _, n := range []int{srcLine, srcLine - 1} {
				for _, m := range costAllowRe.FindAllStringSubmatch(comments[n], -1) {
					if m[1] == rule {
						return true
					}
				}
			}
			return false
		}
	}
	// Commands in string literals (subprocess.run(["bq", "query", ...])).
	for _, pl := range lines {
		if joined := strings.Join(pl.Strings, " "); strings.Contains(joined, "bq") {
			l.shellLine(fileLine(pl.Line), joined, allowedAt(pl.Line))
		}
	}
	if !costUsesBigQuery(src) {
		return
	}
	cappedClients, clients := map[string]bool{}, map[string]bool{}
	setLater := map[string]bool{}
	for _, pl := range lines {
		if m := clientAssignRe.FindStringSubmatch(pl.Code); m != nil {
			clients[m[1]] = true
			for _, c := range scan.LineCalls(pl) {
				if c.Name == "Client" {
					if _, ok := c.Kwargs["default_query_job_config"]; ok {
						cappedClients[m[1]] = true
					}
				}
			}
		}
		if m := attrCapRe.FindStringSubmatch(pl.Code); m != nil {
			setLater[m[1]] = true
			l.capValue(fileLine(pl.Line), m[2], allowedAt(pl.Line))
		}
	}
	// uncappedClientCall marks lines that build a bigquery Client inline
	// without default_query_job_config (bigquery.Client().query(sql)).
	uncappedClientCall := map[int]bool{}
	for _, pl := range lines {
		for _, c := range scan.LineCalls(pl) {
			if c.Name != "Client" || (c.Receiver != "" && c.Receiver != "bigquery" && !strings.HasSuffix(c.Receiver, ".bigquery")) {
				continue
			}
			if _, ok := c.Kwargs["default_query_job_config"]; !ok {
				uncappedClientCall[pl.Line] = true
			}
		}
	}
	for _, pl := range lines {
		al := allowedAt(pl.Line)
		fl := fileLine(pl.Line)
		assigned := ""
		if m := configAssignRe.FindStringSubmatch(pl.Code); m != nil {
			assigned = m[1]
		}
		for _, c := range scan.LineCalls(pl) {
			switch c.Name {
			case "QueryJobConfig":
				if v, ok := c.Kwargs["maximum_bytes_billed"]; ok {
					l.capValue(fl, v, al)
				} else if assigned == "" || !setLater[assigned] {
					l.add(fl, packs.LevelError, "bq-job-config-no-cap", "QueryJobConfig without maximum_bytes_billed; add maximum_bytes_billed="+l.suggest(), al)
				}
			case "query", "query_and_wait":
				recv := c.Receiver
				if c.Func == "."+c.Name {
					// bigquery.Client(...).query(...): a client made on the line
					if !uncappedClientCall[pl.Line] {
						continue
					}
					recv = "bigquery.Client()"
				} else if !clients[c.Receiver] || cappedClients[c.Receiver] {
					continue
				}
				if _, ok := c.Kwargs["job_config"]; ok {
					continue
				}
				// query(sql, job_config) takes it as the second positional;
				// query_and_wait takes job_config by keyword only.
				if c.Name == "query" && len(c.Args) >= 2 {
					continue
				}
				l.add(fl, packs.LevelError, "bq-query-no-cap", recv+"."+c.Name+" without job_config; pass a QueryJobConfig with maximum_bytes_billed="+l.suggest(), al)
			case "read_gbq", "BigQueryInsertJobOperator":
				rule, what := "bq-read-gbq-no-cap", "read_gbq"
				if c.Name == "BigQueryInsertJobOperator" {
					rule, what = "bq-operator-no-cap", "BigQueryInsertJobOperator"
				}
				conf, ok := c.Kwargs["configuration"]
				switch {
				case costHasCapString(c.Strings):
				case !ok:
					l.add(fl, packs.LevelError, rule, what+" without configuration {\"query\": {\"maximumBytesBilled\": \""+l.suggest()+"\"}}", al)
				case !strings.HasPrefix(strings.TrimSpace(conf), "{"):
					l.add(fl, packs.LevelWarn, "bq-config-unchecked", what+" configuration is passed by name; make sure it sets query.maximumBytesBilled", al)
				default:
					l.add(fl, packs.LevelError, rule, what+" configuration without query.maximumBytesBilled", al)
				}
			}
		}
	}
}

var costMagicRe = regexp.MustCompile(`^\s*%%bigquery\b(.*)`)

// lintNotebook scans the cells of a notebook.
func (l *bqLint) lintNotebook(name string, data []byte) error {
	cells, err := scan.NotebookCells(name, data)
	if err != nil {
		return err
	}
	var code []scan.Cell
	ctxCap := false
	for _, c := range cells {
		if c.Kind != "code" && c.Kind != "parameters" {
			continue
		}
		if magicCtxRe.MatchString(c.Source) {
			ctxCap = true
		}
		if !strings.HasPrefix(strings.TrimSpace(c.Source), "%%") {
			code = append(code, c)
		}
	}
	for _, c := range cells {
		if c.Kind != "code" {
			continue
		}
		first := strings.SplitN(strings.TrimSpace(c.Source), "\n", 2)[0]
		m := costMagicRe.FindStringSubmatch(first)
		if m == nil {
			continue
		}
		args := strings.Fields(m[1])
		if bqDryRun(args) {
			continue
		}
		vals := bqFlag(args, "maximum_bytes_billed")
		if len(vals) == 0 {
			if !ctxCap {
				l.add(c.Line, packs.LevelError, "bq-magic-no-cap", "%%bigquery cell without --maximum_bytes_billed; add --maximum_bytes_billed="+l.suggest()+" or set magics.context.default_query_job_config.maximum_bytes_billed", nil)
			}
			continue
		}
		for _, v := range vals {
			l.capValue(c.Line, v, nil)
		}
	}
	src, fileLine := scan.CodeSource(code)
	l.lintPython(src, fileLine)
	return nil
}

func runBQJobMaxBytes(ctx context.Context, in *packs.Input) packs.Result {
	cs := costOf(in.Settings())
	limit, hasCap := cs.Cap(engBigQuery, "max_bytes")
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	files, skipped := 0, 0
	for _, p := range in.ChangedPaths() {
		kind := costLintKind(p)
		if kind == "" {
			continue
		}
		if ctx.Err() != nil {
			return packs.Result{Status: packs.Fail, Detail: "timed out", Findings: res.Findings}
		}
		if files >= lintMaxFiles {
			skipped++
			continue
		}
		data, err := in.ReadNew(p, lintMaxBytes)
		if err != nil {
			// Over the size bound is a documented limit (warn); any other
			// read failure fails the check, since the file went unchecked.
			if fi, serr := os.Stat(filepath.Join(in.Root, filepath.FromSlash(p))); serr == nil && fi.Mode().IsRegular() && fi.Size() > lintMaxBytes {
				res.Findings = append(res.Findings, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "bq-unreadable", Message: fmt.Sprintf("file over %d bytes; not checked", lintMaxBytes)})
			} else {
				res.Findings = append(res.Findings, packs.Finding{File: p, Level: packs.LevelError, Rule: "bq-unreadable", Message: "file could not be read; not checked"})
			}
			continue
		}
		files++
		l := &bqLint{file: p, limit: limit, hasCap: hasCap, capRaw: cs.Raw(engBigQuery, "max_bytes")}
		switch kind {
		case "shell":
			l.lintShell(string(data))
		case "python":
			l.lintPython(data, nil)
		case "notebook":
			if scan.DetectNotebook(p, data) == "" {
				continue
			}
			if err := l.lintNotebook(p, data); err != nil {
				res.Findings = append(res.Findings, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "bq-unreadable", Message: "notebook did not parse; not checked"})
				continue
			}
		}
		res.Findings = append(res.Findings, l.findings...)
	}
	errs, _ := res.Counts()
	if errs > 0 {
		res.Status = packs.Fail
	}
	res.Detail = fmt.Sprintf("%d files checked, %d BigQuery jobs without a usable byte cap", files, errs)
	if skipped > 0 {
		res.Detail += fmt.Sprintf(" (%d files over the %d-file bound not checked)", skipped, lintMaxFiles)
	}
	return res
}
