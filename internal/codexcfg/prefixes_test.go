package codexcfg

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Every id in the prefix table is a classified guard id, never a locked one
// (a locked ask is a hook deny in Codex, and a locked deny is compiled from
// the rule set, not from extra patterns), and every pattern is well formed.
func TestPrefixTableIDsAreKnown(t *testing.T) {
	for _, id := range PrefixIDs() {
		c, ok := guardcfg.Categories[id]
		if !ok {
			t.Errorf("%s: not in guardcfg.Categories", id)
			continue
		}
		if guardcfg.Locked(c) {
			t.Errorf("%s: locked category %s has no place in the prefix table", id, c)
		}
		for _, p := range Prefixes(id) {
			if len(p) < 2 {
				t.Errorf("%s: pattern %v is shorter than two words (too broad)", id, p)
			}
			for _, tok := range p {
				if len(tok) == 0 {
					t.Errorf("%s: empty position in %v", id, p)
				}
				for _, w := range tok {
					if w == "" || strings.ContainsAny(w, " \t\"\\") {
						t.Errorf("%s: bad word %q", id, w)
					}
				}
			}
		}
	}
}

// Each pattern must only match commands its rule matches: a prefix that is
// broader than the rule would let the hook allow a call on a prompt rule that
// tq never asked for (it would only prompt more, never less, but the prompt
// would name the wrong rule).
func TestPrefixesAgreeWithTheirRules(t *testing.T) {
	set, _ := policy.Build(policy.Layers{Builtin: policy.Builtin()})
	ids := map[string]bool{}
	for _, r := range policy.Builtin() {
		ids[r.ID] = true
	}
	for _, id := range PrefixIDs() {
		if !ids[id] {
			continue // a rule another workstream registers (e.g. tq/elevation)
		}
		for _, p := range Prefixes(id) {
			for _, cmd := range expand(p) {
				d := set.Evaluate(policy.ToolCall{Tool: "Bash", Command: cmd + " x"})
				found := false
				for _, m := range d.Matched {
					if m.ID == id {
						found = true
					}
				}
				if !found && d.Action != policy.Deny {
					t.Errorf("%s: %q does not match its own rule (got %v %v)", id, cmd, d.Action, d.Matched)
				}
			}
		}
	}
}

func TestPrefixMatch(t *testing.T) {
	p := Pattern{{"terraform", "tofu"}, {"apply"}}
	for _, tc := range []struct {
		words []string
		want  bool
	}{
		{[]string{"terraform", "apply"}, true},
		{[]string{"tofu", "apply", "-auto-approve"}, true},
		{[]string{"terraform", "plan"}, false},
		{[]string{"terraform"}, false},
		{[]string{"x", "terraform", "apply"}, false},
	} {
		if got := p.Match(tc.words); got != tc.want {
			t.Errorf("%v: got %v", tc.words, got)
		}
	}
}

// expand lists every concrete command a pattern stands for.
func expand(p Pattern) []string {
	out := []string{""}
	for _, tok := range p {
		var next []string
		for _, pre := range out {
			for _, w := range tok {
				next = append(next, strings.TrimSpace(pre+" "+w))
			}
		}
		out = next
	}
	return out
}
