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
			for _, rel := range st.Relations() {
				if rel.Jinja || !strings.Contains(rel.Name, ".") {
					continue
				}
				fs = append(fs, packs.Finding{File: file, Line: rel.Line, Level: packs.LevelError, Rule: "hardcoded-relation",
					Message: fmt.Sprintf("model %s reads %s as a literal: use ref() or source()", modelName(file), rel.Name)})
			}
		}
	}
	fs = newSuppressor(in, idHardcodedRel).filter(fs)
	return packs.Result{Status: packs.OK, Detail: plural(len(models), "SQL model"), Findings: fs}
}
