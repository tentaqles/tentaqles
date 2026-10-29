package manifest

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

// ValidateMode checks the guard-mode keys.
//
// STUB (interface commit): accepts everything. WS1 adds the checks of spec E
// §7 and §11.1 (off is an error, base and categories only with custom, no
// locked category or id, project dirs inside the root, mixed
// guard.unattended + guard.mode is an error) and calls it from Load.
func (g Guard) ValidateMode() error { return nil }
