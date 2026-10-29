package iam

import (
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// policyDocumentRead reports a Terraform aws_iam_policy_document data
// source that the plan reads at apply time: its rendered json is unknown,
// but its statement blocks are in change.after.
func policyDocumentRead(r model.Resource) bool {
	return r.Source == model.KindTFPlan && r.Meta["mode"] == "data" && r.After != nil &&
		strings.EqualFold(r.Type, "aws_iam_policy_document") && r.Has(model.ActRead)
}

// policyDocument lints a deferred aws_iam_policy_document: its HCL-shaped
// statement blocks, mapped onto an IAM document, plus any source or
// override documents that are already known. A statement field that is
// unknown (null) is left out, so the rules it would feed are not judged.
func policyDocument(r model.Resource, o Options) Result {
	var hits []hit
	docs := 0
	if st := model.List(r.After["statement"]); len(st) > 0 {
		docs++
		iss, _ := lintDoc(hclDocument(st), docAuto, o)
		for _, i := range iss {
			hits = append(hits, hit{i.Rule, i.Level, i.Msg})
		}
	}
	for _, key := range []string{"source_policy_documents", "override_policy_documents", "source_json", "override_json"} {
		for n, v := range model.List(r.After[key]) {
			iss, ok := lintDoc(v, docAuto, o)
			if !ok {
				continue
			}
			docs++
			for _, i := range iss {
				hits = append(hits, hit{i.Rule, i.Level, key + "[" + strconv.Itoa(n) + "]: " + i.Msg})
			}
		}
	}
	return suppress(r, hits, docs)
}

// hclDocument maps aws_iam_policy_document statement blocks (effect,
// actions, not_actions, resources, not_resources, principals[].{type,
// identifiers}, not_principals, condition[].{test, variable, values}) onto
// the IAM JSON shape decodeDoc reads.
func hclDocument(stmts []any) map[string]any {
	var out []any
	for _, x := range stmts {
		m := model.Map(x)
		if m == nil {
			continue
		}
		st := map[string]any{}
		effect, _ := model.Str(m["effect"])
		if effect == "" {
			effect = "Allow"
		}
		st["Effect"] = effect
		for hk, key := range map[string]string{"actions": "Action", "not_actions": "NotAction", "resources": "Resource", "not_resources": "NotResource"} {
			if l := model.List(m[hk]); len(l) > 0 {
				st[key] = l
			}
		}
		if pr := hclPrincipals(m["principals"]); pr != nil {
			st["Principal"] = pr
		}
		if pr := hclPrincipals(m["not_principals"]); pr != nil {
			st["NotPrincipal"] = pr
		}
		cond := map[string]any{}
		for _, c := range model.List(m["condition"]) {
			cm := model.Map(c)
			test, _ := model.Str(cm["test"])
			variable, _ := model.Str(cm["variable"])
			if test == "" || variable == "" {
				continue
			}
			ops, _ := cond[test].(map[string]any)
			if ops == nil {
				ops = map[string]any{}
				cond[test] = ops
			}
			ops[variable] = cm["values"]
		}
		if len(cond) > 0 {
			st["Condition"] = cond
		}
		out = append(out, st)
	}
	return map[string]any{"Statement": out}
}

// hclPrincipals: principals { type = "AWS", identifiers = [...] } blocks as
// a Principal map; type "*" is Principal "*". Unknown identifiers stay nil.
func hclPrincipals(v any) any {
	pm := map[string]any{}
	for _, x := range model.List(v) {
		m := model.Map(x)
		typ, _ := model.Str(m["type"])
		if typ == "" {
			continue
		}
		if typ == "*" {
			return "*"
		}
		ids := model.List(m["identifiers"])
		if ids == nil {
			ids = []any{nil} // unknown at plan time: principals() reads it as unresolved
		}
		prev, _ := pm[typ].([]any)
		pm[typ] = append(prev, ids...)
	}
	if len(pm) == 0 {
		return nil
	}
	return pm
}
