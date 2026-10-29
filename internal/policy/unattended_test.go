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

// EvaluateExcept leaves the skipped rules out of the decision, so a weaker
// rule they hid decides, and reports every skipped rule that matched.
func TestEvaluateExceptUncoversWeakerRules(t *testing.T) {
	s := builtinSet(t)
	call := ToolCall{Tool: "Bash", Command: "git push --force origin main"}
	if d := s.Evaluate(call); d.Action != Deny || d.Matched[0].ID != "tq/force-push-main" {
		t.Fatalf("baseline: %+v", ruleIDs(d.Matched))
	}
	skipMain := func(r *Rule) bool { return r.ID == "tq/force-push-main" }
	d, skipped := s.EvaluateExcept(call, skipMain)
	if d.Action != Ask || len(d.Matched) != 1 || d.Matched[0].ID != "tq/force-push" {
		t.Fatalf("decision = %s %v", d.Action, ruleIDs(d.Matched))
	}
	if got := ruleIDs(skipped); len(got) != 1 || got[0] != "tq/force-push-main" {
		t.Fatalf("skipped = %v", got)
	}
	all := func(*Rule) bool { return true }
	d, skipped = s.EvaluateExcept(call, all)
	if d.Action != Allow || len(skipped) != 2 {
		t.Fatalf("skip all: %s %v", d.Action, ruleIDs(skipped))
	}
	d, skipped = s.EvaluateExcept(call, nil)
	if d.Action != Deny || len(skipped) != 0 {
		t.Fatalf("nil skip must be Evaluate: %s %v", d.Action, ruleIDs(skipped))
	}
}

func TestSplitDecision(t *testing.T) {
	d := Decision{Action: Deny, Matched: []*Rule{{ID: "a", Action: Deny}, {ID: "b", Action: Deny}}}
	kept, skipped := SplitDecision(d, func(r *Rule) bool { return r.ID == "a" })
	if kept.Action != Deny || len(kept.Matched) != 1 || kept.Matched[0].ID != "b" || len(skipped) != 1 {
		t.Fatalf("kept %v skipped %v", ruleIDs(kept.Matched), ruleIDs(skipped))
	}
	kept, skipped = SplitDecision(d, func(*Rule) bool { return true })
	if kept.Action != Allow || len(kept.Matched) != 0 || len(skipped) != 2 {
		t.Fatalf("kept %+v skipped %v", kept, ruleIDs(skipped))
	}
	kept, _ = SplitDecision(d, nil)
	if kept.Action != Deny || len(kept.Matched) != 2 {
		t.Fatalf("nil skip keeps everything: %+v", kept)
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
