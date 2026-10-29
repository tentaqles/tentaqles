package data

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

// changedSQLModels returns the changed .sql models (under a project's
// model paths) with their project.
func changedSQLModels(r *dbtRun) [][2]any {
	var out [][2]any
	for _, c := range r.in.Changed {
		if c.Status == 'D' || !strings.EqualFold(path.Ext(c.Path), ".sql") {
			continue
		}
		p := projectOf(r.projects, c.Path)
		if p == nil || !isModelPath(p, c.Path) {
			continue
		}
		out = append(out, [2]any{p, c.Path})
	}
	return out
}

// isStaging reports a staging model: under a staging/ folder or named stg_*.
func isStaging(rel string) bool {
	if strings.HasPrefix(path.Base(rel), "stg_") {
		return true
	}
	for _, s := range strings.Split(path.Dir(rel), "/") {
		if strings.EqualFold(s, "staging") {
			return true
		}
	}
	return false
}

// ---- data/sql-select-star

func runSelectStar(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	var fs []packs.Finding
	models := changedSQLModels(r)
	for _, pm := range models {
		if ctx.Err() != nil {
			break
		}
		p, file := pm[0].(*dbtProject), pm[1].(string)
		src, err := in.ReadNew(file, maxFileBytes)
		if err != nil {
			continue
		}
		rel, _ := p.underModels(file)
		contracted := p.matchContract(r.cfg.ContractPaths, file)
		staging := isStaging(rel)
		for _, st := range scan.SQLStatements(src) {
			for _, s := range selectStars(st) {
				if staging && s.fromSource {
					continue
				}
				f := packs.Finding{File: file, Line: s.line, Level: packs.LevelWarn, Rule: "select-star",
					Message: fmt.Sprintf("select * in model %s: list the columns", modelName(file))}
				if contracted {
					f.Level = packs.LevelError
					f.Message = fmt.Sprintf("select * in contracted model %s: list the columns the contract promises", modelName(file))
				}
				fs = append(fs, f)
			}
		}
	}
	fs = newSuppressor(in, idSelectStar).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(len(models), "SQL model"), Findings: fs}
}

type star struct {
	line       int
	fromSource bool // the SELECT reads straight from {{ source(...) }}
}

// selectStars finds every SELECT * / SELECT t.* of a statement and whether
// its SELECT's FROM target is a source() call.
func selectStars(st scan.Statement) []star {
	type frame struct {
		depth int
		stars []int
	}
	toks := st.Tokens
	var stack []frame
	var out []star
	depth := 0
	resolve := func(fromSource bool) {
		top := stack[len(stack)-1]
		for _, l := range top.stars {
			out = append(out, star{l, fromSource})
		}
		stack = stack[:len(stack)-1]
	}
	for i, t := range toks {
		if t.Kind == scan.TokPunct {
			depth += strings.Count(t.Text, "(") - strings.Count(t.Text, ")")
			for len(stack) > 0 && depth < stack[len(stack)-1].depth {
				resolve(false)
			}
		}
		switch {
		case t.IsWord("SELECT"):
			stack = append(stack, frame{depth: depth})
		case t.IsWord("FROM") && len(stack) > 0 && stack[len(stack)-1].depth == depth:
			src := i+1 < len(toks) && toks[i+1].Kind == scan.TokJinja && strings.Contains(toks[i+1].Text, "source(")
			resolve(src)
		case t.Kind == scan.TokPunct && t.Text == "*" && len(stack) > 0 && stack[len(stack)-1].depth == depth:
			prev := scan.Token{}
			if i > 0 {
				prev = toks[i-1]
			}
			dot := prev.Kind == scan.TokPunct && prev.Text == "."
			lead := prev.IsWord("SELECT") || prev.IsWord("DISTINCT") || prev.IsWord("ALL") || (prev.Kind == scan.TokPunct && prev.Text == ",")
			nextNum := i+1 < len(toks) && toks[i+1].Kind == scan.TokNumber
			if dot || (lead && !nextNum) {
				stack[len(stack)-1].stars = append(stack[len(stack)-1].stars, t.Line)
			}
		}
	}
	for len(stack) > 0 {
		resolve(false)
	}
	return out
}

// ---- data/sql-hardcoded-relation

func runHardcodedRelation(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	var fs []packs.Finding
	models := changedSQLModels(r)
	for _, pm := range models {
		if ctx.Err() != nil {
			break
		}
		file := pm[1].(string)
		src, err := in.ReadNew(file, maxFileBytes)
		if err != nil {
			continue
		}
		for _, st := range scan.SQLStatements(src) {
			for _, rel := range literalRelations(st) {
				fs = append(fs, packs.Finding{File: file, Line: rel.Line, Level: packs.LevelError, Rule: "hardcoded-relation",
					Message: fmt.Sprintf("model %s reads %s as a literal: use ref() or source()", modelName(file), rel.Name)})
			}
		}
	}
	fs = newSuppressor(in, idHardcodedRel).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(len(models), "SQL model"), Findings: fs}
}

// literalRelations returns the FROM/JOIN targets of a statement, at any
// depth (CTE bodies, IN/EXISTS subqueries, derived tables), that name a
// relation literally: a dotted name (schema.table, db.schema.table), or a
// Jinja expression other than ref()/source()/this followed by .name
// ({{ target.schema }}.orders). A bare name is a CTE or an alias, and a name
// followed by ( is a table function. scan.Relations is not used because it
// skips a FROM inside IN (...) as if it were a function call.
func literalRelations(st scan.Statement) []scan.Relation {
	toks := st.Tokens
	var out []scan.Relation
	for i, t := range toks {
		if !(t.IsWord("FROM") || t.IsWord("JOIN")) {
			continue
		}
		// EXTRACT(year FROM x), SUBSTRING(x FROM 2): not a relation
		if t.IsWord("FROM") && fromInFunction(toks, i) {
			continue
		}
		list := t.IsWord("FROM")
		j := i + 1
		for j < len(toks) {
			if toks[j].IsWord("LATERAL") || toks[j].IsWord("ONLY") {
				j++
				continue
			}
			rel, next, ok := literalRelationAt(toks, j)
			if !ok {
				break // a subquery: its own FROM is visited by this loop
			}
			if rel.Name != "" {
				out = append(out, rel)
			}
			j = next
			if j < len(toks) && toks[j].IsWord("AS") {
				j++
			}
			if j < len(toks) && (toks[j].Kind == scan.TokWord || toks[j].Kind == scan.TokIdent) && !sqlReserved(toks[j]) {
				j++
			}
			if list && j < len(toks) && toks[j].Kind == scan.TokPunct && toks[j].Text == "," {
				j++
				continue
			}
			break
		}
	}
	return out
}

// literalRelationAt reads the relation at toks[j]. ok is false for a
// subquery; rel.Name is "" for a target that is not a literal.
func literalRelationAt(toks []scan.Token, j int) (scan.Relation, int, bool) {
	if j >= len(toks) {
		return scan.Relation{}, j, false
	}
	t := toks[j]
	dotted := func(k int) ([]string, int) {
		var parts []string
		for k+1 < len(toks) && toks[k].Kind == scan.TokPunct && toks[k].Text == "." && (toks[k+1].Kind == scan.TokWord || toks[k+1].Kind == scan.TokIdent) {
			parts = append(parts, toks[k+1].Text)
			k += 2
		}
		return parts, k
	}
	callAt := func(k int) bool { return k < len(toks) && toks[k].Kind == scan.TokPunct && toks[k].Text == "(" }
	switch {
	case t.Kind == scan.TokPunct && t.Text == "(":
		return scan.Relation{}, j, false
	case t.Kind == scan.TokJinja:
		parts, k := dotted(j + 1)
		if len(parts) == 0 || lineageJinja(t.Text) || callAt(k) {
			return scan.Relation{Line: t.Line, Jinja: true}, k, true
		}
		return scan.Relation{Name: t.Text + "." + strings.Join(parts, "."), Line: t.Line, Jinja: true}, k, true
	case (t.Kind == scan.TokWord && !sqlReserved(t)) || t.Kind == scan.TokIdent:
		parts, k := dotted(j + 1)
		if len(parts) == 0 || callAt(k) {
			return scan.Relation{Line: t.Line}, k, true
		}
		return scan.Relation{Name: t.Text + "." + strings.Join(parts, "."), Line: t.Line}, k, true
	}
	return scan.Relation{}, j, false
}

// lineageJinja is a Jinja expression dbt resolves through the DAG.
func lineageJinja(s string) bool {
	inner := strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "{}-"))
	return strings.Contains(s, "ref(") || strings.Contains(s, "source(") || inner == "this"
}

// fromInFunction reports a FROM whose innermost enclosing parenthesis holds
// no SELECT: a function argument (EXTRACT, SUBSTRING, TRIM), not a subquery.
func fromInFunction(toks []scan.Token, i int) bool {
	depth := 0
	for k := i - 1; k >= 0; k-- {
		t := toks[k]
		if t.Kind == scan.TokPunct {
			depth += strings.Count(t.Text, ")")
			opens := strings.Count(t.Text, "(")
			if opens > depth {
				return true // reached the enclosing ( with no SELECT at this level
			}
			depth -= opens
			continue
		}
		if depth == 0 && t.IsWord("SELECT") {
			return false
		}
	}
	return false
}

var sqlReservedWords = map[string]bool{"WHERE": true, "JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true,
	"OUTER": true, "FULL": true, "CROSS": true, "ON": true, "USING": true, "GROUP": true, "ORDER": true, "HAVING": true,
	"LIMIT": true, "UNION": true, "EXCEPT": true, "INTERSECT": true, "WINDOW": true, "QUALIFY": true, "NATURAL": true,
	"SET": true, "VALUES": true, "SELECT": true, "FROM": true, "OFFSET": true, "FETCH": true, "RETURNING": true,
	"WITH": true, "AS": true, "LATERAL": true, "PIVOT": true, "UNPIVOT": true, "SAMPLE": true, "TABLESAMPLE": true,
	"FOR": true, "INTO": true, "AND": true, "OR": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true}

func sqlReserved(t scan.Token) bool { return t.Kind == scan.TokWord && sqlReservedWords[t.Upper()] }
