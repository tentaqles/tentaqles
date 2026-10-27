package guardcfg

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// AlwaysAsk are confirms unattended mode never skips: they protect the guard
// and the switch itself, so an agent cannot widen its own permissions.
var AlwaysAsk = []string{"tq/guard-change"}

var unattendedModes = map[string]bool{"bypassPermissions": true, "auto": true}

// Covers reports whether cwd is inside the part of the workspace at root that
// m's guard.unattended block turns on, and names the scope ("workspace" or
// the project entry).
func Covers(m *manifest.Manifest, root, cwd string) (bool, string) {
	if m == nil || !m.Guard.Unattended.Configured() || root == "" || cwd == "" {
		return false, ""
	}
	u := m.Guard.Unattended
	r, c := normDir(root), normDir(cwd)
	if !within(c, r) {
		return false, ""
	}
	if u.Workspace {
		return true, "workspace"
	}
	for _, p := range u.Projects {
		cp := manifest.CleanProject(p)
		if cp == "" || cp == "." || cp == ".." || strings.HasPrefix(cp, "../") || path.IsAbs(cp) || filepath.IsAbs(cp) {
			continue // defense in depth: Validate rejects these, but never widen coverage without it
		}
		if within(c, normDir(filepath.Join(root, filepath.FromSlash(cp)))) {
			return true, cp
		}
	}
	return false, ""
}

// Unattended reports whether a session in mode at cwd skips tq's confirms.
func Unattended(m *manifest.Manifest, root, cwd, mode string, bypassTrusted bool) (bool, string) {
	if !bypassTrusted || !unattendedModes[mode] {
		return false, ""
	}
	return Covers(m, root, cwd)
}

// Skippable reports whether a confirm that matched ids may be skipped: none
// of them is always asked or in the manifest's keep_asking list.
func Skippable(m *manifest.Manifest, ids []string) bool {
	if m == nil || len(ids) == 0 {
		return false
	}
	keep := map[string]bool{}
	for _, id := range AlwaysAsk {
		keep[id] = true
	}
	for _, id := range m.Guard.Unattended.KeepAskingIDs() {
		keep[id] = true
	}
	for _, id := range ids {
		if keep[id] {
			return false
		}
	}
	return true
}

// UnknownKeepAsking returns keep_asking ids that name no rule tq knows.
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

func normDir(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	if e, err := filepath.EvalSymlinks(p); err == nil {
		p = e
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return strings.TrimSuffix(p, "/")
}

func within(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }
