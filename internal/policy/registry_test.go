package policy

import "testing"

func TestRegisterBuiltinAppendsAndRejectsDuplicates(t *testing.T) {
	n := len(Builtin())
	saved := registered
	t.Cleanup(func() { registryMu.Lock(); registered = saved; registryMu.Unlock() })
	RegisterBuiltin(Rule{ID: "test/registered", Action: Ask, Command: `\bzzz\b`, Reason: "test"})
	all := Builtin()
	if len(all) != n+1 || all[len(all)-1].ID != "test/registered" {
		t.Fatalf("registered rule missing: %d -> %d", n, len(all))
	}
	all[len(all)-1].ID = "mutated"
	if Builtin()[n].ID != "test/registered" {
		t.Fatal("Builtin must return a copy")
	}
	for _, r := range []Rule{{ID: "test/registered"}, {ID: "tq/force-push"}, {}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RegisterBuiltin(%q) must panic", r.ID)
				}
			}()
			RegisterBuiltin(r)
		}()
	}
}

func mustSet(t *testing.T, rs ...Rule) *Set {
	t.Helper()
	s, errs := Build(Layers{Manifest: rs})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return s
}

func TestEvaluateMappedRanksAfterMapping(t *testing.T) {
	s := mustSet(t,
		Rule{ID: "t/deny", Action: Deny, Command: `push`, Reason: "d"},
		Rule{ID: "t/ask", Action: Ask, Command: `push`, Reason: "a"},
	)
	call := ToolCall{Tool: "Bash", Command: "git push"}
	if d, ch := s.EvaluateMapped(call, nil); d.Action != Deny || len(ch) != 0 {
		t.Fatalf("nil mapper is Evaluate: %+v %v", d, ch)
	}
	skipDeny := func(r *Rule) Action {
		if r.ID == "t/deny" {
			return Allow
		}
		return r.Action
	}
	d, ch := s.EvaluateMapped(call, skipDeny)
	if d.Action != Ask || len(d.Matched) != 1 || d.Matched[0].ID != "t/ask" {
		t.Fatalf("a skipped deny must not hide the ask: %+v", d)
	}
	if len(ch) != 1 || ch[0].Rule.ID != "t/deny" || ch[0].To != Allow {
		t.Fatalf("changed: %+v", ch)
	}
	lower := func(r *Rule) Action { return Ask }
	if d, ch := s.EvaluateMapped(call, lower); d.Action != Ask || len(d.Matched) != 2 || len(ch) != 1 || ch[0].To != Ask {
		t.Fatalf("lowered deny: %+v %+v", d, ch)
	}
}

func TestMapDecision(t *testing.T) {
	a := &Rule{ID: "t/a", Action: Deny}
	b := &Rule{ID: "t/b", Action: Deny}
	d := Decision{Action: Deny, Matched: []*Rule{a, b}}
	if got, ch := MapDecision(d, nil); got.Action != Deny || ch != nil {
		t.Fatal("nil mapper leaves d")
	}
	got, ch := MapDecision(d, func(r *Rule) Action {
		if r.ID == "t/a" {
			return Allow
		}
		return Ask
	})
	if got.Action != Ask || len(got.Matched) != 1 || got.Matched[0].ID != "t/b" || len(ch) != 2 {
		t.Fatalf("%+v %+v", got, ch)
	}
	if got, _ := MapDecision(d, func(*Rule) Action { return Allow }); got.Action != Allow || len(got.Matched) != 0 {
		t.Fatalf("all skipped is Allow: %+v", got)
	}
}
