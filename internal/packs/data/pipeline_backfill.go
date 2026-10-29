package data

import (
	"context"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// data/pipeline-backfill (spec C §3.4): wall-clock time inside a filter, a
// WHERE or a partition value, instead of the run's logical date.
const ruleWallClock = "wall-clock-filter"

const wallClockMsg = "wall-clock time in a filter or partition value; use the run's logical date (ds, data_interval_start, a partition key or a parameter) so reruns and backfills select the same rows"

var (
	// Python code (strings blanked).
	pyWallClockRe = regexp.MustCompile(`\b(?:datetime\s*\.\s*(?:now|utcnow|today)|date\s*\.\s*today|pendulum\s*\.\s*(?:now|today)|timezone\s*\.\s*utcnow|current_date|current_timestamp|getdate|now)\s*\(`)
	pyFilterRe    = regexp.MustCompile(`\.(?:filter|where|query|isin|between)\s*\(|\.loc\s*\[|(?i:\bpartition\w*)\s*=`)
	pySubscriptRe = regexp.MustCompile(`[\w)\]]\s*\[`) // a subscript, not a list literal
	pyMaskOpRe    = regexp.MustCompile(`==|!=|[<>&|~]`)
	pyAssignRe    = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?::\s*[\w.\[\], ]+)?=[^=]`)
	// SQL text (in .sql files and Python string literals).
	sqlWallClockRe = regexp.MustCompile(`(?i)\b(?:current_date|current_timestamp|getdate\s*\(|getutcdate\s*\(|sysdate|sysdatetime\s*\(|now\s*\(|today\s*\()`)
	sqlFilterRe    = regexp.MustCompile(`(?i)\b(?:where|having|qualify|partition)\b`)
)

var sqlWallClockWords = map[string]bool{"CURRENT_DATE": true, "CURRENT_TIMESTAMP": true, "GETDATE": true, "GETUTCDATE": true, "SYSDATE": true, "SYSDATETIME": true, "NOW": true, "TODAY": true}

var sqlFilterWords = map[string]bool{"WHERE": true, "HAVING": true, "QUALIFY": true, "PARTITION": true, "ON": true}

// sqlClauseEnd ends a filter clause: a later SELECT list is not a filter.
var sqlClauseEnd = map[string]bool{"SELECT": true, "GROUP": true, "ORDER": true, "LIMIT": true, "UNION": true, "WINDOW": true,
	"SET": true, "WHEN": true, "THEN": true, "VALUES": true, "BY": true}

func runPipelineBackfill(_ context.Context, in *packs.Input) packs.Result {
	sel := loadUnits(in)
	cp := pipelineContractModels(in)
	var fs findings
	n := 0
	for _, u := range sel.units {
		level := packs.LevelWarn
		if matchAny(cp, u.path) {
			level = packs.LevelError
		}
		switch u.kind {
		case "python":
			n++
			backfillPython(u, level, &fs)
		case "sql":
			n++
			backfillSQL(u, level, &fs)
		}
	}
	return result(n, "pipeline file", sel.skipped, &fs)
}

func backfillPython(u *unit, level string, fs *findings) {
	tainted := map[string]bool{}
	for _, l := range scan.PythonLines(u.src) {
		code := l.Code
		wall := pyWallClockRe.MatchString(code)
		spans := filterSpans(code)
		if m := pyAssignRe.FindStringSubmatch(code); m != nil && wall && len(spans) == 0 {
			tainted[m[1]] = true
			continue
		}
		hit := false
		for _, span := range spans {
			if pyWallClockRe.MatchString(span) {
				hit = true
			}
			for v := range tainted {
				if wordIn(span, v) {
					hit = true
				}
			}
		}
		if !hit {
			for _, s := range l.Strings {
				if !sqlFilterRe.MatchString(s) {
					continue
				}
				if sqlTextWallClock(s) {
					hit = true
					break
				}
				for v := range tainted {
					if strings.Contains(s, "{"+v+"}") || strings.Contains(s, "{"+v+":") {
						hit = true
					}
				}
			}
		}
		if hit {
			fs.add(u, l.Line, level, ruleWallClock, wallClockMsg)
		}
	}
}

// filterSpans returns the argument text of every filter context in a
// logical line: the parentheses of .filter( .where( .query( .isin(
// .between(, the brackets of .loc[, the brackets of a boolean-mask
// subscript (pandas `df[df.d == x]`: a subscript holding a comparison or a
// & | ~ mask operator; `df["col"]`, `df[0:10]` and column lists are not
// filters), and the value after a partition name up to the next top-level
// comma.
func filterSpans(code string) []string {
	var out []string
	for _, m := range pySubscriptRe.FindAllStringIndex(code, -1) {
		end := m[1]
		if span := code[end:closeOf(code, end)]; pyMaskOpRe.MatchString(span) {
			out = append(out, span)
		}
	}
	for _, m := range pyFilterRe.FindAllStringIndex(code, -1) {
		end := m[1]
		if c := code[end-1]; c == '(' || c == '[' {
			out = append(out, code[end:closeOf(code, end)])
			continue
		}
		if end < len(code) && code[end] == '=' {
			continue // partition_x == y is a comparison, not a value
		}
		out = append(out, code[end:argEnd(code, end)])
	}
	return out
}

// closeOf returns the index of the bracket closing the group opened just
// before i (len(code) when unclosed).
func closeOf(code string, i int) int {
	depth := 1
	for k := i; k < len(code); k++ {
		switch code[k] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(code)
}

// argEnd returns the end of the argument that starts at i: the next comma
// or closing bracket at depth 0.
func argEnd(code string, i int) int {
	depth := 0
	for k := i; k < len(code); k++ {
		switch code[k] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return k
			}
			depth--
		case ',':
			if depth == 0 {
				return k
			}
		}
	}
	return len(code)
}

// sqlTextWallClock reports a wall-clock function after a filter keyword in
// a SQL string.
func sqlTextWallClock(s string) bool {
	f := sqlFilterRe.FindStringIndex(s)
	if f == nil {
		return false
	}
	return sqlWallClockRe.MatchString(s[f[0]:])
}

func backfillSQL(u *unit, level string, fs *findings) {
	for _, st := range scan.SQLStatements(u.src) {
		inFilter := false
		for _, t := range st.Tokens {
			if t.Kind != scan.TokWord && t.Kind != scan.TokIdent {
				continue
			}
			up := t.Upper()
			switch {
			case sqlFilterWords[up]:
				inFilter = true
			case sqlClauseEnd[up]:
				inFilter = false
			case inFilter && sqlWallClockWords[up]:
				fs.add(u, t.Line, level, ruleWallClock, wallClockMsg)
			}
		}
	}
}

func wordIn(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isWordByte(s[j-1])
		after := j+len(w) >= len(s) || !isWordByte(s[j+len(w)])
		if before && after {
			return true
		}
		i = j + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
