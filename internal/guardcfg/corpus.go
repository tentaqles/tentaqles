package guardcfg

import (
	"sync"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// The built-in corpus: every rule id tq ships with its own default
// action(s), and per category the actions its rules take. It lets the
// table-level views (the clamp, LooserThanDefault, grant checks) ask "does
// this value lower some rule here?" exactly for the built-ins, while
// Resolved.Effective stays exact for every rule.
var (
	corpusOnce  sync.Once
	corpusByID  map[string][]policy.Action
	corpusByCat map[Category][]policy.Action
)

func loadCorpus() {
	corpusOnce.Do(func() {
		corpusByID = map[string][]policy.Action{}
		add := func(id string, a policy.Action) {
			for _, x := range corpusByID[id] {
				if x == a {
					return
				}
			}
			corpusByID[id] = append(corpusByID[id], a)
		}
		add(GuardErrorID, policy.Ask)
		for _, r := range policy.Builtin() {
			add(r.ID, r.Action)
		}
		for id := range HookRules {
			add(id, HookRuleAction(id))
		}
		for _, r := range decide.BuiltinJudgeRules() {
			a := policy.Ask
			if r.Max == decide.Deny {
				a = policy.Deny
			}
			add(r.ID, a)
		}
		for _, id := range IdentityGuardIDs {
			add(id, policy.Deny)
		}
		corpusByCat = map[Category][]policy.Action{}
		for id, as := range corpusByID {
			c := Categories[id]
			if c == "" {
				continue
			}
			for _, a := range as {
				dup := false
				for _, x := range corpusByCat[c] {
					dup = dup || x == a
				}
				if !dup {
					corpusByCat[c] = append(corpusByCat[c], a)
				}
			}
		}
	})
}

// BuiltinAction returns the strictest default action of built-in id, and
// false for an id tq does not ship.
func BuiltinAction(id string) (policy.Action, bool) {
	loadCorpus()
	as, ok := corpusByID[id]
	if !ok {
		return policy.Allow, false
	}
	best := policy.Allow
	for _, a := range as {
		if a.StricterThan(best) {
			best = a
		}
	}
	return best, true
}

// lowers reports whether value v gives some action in owns a looser action
// than w does. With no known actions it falls back to the value order
// (fail closed: an unknown rule may deny).
func lowers(owns []policy.Action, v, w Value) bool {
	v, w = v.norm(), w.norm()
	if !v.Looser(w) {
		return false
	}
	if len(owns) == 0 {
		return true
	}
	for _, own := range owns {
		if w.Apply(own).StricterThan(v.Apply(own)) {
			return true
		}
	}
	return false
}

// lowersCat reports whether v lowers a built-in rule of category c below w.
func lowersCat(c Category, v, w Value) bool {
	loadCorpus()
	return lowers(corpusByCat[c], v, w)
}

// lowersID reports whether v lowers rule id below w (unknown ids: the value
// order).
func lowersID(id string, v, w Value) bool {
	loadCorpus()
	return lowers(corpusByID[id], v, w)
}
