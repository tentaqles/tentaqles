package data

import (
	"context"
	"regexp"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// data/pipeline-idempotency (spec C §3.4): append writes with no dedupe in
// the same file.
const ruleAppend = "append-no-dedupe"

const appendMsg = "append write with no MERGE, delete-insert, partition overwrite or dedupe in this file; a rerun of the same interval duplicates rows"

var (
	// Python code patterns (raw lines, comments blanked).
	pyAppendRe       = regexp.MustCompile(`(?i)\.mode\(\s*["']append["']|\b(?:if_exists|if_table_exists|mode)\s*=\s*["']append["']|\.insertInto\(`)
	pyOverwriteArgRe = regexp.MustCompile(`(?i)overwrite\s*=\s*True`)
	insertSelectRe   = regexp.MustCompile(`(?is)\binsert\s+into\b.*\bselect\b`)
	// Anything that makes the write idempotent, anywhere in the file.
	dedupeRe = regexp.MustCompile(`(?i)\bmerge\s+into\b|\.merge\(|whenMatched|replaceWhere|partitionOverwriteMode|\.mode\(\s*["']overwrite|\b(?:mode|if_exists|if_table_exists)\s*=\s*["'](?:overwrite|replace)|\binsert\s+overwrite\b|\bdelete\s+from\b|\bon\s+conflict\b|dropDuplicates|drop_duplicates|\btruncate\b|\bupsert\b|\bqualify\s+row_number\b`)
)

func runPipelineIdempotency(_ context.Context, in *packs.Input) packs.Result {
	sel := loadUnits(in)
	var fs findings
	n := 0
	for _, u := range sel.units {
		if u.kind != "python" && u.kind != "sql" {
			continue
		}
		n++
		if dedupeRe.Match(u.src) {
			continue
		}
		if u.kind == "sql" {
			for _, st := range scan.SQLStatements(u.src) {
				if st.Keyword() == "INSERT" && hasSelect(st) && !hasWord(st, "OVERWRITE") {
					fs.add(u, st.Line, packs.LevelWarn, ruleAppend, appendMsg)
				}
			}
			continue
		}
		for i, l := range u.codeLines() {
			if pyAppendRe.MatchString(l) && !pyOverwriteArgRe.MatchString(l) {
				fs.add(u, i+1, packs.LevelWarn, ruleAppend, appendMsg)
			}
		}
		for _, l := range scan.PythonLines(u.src) {
			for _, s := range l.Strings {
				if insertSelectRe.MatchString(s) {
					fs.add(u, l.Line, packs.LevelWarn, ruleAppend, appendMsg)
				}
			}
		}
	}
	return result(n, "pipeline file", sel.skipped, &fs)
}

func hasSelect(st scan.Statement) bool { return hasWord(st, "SELECT") }

func hasWord(st scan.Statement, w string) bool {
	for _, t := range st.Tokens {
		if t.IsWord(w) {
			return true
		}
	}
	return false
}
