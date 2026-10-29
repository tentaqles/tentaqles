package guardcfg

import (
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// UnattendedGroup says what unattended mode does with a built-in rule.
type UnattendedGroup string

const (
	// GroupIdentity rules keep each client's identity apart: the wrong
	// account, subscription or MCP server.
	GroupIdentity UnattendedGroup = "identity"
	// GroupSecret rules keep secrets out of the transcript, commits and
	// third-party systems.
	GroupSecret UnattendedGroup = "secret"
	// GroupSelfProtection rules stop the agent from changing tq's own
	// posture: the guard, its trust and the session's settings and hooks.
	GroupSelfProtection UnattendedGroup = "self-protection"
	// GroupSkippable rules are skipped in unattended sessions, asks and
	// denies alike.
	GroupSkippable UnattendedGroup = "skippable"
)

// GuardErrorID names the fail-safe ask of a guard panic.
const GuardErrorID = "tq/guard-error"

// UnattendedGroups classifies every built-in rule (internal/policy regex
// rules, HookRules, Jev judgment rules and the guard's fail-safe ask) for
// unattended mode. It is the one place that decides which rules unattended
// sessions still enforce; a test fails when a built-in rule is missing here.
// The identity guard (internal/guard: blocked_commands, wrong-cloud,
// git-email-drift, gh-user, ...) runs before these rules and is never skipped.
var UnattendedGroups = map[string]UnattendedGroup{
	// identity
	"tq/cross-client-mcp": GroupIdentity,
	// secrets
	"tq/secret-store-read":       GroupSecret,
	"tq/secret-store-shell":      GroupSecret,
	"tq/env-file-read":           GroupSecret,
	"tq/env-file-shell":          GroupSecret,
	"tq/env-dump":                GroupSecret,
	"tq/commit-secret":           GroupSecret,
	"jev/service-role-client":    GroupSecret,
	"jev/n8n-inline-credentials": GroupSecret,
	// self-protection
	"tq/guard-change":         GroupSelfProtection,
	"tq/guard-file-write":     GroupSelfProtection,
	"tq/settings-widen":       GroupSelfProtection,
	"tq/settings-shell-write": GroupSelfProtection,
	"tq/trust-store-write":    GroupSelfProtection,
	GuardErrorID:              GroupSelfProtection,
	// everything else
	"tq/force-push-main":       GroupSkippable,
	"tq/force-push":            GroupSkippable,
	"tq/gh-admin-merge":        GroupSkippable,
	"tq/gh-repo-delete":        GroupSkippable,
	"tq/cloud-delete":          GroupSkippable,
	"tq/iac-destroy":           GroupSkippable,
	"tq/registry-write":        GroupSkippable,
	"tq/tracker-cli-write":     GroupSkippable,
	"tq/sql-destructive-shell": GroupSkippable,
	"tq/migration-edit":        GroupSkippable,
	"tq/mcp-sql-write":         GroupSkippable,
	"tq/mcp-apply-migration":   GroupSkippable,
	"tq/mcp-n8n-write":         GroupSkippable,
	"tq/mcp-github-write":      GroupSkippable,
	"tq/mcp-hosting-exec":      GroupSkippable,
	"tq/iac-apply":             GroupSkippable,
	"tq/remote-migration":      GroupSkippable,
	"tq/gha-unpinned":          GroupSkippable,
	"tq/gha-branch-ref":        GroupSkippable,
	"tq/gha-pr-target":         GroupSkippable,
	"tq/commit-pii":            GroupSkippable,
	"tq/push-pii":              GroupSkippable,
	"tq/text-pii":              GroupSkippable,
	"tq/pii-fixture-dump":      GroupSkippable,
	"tq/commit-message":        GroupSkippable,
	"jev/destructive-sql":      GroupSkippable,
	"jev/rls-weakened":         GroupSkippable,
}

// AlwaysEnforced returns the built-in rule ids unattended mode never skips,
// by group, each list sorted.
func AlwaysEnforced() map[UnattendedGroup][]string {
	out := map[UnattendedGroup][]string{}
	for id, g := range UnattendedGroups {
		if g != GroupSkippable {
			out[g] = append(out[g], id)
		}
	}
	for _, ids := range out {
		sort.Strings(ids)
	}
	return out
}

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

// Enforced reports whether an unattended session still applies r (ask or
// deny) in the workspace of m: a built-in rule outside GroupSkippable, a rule
// in guard.unattended.keep_asking, or a non-built-in rule with
// unattended: enforce (any other non-empty value fails closed). Every other
// rule, a built-in one not in UnattendedGroups included, is skipped.
func Enforced(m *manifest.Manifest, r *policy.Rule) bool {
	if r == nil || m == nil {
		return true
	}
	if g, ok := UnattendedGroups[r.ID]; ok && g != GroupSkippable {
		return true
	}
	if r.Unattended != "" {
		return true
	}
	for _, id := range m.Guard.Unattended.KeepAskingIDs() {
		if id == r.ID {
			return true
		}
	}
	return false
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
