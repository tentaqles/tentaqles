package guardcfg

import (
	"github.com/tentaqles/tentaqles/internal/manifest"
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

// LooserThanDefault reports whether r may skip or lower some rule (the
// statusline's yellow). STUB (interface commit): true for relaxed and custom;
// WS1 compares the table and overrides against default.
func (r Resolved) LooserThanDefault() bool {
	return r.Mode == ModeRelaxed || r.Mode == ModeCustom
}

// Effective returns the action for rule under r (Allow = skipped): the
// category's value, then Overrides[rule.ID], then the ceiling clamp, then the
// locked groups at their own action or stricter.
//
// STUB (interface commit): returns rule's own action, which is the default
// mode. WS1 implements the full order.
func (r Resolved) Effective(rule *policy.Rule) policy.Action {
	if rule == nil {
		return policy.Allow
	}
	return rule.Action
}

// Resolve computes the mode for a call (spec E §6.1): built-in default, the
// global file g, the workspace manifest m (named ws, rooted at root), the
// deepest guard.projects entry covering cwd (and target, for a file tool;
// "" otherwise), the tighten-only repo file, the per-launch override launch
// (TQ_MODE, "" when unset; "off" is handled before this), then the ceiling
// grant (nil: default ceiling). m may be nil (neutral folder).
//
// STUB (interface commit): returns DefaultResolved(). WS1 implements it.
func Resolve(m *manifest.Manifest, ws, root, cwd, target string, g File, repo RepoFile, launch string, grant *Grant) Resolved {
	return DefaultResolved()
}
