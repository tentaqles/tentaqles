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
}

// HookRules are checks the hook runs in code rather than as regex rules;
// they can be switched off and their action changed like any other rule.
var HookRules = map[string]string{
	"tq/commit-secret":    "a git commit that would record a secret",
	"tq/cross-client-mcp": "an MCP server that belongs to another client",
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

// Resolve merges the global file (loaded here) with m's guard block. m may
// be nil (no trusted workspace): then only the global file applies.
func Resolve(m *manifest.Manifest, wsName string) Effective {
	g, err := Load()
	return Merge(g, err, m, wsName)
}

// Merge is Resolve with the global file given; split out for tests.
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
		out = append(out, RuleInfo{ID: id, Action: policy.Deny, Kind: "hook", Summary: s})
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
