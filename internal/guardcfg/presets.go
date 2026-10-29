package guardcfg

// Mode names (spec E §4). Off is never stored in a file: it exists only as a
// launch (TQ_MODE=off) or a window state.
const (
	ModeOff     = "off"
	ModeDefault = "default"
	ModeRelaxed = "relaxed"
	ModeStrict  = "strict"
	ModeCustom  = "custom"
)

// EnvMode is the per-launch override (layer 6). The hook reads it from its own
// process env, never from a tool call's command line.
const EnvMode = "TQ_MODE"

// StoredModes are the values guard.mode may take in a file, in display order.
func StoredModes() []string { return []string{ModeDefault, ModeRelaxed, ModeStrict, ModeCustom} }

// PresetNames are the presets a custom table may use as base.
func PresetNames() []string { return []string{ModeDefault, ModeRelaxed, ModeStrict} }

// ValidStoredMode reports whether s may be stored as guard.mode ("off" may not).
func ValidStoredMode(s string) bool {
	switch s {
	case ModeDefault, ModeRelaxed, ModeStrict, ModeCustom:
		return true
	}
	return false
}

// ValidBase reports whether s may be a custom table's base.
func ValidBase(s string) bool {
	switch s {
	case ModeDefault, ModeRelaxed, ModeStrict:
		return true
	}
	return false
}

func allRule() Table {
	t := Table{}
	for _, c := range allCategories {
		t[c] = ValueRule
	}
	return t
}

// presets are spec E §4's table. Locked categories are always ValueRule.
var presets = map[string]Table{
	ModeDefault: allRule(),
	ModeRelaxed: func() Table {
		t := allRule()
		for _, c := range []Category{CatProdChanges, CatCloudDestructive, CatDBDestructive, CatExternalWrites, CatCISupplyChain, CatHygiene, CatPacks} {
			t[c] = ValueSkip
		}
		t[CatGitHistory] = ValueAsk
		t[CatSystem] = ValueAsk
		return t
	}(),
	ModeStrict: func() Table {
		t := allRule()
		t[CatProdChanges] = ValueDeny
		t[CatCloudDestructive] = ValueDeny
		t[CatGitHistory] = ValueDeny
		t[CatExternalWrites] = ValueAsk
		return t
	}(),
}

// Preset returns a copy of the named preset's table (default, relaxed or
// strict) with every category set, and false for any other name.
func Preset(name string) (Table, bool) {
	t, ok := presets[name]
	if !ok {
		return nil, false
	}
	return t.Clone(), true
}
