package guardcfg

import (
	"sort"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Category groups guard rules for the guard modes (spec E §5). Every rule tq
// ships has exactly one; a mode gives each category a Value.
type Category string

const (
	CatIdentity         Category = "identity"
	CatSecrets          Category = "secrets"
	CatSelfProtection   Category = "self-protection"
	CatPrivacy          Category = "privacy"
	CatGitHistory       Category = "git-history"
	CatProdChanges      Category = "prod-changes"
	CatCloudDestructive Category = "cloud-destructive"
	CatDBDestructive    Category = "db-destructive"
	CatSystem           Category = "system"
	CatExternalWrites   Category = "external-writes"
	CatCISupplyChain    Category = "ci-supply-chain"
	CatHygiene          Category = "hygiene"
	CatPacks            Category = "packs"
)

// allCategories is the order of spec E §4's table: the locked groups first.
var allCategories = []Category{
	CatIdentity, CatSecrets, CatSelfProtection, CatPrivacy,
	CatGitHistory, CatProdChanges, CatCloudDestructive, CatDBDestructive,
	CatSystem, CatExternalWrites, CatCISupplyChain, CatHygiene, CatPacks,
}

// AllCategories returns the thirteen categories in display order (locked first).
func AllCategories() []Category { return append([]Category{}, allCategories...) }

// Locked reports whether c is enforced in every mode except off and can never
// be toggled: identity, secrets, self-protection and privacy.
func Locked(c Category) bool {
	switch c {
	case CatIdentity, CatSecrets, CatSelfProtection, CatPrivacy:
		return true
	}
	return false
}

// LockedCategories returns the locked categories in display order.
func LockedCategories() []Category {
	return []Category{CatIdentity, CatSecrets, CatSelfProtection, CatPrivacy}
}

// ParseCategory returns the category named s, and false for an unknown name.
func ParseCategory(s string) (Category, bool) {
	for _, c := range allCategories {
		if string(c) == s {
			return c, true
		}
	}
	return "", false
}

// GuardIDPrefix prefixes the identity guard's checks (internal/guard
// Decision.Rule) so the table, the audit and the docs can name them.
const GuardIDPrefix = "guard/"

// IdentityGuardIDs are the ids of the identity guard's checks, which run
// before every policy rule. They are all CatIdentity.
var IdentityGuardIDs = []string{
	"guard/blocked-command", "guard/wrong-cloud", "guard/neutral-remote", "guard/untrusted",
	"guard/env-drift", "guard/claude-config-drift", "guard/git-email-drift", "guard/gh-user",
}

// Plugin-side guards (Python, used when the tq binary is missing or for
// headless runs), classified so docs and tests can name them.
const (
	PluginIdentityGuardID         = "plugin/identity-guard"
	PluginHeadlessManifestGuardID = "plugin/headless-manifest-guard"
)

// New built-in ids of spec E (rules written by WS1/WS2).
const (
	NestedAgentID      = "tq/nested-agent-loosened"
	ModeChangeID       = "tq/mode-change"
	OutsidePathWriteID = "tq/outside-path-write"
	SystemDirWriteID   = "tq/system-dir-write"
	ElevationID        = "tq/elevation"
)

// Categories classifies every rule id tq knows (spec E §5.2): built-in policy
// rules, HookRules, Jev judgment rules, GuardErrorID, the identity guard's
// checks, the plugin guards, and pack guards from branches not merged yet.
// TestEveryRuleHasCategory fails when a built-in id is missing here.
var Categories = map[string]Category{
	// identity
	"guard/blocked-command":     CatIdentity,
	"guard/wrong-cloud":         CatIdentity,
	"guard/neutral-remote":      CatIdentity,
	"guard/untrusted":           CatIdentity,
	"guard/env-drift":           CatIdentity,
	"guard/claude-config-drift": CatIdentity,
	"guard/git-email-drift":     CatIdentity,
	"guard/gh-user":             CatIdentity,
	PluginIdentityGuardID:       CatIdentity,
	"tq/cross-client-mcp":       CatIdentity,
	"tq/iac-identity-drift":     CatIdentity,
	// secrets
	"tq/secret-store-read":       CatSecrets,
	"tq/secret-store-shell":      CatSecrets,
	"tq/env-file-read":           CatSecrets,
	"tq/env-file-shell":          CatSecrets,
	"tq/env-dump":                CatSecrets,
	"tq/commit-secret":           CatSecrets,
	"jev/service-role-client":    CatSecrets,
	"jev/n8n-inline-credentials": CatSecrets,
	// self-protection
	"tq/guard-change":             CatSelfProtection,
	"tq/guard-file-write":         CatSelfProtection,
	"tq/settings-widen":           CatSelfProtection,
	"tq/settings-shell-write":     CatSelfProtection,
	"tq/trust-store-write":        CatSelfProtection,
	GuardErrorID:                  CatSelfProtection,
	NestedAgentID:                 CatSelfProtection,
	ModeChangeID:                  CatSelfProtection,
	PluginHeadlessManifestGuardID: CatSelfProtection,
	"tq/hub-store-write":          CatSelfProtection,
	"tq/pack-config-change":       CatSelfProtection,
	"tq/human-answer":             CatSelfProtection,
	// privacy
	"tq/commit-pii":       CatPrivacy,
	"tq/push-pii":         CatPrivacy,
	"tq/text-pii":         CatPrivacy,
	"tq/pii-fixture-dump": CatPrivacy,
	// git-history
	"tq/force-push-main": CatGitHistory,
	"tq/force-push":      CatGitHistory,
	"tq/gh-admin-merge":  CatGitHistory,
	"tq/gh-repo-delete":  CatGitHistory,
	// prod-changes
	"tq/iac-apply":        CatProdChanges,
	"tq/remote-migration": CatProdChanges,
	"tq/dbt-prod-target":  CatProdChanges,
	"tq/model-promote":    CatProdChanges,
	"tq/pipeline-run":     CatProdChanges,
	"tq/iac-apply-env":    CatProdChanges,
	"tq/db-migrate-env":   CatProdChanges,
	"tq/restore-target":   CatProdChanges,
	// cloud-destructive
	"tq/cloud-delete": CatCloudDestructive,
	"tq/iac-destroy":  CatCloudDestructive,
	// db-destructive
	"tq/sql-destructive-shell": CatDBDestructive,
	"jev/destructive-sql":      CatDBDestructive,
	"tq/migration-edit":        CatDBDestructive,
	"tq/mcp-sql-write":         CatDBDestructive,
	"tq/mcp-apply-migration":   CatDBDestructive,
	"jev/rls-weakened":         CatDBDestructive,
	"tq/dbt-full-refresh":      CatDBDestructive,
	// system
	"tq/registry-write": CatSystem,
	OutsidePathWriteID:  CatSystem,
	SystemDirWriteID:    CatSystem,
	ElevationID:         CatSystem,
	// external-writes
	"tq/mcp-github-write":  CatExternalWrites,
	"tq/mcp-n8n-write":     CatExternalWrites,
	"tq/mcp-hosting-exec":  CatExternalWrites,
	"tq/tracker-cli-write": CatExternalWrites,
	// ci-supply-chain
	"tq/gha-unpinned":   CatCISupplyChain,
	"tq/gha-branch-ref": CatCISupplyChain,
	"tq/gha-pr-target":  CatCISupplyChain,
	// hygiene
	"tq/commit-message": CatHygiene,
	// packs (fallback: cost guards)
	"tq/warehouse-resize":          CatPacks,
	"tq/bq-unbounded-query":        CatPacks,
	"tq/warehouse-query-unbounded": CatPacks,
}

// CategoryOf returns r's category: the table entry for a built-in id, else
// the rule's own declared category (custom rules, `category:`). "" means
// unclassified, which every mode applies at the rule's own action (fail
// closed). An unknown declared name is "" too.
func CategoryOf(r *policy.Rule) Category {
	if r == nil {
		return ""
	}
	if c, ok := Categories[r.ID]; ok {
		return c
	}
	if c, ok := ParseCategory(r.Category); ok {
		return c
	}
	return ""
}

// LockedIDs returns the ids in Categories that sit in a locked category, by
// category, each list sorted. It replaces AlwaysEnforced.
func LockedIDs() map[Category][]string {
	out := map[Category][]string{}
	for id, c := range Categories {
		if Locked(c) {
			out[c] = append(out[c], id)
		}
	}
	for _, ids := range out {
		sort.Strings(ids)
	}
	return out
}
