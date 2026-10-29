package data

import (
	"context"
	"regexp"
	"strings"

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
	// Python code that makes a write idempotent, matched on code with its
	// string literals restored and its comments and docstrings left out.
	// A bare .merge( is a pandas join, not a MERGE: a Delta merge counts
	// through its whenMatched*/whenNotMatched* clauses.
	pyDedupeRe  = regexp.MustCompile(`(?i)whenMatched|whenNotMatched|replaceWhere|partitionOverwriteMode|\.mode\(\s*["']overwrite|\b(?:mode|if_exists|if_table_exists)\s*=\s*["'](?:overwrite|replace|merge)|dropDuplicates|drop_duplicates|\bupsert\b`)
	docstringRe = regexp.MustCompile(`^[rRbBuUfF]{0,2}(?:""|'')$`)
)

// hasDedupe reports evidence, anywhere in the file, that its writes are
// idempotent. Comments, docstrings and SQL string values never count.
func hasDedupe(u *unit) bool {
	if u.kind == "sql" {
		return sqlDedupe(scan.SQLStatements(u.src))
	}
	for _, l := range scan.PythonLines(u.src) {
		if docstringRe.MatchString(l.Code) {
			continue
		}
		if pyDedupeRe.MatchString(withStrings(l)) {
			return true
		}
		for _, s := range l.Strings {
			if sqlDedupe(scan.SQLStatements([]byte(s))) {
				return true
			}
		}
	}
	return false
}

// sqlDedupe reports an idempotent SQL write: MERGE, DELETE, TRUNCATE,
// INSERT OVERWRITE, INSERT ... ON CONFLICT, or a QUALIFY ROW_NUMBER dedupe.
// Only word tokens count, so comments and string values are ignored.
func sqlDedupe(stmts []scan.Statement) bool {
	for _, st := range stmts {
		switch st.Keyword() {
		case "MERGE", "DELETE", "TRUNCATE":
			return true
		}
		var prev string
		for _, t := range st.Tokens {
			if t.Kind != scan.TokWord {
				continue
			}
			up := t.Upper()
			if (prev == "INSERT" && up == "OVERWRITE") || (prev == "ON" && up == "CONFLICT") || (prev == "QUALIFY" && up == "ROW_NUMBER") || up == "UPSERT" {
				return true
			}
			prev = up
		}
	}
	return false
}

// withStrings is a logical line's code with each emptied string literal
// refilled with its contents, in order.
func withStrings(l scan.PyLine) string {
	if len(l.Strings) == 0 {
		return l.Code
	}
	var b strings.Builder
	k := 0
	c := l.Code
	for i := 0; i < len(c); i++ {
		b.WriteByte(c[i])
		if (c[i] == '"' || c[i] == '\'') && i+1 < len(c) && c[i+1] == c[i] && k < len(l.Strings) {
			b.WriteString(l.Strings[k])
			b.WriteByte(c[i])
			k++
			i++
		}
	}
	return b.String()
}

func runPipelineIdempotency(_ context.Context, in *packs.Input) packs.Result {
	sel := loadUnits(in)
	var fs findings
	n := 0
	for _, u := range sel.units {
		if u.kind != "python" && u.kind != "sql" {
			continue
		}
		n++
		if hasDedupe(u) {
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
			if docstringRe.MatchString(l.Code) {
				continue
			}
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
