package policy_test

import (
	"slices"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func matchedIDs(d policy.Decision) []string {
	var ids []string
	for _, r := range d.Matched {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

// Guard mode default equals Evaluate (main before spec E) over the built-in
// decision corpus: the same action, the same matched rules, nothing changed
// (spec E §16).
func TestDefaultModeEqualsEvaluate(t *testing.T) {
	s, errs := policy.Build(policy.Layers{Builtin: policy.Builtin()})
	if len(errs) > 0 {
		t.Fatalf("builtin rules do not compile: %v", errs)
	}
	def := guardcfg.DefaultResolved()
	if !def.IsDefault() {
		t.Fatal("DefaultResolved must take the hook's fast path")
	}
	calls := policy.BuiltinCorpus()
	if len(calls) < 50 {
		t.Fatalf("corpus too small: %d", len(calls))
	}
	for _, call := range calls {
		want := s.Evaluate(call)
		got, changed := s.EvaluateMapped(call, def.Effective)
		if got.Action != want.Action || !slices.Equal(matchedIDs(got), matchedIDs(want)) {
			t.Errorf("%+v: default %s %v, Evaluate %s %v", call, got.Action, matchedIDs(got), want.Action, matchedIDs(want))
		}
		if len(changed) != 0 {
			t.Errorf("%+v: default changed %v", call, changed)
		}
		if mapped, ch := policy.MapDecision(want, def.Effective); mapped.Action != want.Action || len(ch) != 0 {
			t.Errorf("%+v: MapDecision under default: %s %v", call, mapped.Action, ch)
		}
	}
}
