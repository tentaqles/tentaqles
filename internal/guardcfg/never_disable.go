package guardcfg

import "strings"

// neverDisable are rule ids no layer can switch off or soften: guard.disable
// and guard.actions skip them, unattended mode never skips their confirm
// (AlwaysAsk), and no pack guard may supersede them. tq/hub-store-write
// protects the hub store, the pack ledgers and claims and the saved plans
// under $TQ_HOME; tq/pack-config-change protects the pack config layers;
// tq/human-answer keeps answers and applies with the human, and
// tq/secret-store-shell keeps the pack ledger signing key (the root of every
// pack approval) out of the shell.
var neverDisable = map[string]bool{
	"tq/hub-store-write":    true,
	"tq/pack-config-change": true,
	"tq/human-answer":       true,
	"tq/secret-store-shell": true,
}

// NeverDisable reports whether id ignores guard.disable and guard.actions.
func NeverDisable(id string) bool { return neverDisable[id] }

// dropNeverDisable removes the never-disable ids from the disable origins
// and the action origins.
func dropNeverDisable(origin map[string]string, actions map[string]string) {
	for id := range origin {
		if neverDisable[id] {
			delete(origin, id)
		}
	}
	for id := range actions {
		if neverDisable[id] {
			delete(actions, id)
		}
	}
}

// Unsupersedable reports whether a pack guard may never drop id through
// Supersedes: the never-disable ids and the rules that guard secrets, the
// guard itself, the hub and the Claude Code settings.
func Unsupersedable(id string) bool {
	if neverDisable[id] {
		return true
	}
	for _, p := range []string{"tq/secret-", "tq/env-", "tq/guard-", "tq/settings-", "tq/commit-secret", "tq/hub-"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}
