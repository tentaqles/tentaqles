package policy

// packRules are the static regex rules of the packs (spec C §2.10). Each
// sets Pack: the hook drops it while that pack is not enabled. One id may
// appear once per pack (tq/hub-store-write is listed for cloud and for db).
// Spec D's guard-hardening and iac-policy workstreams append their blocks
// here.
func packRules() []Rule {
	return []Rule{}
}

// PackRules returns the static pack rules (for listing).
func PackRules() []Rule { return packRules() }

// ForPacks returns rules without the pack rules whose pack is not enabled.
func ForPacks(rules []Rule, enabled func(pack string) bool) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.Pack != "" && (enabled == nil || !enabled(r.Pack)) {
			continue
		}
		out = append(out, r)
	}
	return out
}
