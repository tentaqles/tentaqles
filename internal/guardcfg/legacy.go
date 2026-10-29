package guardcfg

import "sort"

// The pre-spec-E unattended names, kept so callers outside WS1 (tq
// unattended, which becomes an alias of tq mode) keep compiling until they
// move to the categories. Deprecated: use Category, LockedIDs and
// UnknownOverrides.

// UnattendedGroup is the old classification. Deprecated: use Category.
type UnattendedGroup string

// The old groups. GroupPrivacy is new: the PII rules, locked by spec E.
const (
	GroupIdentity       UnattendedGroup = "identity"
	GroupSecret         UnattendedGroup = "secret"
	GroupSelfProtection UnattendedGroup = "self-protection"
	GroupPrivacy        UnattendedGroup = "privacy"
	GroupSkippable      UnattendedGroup = "skippable"
)

// AlwaysEnforced returns the locked built-in ids by old group name, each
// list sorted. Deprecated: use LockedIDs.
func AlwaysEnforced() map[UnattendedGroup][]string {
	names := map[Category]UnattendedGroup{CatIdentity: GroupIdentity, CatSecrets: GroupSecret, CatSelfProtection: GroupSelfProtection, CatPrivacy: GroupPrivacy}
	out := map[UnattendedGroup][]string{}
	for c, ids := range LockedIDs() {
		out[names[c]] = append([]string{}, ids...)
		sort.Strings(out[names[c]])
	}
	return out
}
