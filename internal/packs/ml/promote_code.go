package ml

// ml/promotion-in-code (spec C §4.3): code must not call a registry
// promotion API with a protected target outside the declared promotion
// script. Promotions go through tq pack ml promote.

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// promotionAPIs: the call name and where its target is (positional index
// and keyword). update_model_package's target is ModelApprovalStatus.
var promotionAPIs = map[string]struct {
	pos int
	kw  string
}{
	"transition_model_version_stage": {2, "stage"},
	"set_registered_model_alias":     {1, "alias"},
	"update_model_package":           {-1, "ModelApprovalStatus"},
}

const defaultPromoteScript = "scripts/promote.py"

func isPythonish(p string) bool {
	return strings.HasSuffix(p, ".py") || strings.HasSuffix(p, ".ipynb")
}

func isTestPath(p string) bool {
	base := path.Base(p)
	return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || strings.Contains("/"+p, "/tests/") || strings.Contains("/"+p, "/test/")
}

// promotionScripts are the files allowed to call the promotion APIs.
func promotionScripts(st Settings) []string {
	out := []string{defaultPromoteScript}
	return append(out, scriptWords(st.Promotion.Command)...)
}

func promotionInCodeApplies(in *packs.Input) bool {
	for _, p := range in.ChangedPaths() {
		if isPythonish(p) {
			return true
		}
	}
	return false
}

type argSpan struct {
	start, end int
	kw         string
}

// callArgs splits the argument list of the call whose "(" is at open.
func callArgs(code string, open int) []argSpan {
	var out []argSpan
	depth, start := 0, open+1
	add := func(end int) {
		s := code[start:end]
		a := argSpan{start: start, end: end}
		if m := kwRe.FindStringSubmatch(s); m != nil && !strings.HasPrefix(s[len(m[0]):], "=") {
			a.kw = m[1]
			a.start = start + len(m[0])
		}
		if strings.TrimSpace(code[a.start:a.end]) != "" {
			out = append(out, a)
		}
	}
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				add(i)
				return out
			}
		case ',':
			if depth == 1 {
				add(i)
				start = i + 1
			}
		}
	}
	return out
}

var kwRe = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*=`)

// target returns the target argument's literal value, whether it is a plain
// string literal, and whether the argument is present at all.
func target(l scan.PyLine, open, pos int, kw string) (val string, literal, present bool) {
	args := callArgs(l.Code, open)
	var a *argSpan
	positional := 0
	for i := range args {
		if args[i].kw != "" {
			if args[i].kw == kw {
				a = &args[i]
			}
			continue
		}
		if positional == pos && a == nil {
			a = &args[i]
		}
		positional++
	}
	if a == nil {
		return "", false, false
	}
	expr := strings.TrimSpace(l.Code[a.start:a.end])
	if expr != `""` && expr != `''` {
		return "", false, true
	}
	lead := strings.TrimLeft(l.Code[a.start:a.end], " \t")
	idx := countLiterals(l.Code[:a.end-len(lead)])
	if idx < len(l.Strings) {
		return l.Strings[idx], true, true
	}
	return "", false, true
}

// countLiterals counts the string literals in blanked code. scan.PythonLines
// turns each literal into two double or two single quotes, by its quote, and
// every quote left in the code belongs to such a pair, so the pairs are
// counted in source order.
func countLiterals(code string) int {
	n := 0
	for i := 0; i+1 < len(code); i++ {
		if (code[i] == '"' || code[i] == '\'') && code[i+1] == code[i] {
			n++
			i++
		}
	}
	return n
}

// promotionFindings scans one Python source; fileLine maps source lines to
// file lines (notebooks).
func promotionFindings(file string, src []byte, fileLine func(int) int, st Settings, test bool) []packs.Finding {
	var fs []packs.Finding
	for _, l := range scan.PythonLines(src) {
		seen := map[string]int{}
		for _, c := range scan.LineCalls(l) {
			api, ok := promotionAPIs[c.Name]
			if !ok {
				continue
			}
			// the n-th "name(" of this line is this call
			n := seen[c.Name]
			seen[c.Name]++
			open := nthIndex(l.Code, c.Name+"(", n)
			if open < 0 {
				continue
			}
			open += len(c.Name)
			val, literal, present := target(l, open, api.pos, api.kw)
			level, msg := "", ""
			switch {
			case c.Name == "update_model_package":
				if present && literal && strings.EqualFold(val, "Approved") {
					level, msg = packs.LevelError, "update_model_package approves a model package: promote through tq pack ml promote request"
				} else if present && !literal {
					level, msg = packs.LevelWarn, "update_model_package sets ModelApprovalStatus from a variable: route approvals through tq pack ml promote"
				}
			case !present:
			case literal && st.Protected(val):
				level, msg = packs.LevelError, fmt.Sprintf("%s to protected target %q: promote through tq pack ml promote request", c.Name, strings.ToLower(val))
			case !literal:
				level, msg = packs.LevelWarn, c.Name+" with a target tq cannot read: route promotions to protected targets through tq pack ml promote"
			}
			if level == "" {
				continue
			}
			if test && level == packs.LevelError {
				level = packs.LevelWarn
			}
			line := l.Line
			if fileLine != nil {
				line = fileLine(l.Line)
			}
			fs = append(fs, packs.Finding{File: file, Line: line, Level: level, Rule: "promotion-in-code", Message: msg})
		}
	}
	return fs
}

func nthIndex(s, sub string, n int) int {
	off := 0
	for i := 0; ; i++ {
		j := strings.Index(s[off:], sub)
		if j < 0 {
			return -1
		}
		// a match must start a name, not end one ("xset_registered_model_alias(")
		at := off + j
		if at > 0 && (isWord(s[at-1])) {
			off = at + len(sub)
			i--
			continue
		}
		if i == n {
			return at
		}
		off = at + len(sub)
	}
}

func isWord(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func runPromotionInCode(ctx context.Context, in *packs.Input) packs.Result {
	st := loadIn(in)
	allowed := promotionScripts(st)
	var fs []packs.Finding
	n := 0
	for _, p := range in.ChangedPaths() {
		if !isPythonish(p) {
			continue
		}
		skip := false
		for _, a := range allowed {
			if p == a {
				skip = true
			}
		}
		if skip {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		raw, err := in.ReadNew(p, 0)
		if err != nil {
			continue // deleted or unreadable: nothing to scan
		}
		n++
		test := isTestPath(p)
		if strings.HasSuffix(p, ".ipynb") {
			cells, err := scan.NotebookCells(p, raw)
			if err != nil {
				fs = append(fs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "promotion-in-code", Message: "the notebook cannot be parsed: " + err.Error()})
				continue
			}
			src, fl := scan.CodeSource(cells)
			fs = append(fs, promotionFindings(p, src, fl, st, test)...)
			continue
		}
		fs = append(fs, promotionFindings(p, raw, nil, st, test)...)
	}
	return packs.Result{Status: packs.OK, Detail: plural(n, "Python file") + " scanned for registry promotion calls", Findings: fs}
}
