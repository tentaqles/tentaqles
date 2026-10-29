package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// GuardProject is one guard.projects.<dir> entry (spec E §6.1 layer 4). The
// key is a folder relative to the workspace root, cleaned like
// guard.unattended.projects (CleanProject).
type GuardProject struct {
	Mode          string            `yaml:"mode,omitempty"`
	Base          string            `yaml:"base,omitempty"`
	Categories    map[string]string `yaml:"categories,omitempty"`
	RulesOverride map[string]string `yaml:"rules_override,omitempty"`
}

// GuardCodex is guard.codex (spec E §7).
type GuardCodex struct {
	// Sandbox is read-only|workspace-write|danger-full-access (was
	// guard.unattended.codex_sandbox).
	Sandbox string `yaml:"sandbox,omitempty"`
}

// The guard-mode names, as guardcfg defines them (manifest cannot import
// guardcfg; a guardcfg test keeps the lists in step).
var (
	// GuardStoredModes are the values guard.mode may take ("off" is never stored).
	GuardStoredModes = []string{"default", "relaxed", "strict", "custom"}
	// GuardBases are the presets a custom table may start from.
	GuardBases = []string{"default", "relaxed", "strict"}
	// GuardValues are a category's or a rule override's values.
	GuardValues = []string{"skip", "ask", "deny", "rule"}
	// GuardCategories are the thirteen categories.
	GuardCategories = []string{"identity", "secrets", "self-protection", "privacy",
		"git-history", "prod-changes", "cloud-destructive", "db-destructive",
		"system", "external-writes", "ci-supply-chain", "hygiene", "packs"}
	// GuardLockedCategories are enforced in every mode and never toggled.
	GuardLockedCategories = []string{"identity", "secrets", "self-protection", "privacy"}
)

// LockedRuleID reports whether id is a built-in rule in a locked category.
// guardcfg sets it in its init (it owns the category table); until then no
// id is locked here, and guardcfg still never changes a locked rule.
var LockedRuleID = func(id string) bool { return false }

// HasMode reports whether the guard block sets any guard-mode key.
func (g Guard) HasMode() bool {
	return g.Mode != "" || g.Base != "" || len(g.Categories) > 0 || len(g.RulesOverride) > 0 ||
		len(g.Projects) > 0 || g.HeadlessMode != "" || g.Codex != nil
}

// CodexSandbox returns guard.codex.sandbox, else the legacy
// guard.unattended.codex_sandbox, else workspace-write.
func (g Guard) CodexSandbox() string {
	if g.Codex != nil && g.Codex.Sandbox != "" {
		return g.Codex.Sandbox
	}
	return g.Unattended.Sandbox()
}

// ValidateMode checks the guard-mode keys (spec E §7, §11.1): off is never
// stored, base and categories only with mode custom, no locked category or
// rule id, project dirs inside the root, and guard.unattended mixed with the
// new keys is an error. Load calls it.
func (g Guard) ValidateMode() error {
	if g.Unattended != nil && (g.Mode != "" || g.Base != "" || len(g.Categories) > 0 || len(g.RulesOverride) > 0 || len(g.Projects) > 0) {
		return fmt.Errorf("guard.unattended and guard.mode cannot be mixed: move the unattended block to guard.mode (tq mode set)")
	}
	if err := validateModeKeys("guard", g.Mode, g.Base, g.Categories, g.RulesOverride); err != nil {
		return err
	}
	dirs := make([]string, 0, len(g.Projects))
	for d := range g.Projects {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if err := validateProjectDir(d); err != nil {
			return fmt.Errorf("guard.projects: %w", err)
		}
		p := g.Projects[d]
		if err := validateModeKeys("guard.projects."+d, p.Mode, p.Base, p.Categories, p.RulesOverride); err != nil {
			return err
		}
	}
	if g.HeadlessMode != "" && !contains(GuardStoredModes, g.HeadlessMode) {
		return fmt.Errorf("guard.headless_mode must be one of %s, got %q", strings.Join(GuardStoredModes, "|"), g.HeadlessMode)
	}
	if g.Codex != nil && g.Codex.Sandbox != "" && !contains(CodexSandboxes, g.Codex.Sandbox) {
		return fmt.Errorf("guard.codex.sandbox must be one of %s, got %q", strings.Join(CodexSandboxes, "|"), g.Codex.Sandbox)
	}
	return nil
}

// ValidateModeKeys checks one layer's mode, base, categories and
// rules_override (also used for the global guard.yaml). where names the
// layer in errors.
func ValidateModeKeys(where, mode, base string, cats, overrides map[string]string) error {
	return validateModeKeys(where, mode, base, cats, overrides)
}

func validateModeKeys(where, mode, base string, cats, overrides map[string]string) error {
	switch {
	case mode == "off":
		return fmt.Errorf("%s.mode: off is never stored; use claude-off, tq run --off or tq off", where)
	case mode != "" && !contains(GuardStoredModes, mode):
		return fmt.Errorf("%s.mode must be one of %s, got %q", where, strings.Join(GuardStoredModes, "|"), mode)
	case base != "" && mode != "custom":
		return fmt.Errorf("%s.base is only valid with mode: custom", where)
	case base != "" && !contains(GuardBases, base):
		return fmt.Errorf("%s.base must be one of %s, got %q", where, strings.Join(GuardBases, "|"), base)
	case len(cats) > 0 && mode != "custom":
		return fmt.Errorf("%s.categories is only valid with mode: custom", where)
	}
	for _, c := range sortedKeys(cats) {
		v := cats[c]
		switch {
		case !contains(GuardCategories, c):
			return fmt.Errorf("%s.categories: unknown category %q", where, c)
		case contains(GuardLockedCategories, c):
			return fmt.Errorf("%s.categories: %s is locked (always enforced) and cannot be set", where, c)
		case !contains(GuardValues, v):
			return fmt.Errorf("%s.categories[%s] must be one of %s, got %q", where, c, strings.Join(GuardValues, "|"), v)
		}
	}
	for _, id := range sortedKeys(overrides) {
		v := overrides[id]
		switch {
		case strings.TrimSpace(id) == "" || strings.ContainsAny(id, " \t\r\n"):
			return fmt.Errorf("%s.rules_override: %q is not a rule id", where, id)
		case LockedRuleID(id):
			return fmt.Errorf("%s.rules_override: %s is in a locked group (always enforced) and cannot be overridden", where, id)
		case !contains(GuardValues, v):
			return fmt.Errorf("%s.rules_override[%s] must be one of %s, got %q", where, id, strings.Join(GuardValues, "|"), v)
		}
	}
	return nil
}

func validateProjectDir(p string) error {
	raw := strings.TrimSpace(p)
	c := CleanProject(p)
	switch {
	case raw == "":
		return fmt.Errorf("empty entry")
	case isAbsLike(raw):
		return fmt.Errorf("%q must be relative to the workspace root", p)
	case c == ".":
		return fmt.Errorf("%q is the workspace root; set guard.mode instead", p)
	case c == ".." || strings.HasPrefix(c, "../"):
		return fmt.Errorf("%q leaves the workspace root", p)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ModeView returns g with a legacy guard.unattended block read as guard
// modes (spec E §11.1), in memory only:
//   - workspace: true → mode: relaxed;
//   - projects: [p…] → projects.<p>.mode: relaxed;
//   - keep_asking: [ids] → rules_override {id: rule} (absent: the old
//     defaults tq/iac-apply and tq/remote-migration); ids in a locked group
//     are dropped (they are always enforced anyway);
//   - codex_sandbox → codex.sandbox.
//
// A block that turns nothing on adds nothing. A guard without the legacy
// block is returned unchanged.
func (g Guard) ModeView() Guard {
	u := g.Unattended
	if !u.Configured() {
		return g
	}
	out := g
	out.Unattended = nil
	if u.Workspace && out.Mode == "" {
		out.Mode = "relaxed"
	}
	if len(u.Projects) > 0 {
		ps := make(map[string]GuardProject, len(g.Projects)+len(u.Projects))
		for k, v := range g.Projects {
			ps[k] = v
		}
		for _, p := range u.Projects {
			if validateProjectDir(p) != nil {
				continue // Validate rejects these; never widen coverage without it
			}
			cp := CleanProject(p)
			if _, ok := ps[cp]; !ok {
				ps[cp] = GuardProject{Mode: "relaxed"}
			}
		}
		out.Projects = ps
	}
	ov := make(map[string]string, len(g.RulesOverride))
	for k, v := range g.RulesOverride {
		ov[k] = v
	}
	for _, id := range u.KeepAskingIDs() {
		id = strings.TrimSpace(id)
		if id == "" || LockedRuleID(id) {
			continue
		}
		if _, ok := ov[id]; !ok {
			ov[id] = "rule"
		}
	}
	out.RulesOverride = ov
	if u.CodexSandbox != "" && (out.Codex == nil || out.Codex.Sandbox == "") {
		out.Codex = &GuardCodex{Sandbox: u.CodexSandbox}
	}
	return out
}
