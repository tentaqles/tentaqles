package bundle

import (
	"path/filepath"
	"reflect"
)

// SyncSettings rewrites <dir>/settings.json so that enabledPlugins and
// extraKnownMarketplaces match d exactly, preserving every other key.
// changed reports whether the resulting file differs from what was there.
func SyncSettings(dir string, d Desired) (bool, error) {
	path := filepath.Join(dir, "settings.json")

	current, err := ReadJSONMap(path)
	if err != nil {
		return false, err
	}

	next := map[string]any{}
	for k, v := range current {
		next[k] = v
	}
	next["enabledPlugins"] = toAnyMapBool(d.EnabledPlugins)
	next["extraKnownMarketplaces"] = toAnyMapAny(d.Marketplaces)

	if reflect.DeepEqual(current, next) {
		return false, nil
	}

	if err := WriteJSONAtomic(path, next); err != nil {
		return false, err
	}
	return true, nil
}

func toAnyMapBool(m map[string]bool) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toAnyMapAny(m map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
