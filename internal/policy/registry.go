package policy

import (
	"fmt"
	"sort"
	"sync"
)

// Built-in rule parts registered from other files of this package
// (builtin_system.go, builtin_agents.go, ...) in their init funcs, so each
// workstream owns its own file and Builtin stays one list.
var (
	registryMu sync.Mutex
	registered []Rule
)

// RegisterBuiltin adds rules to Builtin. Call it from an init func. It panics
// on an empty id or an id already built in or registered by an earlier call:
// ids are stable and unique per part. One call may pass several rules with
// the same id (one id may span several regex rules, e.g. a file-tool and a
// shell form).
func RegisterBuiltin(rules ...Rule) {
	registryMu.Lock()
	defer registryMu.Unlock()
	seen := map[string]bool{}
	for _, r := range coreBuiltin() {
		seen[r.ID] = true
	}
	for _, r := range registered {
		seen[r.ID] = true
	}
	for _, r := range rules {
		if r.ID == "" {
			panic("policy.RegisterBuiltin: rule without id")
		}
		if seen[r.ID] {
			panic(fmt.Sprintf("policy.RegisterBuiltin: duplicate rule id %s", r.ID))
		}
	}
	registered = append(registered, rules...)
}

// Builtin returns tq's default rules: the ones in builtin.go, then every
// registered part, in registration order. IDs are stable: manifests disable
// them by id (guard.disable) and tests pin them. The slice is a fresh copy.
func Builtin() []Rule {
	registryMu.Lock()
	defer registryMu.Unlock()
	return append(coreBuiltin(), registered...)
}

// Mapped is one matching rule whose action a mapper changed: To is the
// action applied (Allow = skipped). The audit records these.
type Mapped struct {
	Rule *Rule
	To   Action
}

// EvaluateMapped is Evaluate with each matching rule's action replaced by
// fn(rule) (nil fn: the rule's own action). Ranking happens after mapping, so
// a skipped or lowered stricter rule never hides a weaker one that still
// applies. It also returns every matching rule whose action fn changed, in
// rule order. Decision.Matched holds the rules at the winning (mapped) level.
func (s *Set) EvaluateMapped(call ToolCall, fn func(*Rule) Action) (Decision, []Mapped) {
	var d Decision
	var changed []Mapped
	for _, r := range s.Rules {
		if !r.Match(call) {
			continue
		}
		a := r.Action
		if fn != nil {
			a = fn(r)
		}
		if a != r.Action {
			changed = append(changed, Mapped{Rule: r, To: a})
		}
		if a == Allow {
			continue
		}
		switch {
		case a.rank() > d.Action.rank():
			d.Action = a
			d.Matched = []*Rule{r}
		case a.rank() == d.Action.rank():
			d.Matched = append(d.Matched, r)
		}
	}
	sort.SliceStable(d.Matched, func(i, j int) bool { return d.Matched[i].ID < d.Matched[j].ID })
	return d, changed
}

// MapDecision applies fn (nil: unchanged) to the rules of a one-source
// decision (commit scan, PII, commit message, cross-client MCP, Jev, a pack
// guard). The result keeps the rules at the strictest mapped level; when all
// are skipped it is Allow. It also returns the rules fn changed. A merged
// decision has already dropped its weaker rules, so use it on one source only.
func MapDecision(d Decision, fn func(*Rule) Action) (Decision, []Mapped) {
	if fn == nil || d.Action == Allow {
		return d, nil
	}
	var out Decision
	var changed []Mapped
	for _, r := range d.Matched {
		a := fn(r)
		if a != r.Action {
			changed = append(changed, Mapped{Rule: r, To: a})
		}
		switch {
		case a == Allow:
		case a.rank() > out.Action.rank():
			out.Action = a
			out.Matched = []*Rule{r}
		case a.rank() == out.Action.rank():
			out.Matched = append(out.Matched, r)
		}
	}
	return out, changed
}
