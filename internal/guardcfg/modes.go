package guardcfg

import (
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// Value is what a mode does with a category or a rule id (spec E §4, §6.2).
type Value string

const (
	ValueSkip Value = "skip" // no ask, no deny (Allow)
	ValueAsk  Value = "ask"
	ValueDeny Value = "deny"
	ValueRule Value = "rule" // the rule's own action (after guard.actions)
)

// Values returns the four values in display order.
func Values() []Value { return []Value{ValueSkip, ValueAsk, ValueDeny, ValueRule} }

// ParseValue returns the value named s, and false for an unknown name.
func ParseValue(s string) (Value, bool) {
	switch v := Value(s); v {
	case ValueSkip, ValueAsk, ValueDeny, ValueRule:
		return v, true
	}
	return "", false
}

// Apply returns the action v gives a rule whose own action is own: skip is
// Allow, ask and deny are themselves, rule (or "") is own.
func (v Value) Apply(own policy.Action) policy.Action {
	switch v {
	case ValueSkip:
		return policy.Allow
	case ValueAsk:
		return policy.Ask
	case ValueDeny:
		return policy.Deny
	}
	return own
}

// Table gives each category a value. A category missing from a table is
// ValueRule.
type Table map[Category]Value

// Get returns c's value, ValueRule when unset.
func (t Table) Get(c Category) Value {
	if v, ok := t[c]; ok && v != "" {
		return v
	}
	return ValueRule
}

// Clone returns a copy of t (nil stays nil).
func (t Table) Clone() Table {
	if t == nil {
		return nil
	}
	out := make(Table, len(t))
	for k, v := range t {
		out[k] = v
	}
	return out
}

// Grant is a stored mode grant (the ceiling). See trust.Grant.
type Grant = trust.Grant

// RepoFile is the mode part of a repo's .claude/tq-rules.yaml (layer 5,
// untrusted, tighten-only).
type RepoFile struct {
	Mode          string             `yaml:"mode,omitempty"`
	Categories    map[Category]Value `yaml:"categories,omitempty"`
	RulesOverride map[string]Value   `yaml:"rules_override,omitempty"`
}

// Resolved is the guard mode in effect for one call (spec E §6.2).
type Resolved struct {
	Mode      string           // default|relaxed|strict|custom, after the ceiling
	Base      string           // the preset a custom table starts from
	Table     Table            // after layers 1-6
	Overrides map[string]Value // rules_override, merged
	Ceiling   *Grant           // nil: the default ceiling
	// Sources names the layer that set each category or id:
	// "builtin"|"global"|<workspace>|"project:<dir>"|"repo"|"launch".
	Sources map[string]string
	Clamped []string // categories/ids loosened past the ceiling and clamped
	Ignored []string // repo-file values that tried to loosen

	// Requested is the mode the layers asked for before the ceiling clamp
	// ("relaxed" for a workspace that asks for relaxed with no grant, while
	// Mode is then "default").
	Requested string
	// ModeSource names the layer that set Requested (as in Sources).
	ModeSource string
}

// DefaultResolved is the built-in default mode: every category at the rule's
// own action, no overrides, the default ceiling.
func DefaultResolved() Resolved {
	t, _ := Preset(ModeDefault)
	return Resolved{
		Mode: ModeDefault, Base: ModeDefault, Requested: ModeDefault, ModeSource: "builtin",
		Table: t, Overrides: map[string]Value{}, Sources: map[string]string{},
	}
}

// IsDefault reports whether r changes nothing: mode default, every category
// at rule, no overrides. The hook takes its fast path then.
func (r Resolved) IsDefault() bool {
	if r.Mode != "" && r.Mode != ModeDefault {
		return false
	}
	if len(r.Overrides) > 0 {
		return false
	}
	for _, v := range r.Table {
		if v != ValueRule && v != "" {
			return false
		}
	}
	return true
}

// rank orders the values from loosest to strictest: skip < ask < rule <
// deny. For any rule action (ask or deny), Apply is monotone in it, so the
// order is exact: a higher value never gives a looser action.
func (v Value) rank() int {
	switch v {
	case ValueSkip:
		return 0
	case ValueAsk:
		return 1
	case ValueDeny:
		return 3
	}
	return 2 // rule, "" and anything unknown (fail closed)
}

// norm maps "" and unknown values to ValueRule.
func (v Value) norm() Value {
	if _, ok := ParseValue(string(v)); ok {
		return v
	}
	return ValueRule
}

// Looser reports whether v is looser than w (lower in the order).
func (v Value) Looser(w Value) bool { return v.rank() < w.rank() }

// stricterValue returns the stricter of v and w.
func stricterValue(v, w Value) Value {
	if v.norm().rank() >= w.norm().rank() {
		return v.norm()
	}
	return w.norm()
}

// LooserThanDefault reports whether r may skip or lower some rule (the
// statusline's yellow): a toggleable category or a rule override that lowers
// a built-in rule's own action. Strict is never looser: its external-writes
// ask lowers no built-in rule (they all ask).
func (r Resolved) LooserThanDefault() bool {
	for _, c := range allCategories {
		if Locked(c) {
			continue
		}
		if lowersCat(c, r.Table.Get(c), ValueRule) {
			return true
		}
	}
	for id, v := range r.Overrides {
		if Locked(Categories[id]) {
			continue
		}
		if lowersID(id, v, ValueRule) {
			return true
		}
	}
	return false
}

// Effective returns the action for rule under r (Allow = skipped), in spec E
// §6.2's order: the category's value (rule: the rule's own action, which
// already holds guard.actions), then Overrides[rule.ID], then the ceiling
// clamp, then the locked groups at their own action. An unclassified rule
// (no category) keeps its own action, and an override can only tighten it.
func (r Resolved) Effective(rule *policy.Rule) policy.Action {
	if rule == nil {
		return policy.Allow
	}
	own := rule.Action
	if own == policy.Allow {
		return own
	}
	cat := CategoryOf(rule)
	if Locked(cat) {
		return own
	}
	v := ValueRule
	if cat != "" {
		v = r.Table.Get(cat).norm()
	}
	if ov, ok := r.Overrides[rule.ID]; ok {
		v = ov.norm()
		if r.Requested == ModeStrict && cat != "" {
			// strict pins the categories it sets (prod-changes,
			// cloud-destructive, git-history, external-writes) against a
			// looser override (§4.4); the others keep the override.
			if pin, _ := Preset(ModeStrict); pin.Get(cat) != ValueRule {
				v = stricterValue(v, pin.Get(cat))
			}
		}
	}
	v = stricterValue(v, r.ceilingValue(rule.ID, cat))
	return v.Apply(own)
}

// ceilingValue is the loosest value the ceiling allows for rule id in
// category cat: the grant's override, else its table's value; ValueRule
// (the rule's own action) with no grant or no category.
func (r Resolved) ceilingValue(id string, cat Category) Value {
	if cat == "" || r.Ceiling == nil {
		return ValueRule
	}
	t, ov := grantTable(r.Ceiling)
	if v, ok := ov[id]; ok {
		return v
	}
	return t.Get(cat).norm()
}
