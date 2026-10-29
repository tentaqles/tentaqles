package manifest

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// DefaultKeepAsking are the confirms unattended mode still asks when the
// manifest does not list its own: irreversible, outward-facing changes.
var DefaultKeepAsking = []string{"tq/iac-apply", "tq/remote-migration"}

// CodexSandboxes are the sandbox_mode values tq writes for Codex.
var CodexSandboxes = []string{"read-only", "workspace-write", "danger-full-access"}

// isAbsLike reports whether a projects entry is an absolute path in any
// spelling (POSIX, a Windows drive or a UNC/volume name).
func isAbsLike(raw string) bool {
	c := CleanProject(raw)
	return filepath.IsAbs(raw) || strings.HasPrefix(c, "/") || filepath.VolumeName(raw) != "" || (len(raw) > 1 && raw[1] == ':')
}

// Unattended is the legacy guard.unattended block (deprecated by spec E: it
// is read as guard modes through Guard.ModeView). KeepAsking is a pointer so an
// absent list (defaults) differs from an explicit empty one (ask nothing).
type Unattended struct {
	Workspace    bool      `yaml:"workspace"`
	Projects     []string  `yaml:"projects,omitempty"`
	KeepAsking   *[]string `yaml:"keep_asking,omitempty"`
	CodexSandbox string    `yaml:"codex_sandbox,omitempty"`
}

// Configured reports whether the block turns anything on.
func (u *Unattended) Configured() bool {
	return u != nil && (u.Workspace || len(u.Projects) > 0)
}

// KeepAskingIDs returns the manifest's list, or the defaults when absent.
func (u *Unattended) KeepAskingIDs() []string {
	if u == nil || u.KeepAsking == nil {
		return append([]string{}, DefaultKeepAsking...)
	}
	return append([]string{}, (*u.KeepAsking)...)
}

// Sandbox returns codex_sandbox, defaulting to workspace-write.
func (u *Unattended) Sandbox() string {
	if u == nil || u.CodexSandbox == "" {
		return "workspace-write"
	}
	return u.CodexSandbox
}

// CleanProject returns the slash-separated, cleaned form of a projects entry.
func CleanProject(p string) string {
	return path.Clean(filepath.ToSlash(strings.TrimSpace(p)))
}

// Validate checks the block; a nil block is valid (off).
func (u *Unattended) Validate() error {
	if u == nil {
		return nil
	}
	for _, p := range u.Projects {
		if strings.TrimSpace(p) != "" && !isAbsLike(strings.TrimSpace(p)) && CleanProject(p) == "." {
			return fmt.Errorf("projects: %q is the workspace root; use workspace: true", p)
		}
		if err := validateProjectDir(p); err != nil {
			return fmt.Errorf("projects: %w", err)
		}
	}
	if u.KeepAsking != nil {
		for _, id := range *u.KeepAsking {
			if strings.TrimSpace(id) == "" || strings.ContainsAny(id, " \t\r\n") {
				return fmt.Errorf("keep_asking: %q is not a rule id", id)
			}
		}
	}
	if u.CodexSandbox != "" && !contains(CodexSandboxes, u.CodexSandbox) {
		return fmt.Errorf("codex_sandbox must be one of %s, got %q", strings.Join(CodexSandboxes, "|"), u.CodexSandbox)
	}
	return nil
}
