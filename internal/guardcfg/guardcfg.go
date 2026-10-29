// Package guardcfg resolves which guard rules are on, and with which action,
// for a workspace. Three trusted layers decide, most specific last:
//
//  1. tq's built-in rules (internal/policy, internal/decide);
//  2. the user's global file, ~/.tentaqles/guard.yaml (`tq guard ... --all`);
//  3. the workspace manifest's guard block (`tq guard ... --ws NAME`).
//
// A project's .claude/tq-rules.yaml is not a layer here: repo content is
// untrusted, so it may only add rules and can never turn one off.
package guardcfg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

// FileName is the global guard file under TQ_HOME.
const FileName = "guard.yaml"

// Path is ~/.tentaqles/guard.yaml (or $TQ_HOME/guard.yaml).
func Path() string { return filepath.Join(paths.Home(), FileName) }

// File is the global guard file.
type File struct {
	Disable []string                 `yaml:"disable,omitempty"`
	Actions map[string]policy.Action `yaml:"actions,omitempty"`
	Rules   []policy.Rule            `yaml:"rules,omitempty"`

	// Guard mode, layer 2 (spec E §6.1, §7). Validation is WS1's: "off" is
	// a load error, categories only with mode custom, never a locked one.
	Mode          string             `yaml:"mode,omitempty"`
	Base          string             `yaml:"base,omitempty"`
	Categories    map[Category]Value `yaml:"categories,omitempty"`
	RulesOverride map[string]Value   `yaml:"rules_override,omitempty"`
}

// HookRules are checks the hook runs in code rather than as regex rules;
// they can be switched off and their action changed like any other rule.
var HookRules = map[string]string{
	"tq/commit-secret":    "a git commit that would record a secret",
	"tq/cross-client-mcp": "an MCP server that belongs to another client",
	"tq/commit-pii":       "a git commit that would record personal or financial data",
	"tq/push-pii":         "a git push whose commits record personal or financial data",
	"tq/text-pii":         "a commit, tag or GitHub PR/issue/release text that carries personal or financial data",
	"tq/pii-fixture-dump": "copying rows from classified data into tests/, fixtures/ or seeds/",
	"tq/commit-message":   "a git commit whose message is not Conventional Commits",
	OutsidePathWriteID:    "a write or recursive delete of an absolute path outside the workspace, the session folder, your repos and the temp folders",
}

// HookRuleAction is a hook rule's default action: ask for the commit
// message and outside-path checks, deny for the rest.
func HookRuleAction(id string) policy.Action {
	switch id {
	case "tq/commit-message", OutsidePathWriteID:
		return policy.Ask
	}
	return policy.Deny
}

// validModeKeys checks the global file's guard-mode keys like a manifest's.
func validModeKeys(f File) error {
	cats := map[string]string{}
	for c, v := range f.Categories {
		cats[string(c)] = string(v)
	}
	ov := map[string]string{}
	for id, v := range f.RulesOverride {
		ov[id] = string(v)
	}
	if err := manifest.ValidateModeKeys("guard.yaml", f.Mode, f.Base, cats, ov); err != nil {
		return err
	}
	for _, r := range f.Rules {
		if r.Category != "" {
			if _, ok := ParseCategory(r.Category); !ok {
				return fmt.Errorf("rules[%s].category: unknown category %q", r.ID, r.Category)
			}
		}
	}
	return nil
}

// UnknownOverrides returns the rules_override ids (global, workspace,
// projects, and the legacy keep_asking) that name no rule tq knows, sorted.
func UnknownOverrides(m *manifest.Manifest, g File) []string {
	known := Known(m, g)
	ids := map[string]bool{}
	for id := range g.RulesOverride {
		ids[id] = true
	}
	if m != nil {
		gv := m.Guard.ModeView()
		for id := range gv.RulesOverride {
			ids[id] = true
		}
		for _, p := range gv.Projects {
			for id := range p.RulesOverride {
				ids[id] = true
			}
		}
	}
	var out []string
	for id := range ids {
		if _, ok := Lookup(id, known); !ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// UnknownKeepAsking returns keep_asking ids that name no rule tq knows.
// Deprecated: use UnknownOverrides.
func UnknownKeepAsking(m *manifest.Manifest, g File) []string {
	if m == nil || m.Guard.Unattended == nil {
		return nil
	}
	known := Known(m, g)
	var out []string
	for _, id := range m.Guard.Unattended.KeepAskingIDs() {
		if _, ok := Lookup(id, known); !ok {
			out = append(out, id)
		}
	}
	return out
}

// Load reads the global file. A missing file is an empty File. A file that
// cannot be parsed or holds an invalid action is an error: callers must then
// apply nothing from it, so every built-in rule stays on (fail closed).
func Load() (File, error) {
	var f File
	raw, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return File{}, err
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("%s: %w", Path(), err)
	}
	if err := validActions(f.Actions); err != nil {
		return File{}, fmt.Errorf("%s: %w", Path(), err)
	}
	if err := validModeKeys(f); err != nil {
		return File{}, fmt.Errorf("%s: %w", Path(), err)
	}
	for i := range f.Rules {
		f.Rules[i].Source = Path()
	}
	return f, nil
}

// Save writes the global file atomically.
func Save(f File) error {
	if err := validActions(f.Actions); err != nil {
		return err
	}
	f.Disable = uniqSorted(f.Disable)
	raw, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	header := "# tq guard settings for every workspace. Edit with `tq guard ... --all`;\n# a workspace manifest's guard block overrides this file.\n"
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header), raw...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

func validActions(m map[string]policy.Action) error {
	for id, a := range m {
		if a != policy.Ask && a != policy.Deny {
			return fmt.Errorf("actions[%s] must be ask or deny, got %q", id, a)
		}
	}
	return nil
}

// Effective is the merged guard configuration for one workspace.
type Effective struct {
	Disable []string                 // ids switched off
	Actions map[string]policy.Action // action overrides
	Global  []policy.Rule            // rules added by the global file
	// Origin says where each override came from: "global" or the
	// workspace name.
	DisableOrigin map[string]string
	ActionOrigin  map[string]string
	GlobalErr     error // the global file could not be loaded (ignored)
	// LockedIgnored lists, sorted, the disable and looser actions entries
	// that named a locked rule and were ignored (doctor guard-locked-override),
	// as "disable:<id>@<origin>" or "actions:<id>@<origin>".
	LockedIgnored []string
}

// Off reports whether id is switched off.
func (e Effective) Off(id string) bool {
	_, ok := e.DisableOrigin[id]
	return ok
}

// ActionFor returns the effective action for id given its default.
func (e Effective) ActionFor(id string, def policy.Action) policy.Action {
	if a, ok := e.Actions[id]; ok {
		return a
	}
	return def
}

// ResolveActions merges the global file (loaded here) with m's guard block
// (named Resolve before spec E, which uses Resolve for the guard mode). m may
// be nil (no trusted workspace): then only the global file applies.
func ResolveActions(m *manifest.Manifest, wsName string) Effective {
	g, err := Load()
	return Merge(g, err, m, wsName)
}

// Merge is ResolveActions with the global file given; split out for tests.
func Merge(g File, gerr error, m *manifest.Manifest, wsName string) Effective {
	e := Effective{Actions: map[string]policy.Action{}, DisableOrigin: map[string]string{}, ActionOrigin: map[string]string{}, GlobalErr: gerr}
	if gerr == nil {
		for _, id := range g.Disable {
			e.DisableOrigin[strings.TrimSpace(id)] = "global"
		}
		for id, a := range g.Actions {
			e.Actions[id] = a
			e.ActionOrigin[id] = "global"
		}
		e.Global = g.Rules
	}
	if m != nil {
		for _, id := range m.Guard.Disable {
			e.DisableOrigin[strings.TrimSpace(id)] = wsName
		}
		// Jev rules are switched off through the decision block too.
		for _, id := range m.Decision.Disable {
			if _, ok := e.DisableOrigin[id]; !ok {
				e.DisableOrigin[strings.TrimSpace(id)] = wsName
			}
		}
		// Enable is applied last: it wins over the global file and
		// decision.disable for this workspace.
		for _, id := range m.Guard.Enable {
			delete(e.DisableOrigin, strings.TrimSpace(id))
		}
		for id, a := range m.Guard.Actions {
			e.Actions[id] = a
			e.ActionOrigin[id] = wsName
		}
	}
	// Locked rules (spec E §6.1 step 9): guard.disable is ignored, and
	// guard.actions may only make them stricter.
	for id, origin := range e.DisableOrigin {
		if LockedID(id) {
			delete(e.DisableOrigin, id)
			e.LockedIgnored = append(e.LockedIgnored, "disable:"+id+"@"+origin)
		}
	}
	for id, a := range e.Actions {
		if !LockedID(id) {
			continue
		}
		if def, ok := BuiltinAction(id); ok && def.StricterThan(a) {
			e.LockedIgnored = append(e.LockedIgnored, "actions:"+id+"@"+e.ActionOrigin[id])
			delete(e.Actions, id)
			delete(e.ActionOrigin, id)
		}
	}
	sort.Strings(e.LockedIgnored)
	for id := range e.DisableOrigin {
		e.Disable = append(e.Disable, id)
	}
	sort.Strings(e.Disable)
	return e
}

// RuleInfo describes one known rule for listing.
type RuleInfo struct {
	ID      string
	Action  policy.Action // default action
	Kind    string        // builtin | hook | jev | global | manifest
	Summary string
}

// Known lists every rule id a user may toggle for workspace m (nil: none).
func Known(m *manifest.Manifest, g File) []RuleInfo {
	var out []RuleInfo
	seen := map[string]bool{}
	for _, r := range policy.Builtin() {
		if seen[r.ID] {
			continue // one id may span several regex rules
		}
		seen[r.ID] = true
		out = append(out, RuleInfo{ID: r.ID, Action: r.Action, Kind: "builtin", Summary: r.Reason})
	}
	for id, s := range HookRules {
		out = append(out, RuleInfo{ID: id, Action: HookRuleAction(id), Kind: "hook", Summary: s})
	}
	for _, r := range decide.BuiltinJudgeRules() {
		a := policy.Ask
		if r.Max == decide.Deny {
			a = policy.Deny
		}
		out = append(out, RuleInfo{ID: r.ID, Action: a, Kind: "jev", Summary: r.Question})
	}
	for _, r := range g.Rules {
		out = append(out, RuleInfo{ID: r.ID, Action: r.Action, Kind: "global", Summary: r.Reason})
	}
	if m != nil {
		for _, r := range m.Guard.Rules {
			out = append(out, RuleInfo{ID: r.ID, Action: r.Action, Kind: "manifest", Summary: r.Reason})
		}
		for _, r := range m.Decision.Rules {
			out = append(out, RuleInfo{ID: r.ID, Action: policy.Ask, Kind: "jev", Summary: r.Question})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Lookup finds id among the known rules.
func Lookup(id string, known []RuleInfo) (RuleInfo, bool) {
	for _, r := range known {
		if r.ID == id {
			return r, true
		}
	}
	return RuleInfo{}, false
}

func uniqSorted(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
