package policy

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func ruleIDs(rs []*Rule) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

// / EvaluateMapped keeps the unattended branch's property: a skipped or
// lowered stricter rule never hides a weaker one, and only the rules whose
// action changed are reported.
func TestEvaluateMappedUncoversWeakerRules(t *testing.T) {
	s := builtinSet(t)
	call := ToolCall{Tool: "Bash", Command: "git push --force origin main"}
	if d := s.Evaluate(call); d.Action != Deny || d.Matched[0].ID != "tq/force-push-main" {
		t.Fatalf("baseline: %+v", ruleIDs(d.Matched))
	}
	skipMain := func(r *Rule) Action {
		if r.ID == "tq/force-push-main" {
			return Allow
		}
		return r.Action
	}
	d, changed := s.EvaluateMapped(call, skipMain)
	if d.Action != Ask || len(d.Matched) != 1 || d.Matched[0].ID != "tq/force-push" {
		t.Fatalf("decision = %s %v", d.Action, ruleIDs(d.Matched))
	}
	if len(changed) != 1 || changed[0].Rule.ID != "tq/force-push-main" || changed[0].To != Allow {
		t.Fatalf("changed = %+v", changed)
	}
	all := func(*Rule) Action { return Allow }
	d, changed = s.EvaluateMapped(call, all)
	if d.Action != Allow || len(changed) != 2 {
		t.Fatalf("skip all: %s %d", d.Action, len(changed))
	}
	d, changed = s.EvaluateMapped(call, nil)
	if d.Action != Deny || len(changed) != 0 {
		t.Fatalf("nil mapper must be Evaluate: %s %d", d.Action, len(changed))
	}
	// A deny lowered to ask ties with the other ask: both are listed.
	lower := func(r *Rule) Action {
		if r.Action == Deny {
			return Ask
		}
		return r.Action
	}
	d, changed = s.EvaluateMapped(call, lower)
	if d.Action != Ask || len(d.Matched) != 2 || len(changed) != 1 || changed[0].To != Ask {
		t.Fatalf("lowered: %s %v %+v", d.Action, ruleIDs(d.Matched), changed)
	}
}

func TestRuleUnattendedField(t *testing.T) {
	var f struct {
		Rules []Rule `yaml:"rules"`
	}
	src := "rules:\n  - id: acme/no-bar\n    action: deny\n    command: '\\bbar\\b'\n    reason: no bar\n    unattended: enforce\n"
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Rules) != 1 || f.Rules[0].Unattended != UnattendedEnforce {
		t.Fatalf("rules = %+v", f.Rules)
	}
	if err := f.Rules[0].Compile(); err != nil {
		t.Fatal(err)
	}
}
