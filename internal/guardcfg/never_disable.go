package guardcfg

import "strings"

// NeverDisable reports whether id ignores guard.disable and guard.actions
// that would soften it. Since spec E it is the locked categories (identity,
// secrets, self-protection, privacy): the pack framework's core rules
// tq/hub-store-write, tq/pack-config-change and tq/human-answer are
// self-protection, and tq/secret-store-shell (the pack ledger signing key) is
// secrets. Merge applies it (spec E §6.1 step 9).
func NeverDisable(id string) bool { return LockedID(id) }

// Unsupersedable reports whether a pack guard may never drop id through
// Supersedes: the locked ids and the rules that guard secrets, the guard
// itself, the hub and the Claude Code settings.
func Unsupersedable(id string) bool {
	if LockedID(id) {
		return true
	}
	for _, p := range []string{"tq/secret-", "tq/env-", "tq/guard-", "tq/settings-", "tq/commit-secret", "tq/hub-"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}
