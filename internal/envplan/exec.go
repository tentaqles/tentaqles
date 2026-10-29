package envplan

import (
	"runtime"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// envKey normalises a variable name for comparison: Windows names are
// case-insensitive.
func envKey(k string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}

func splitKV(kv string) (string, string, bool) {
	if i := strings.IndexByte(kv, '='); i >= 0 {
		return kv[:i], kv[i+1:], true
	}
	return kv, "", false
}

// AllProviderVars returns every variable name any catalog provider can set,
// sorted.
func AllProviderVars() []string {
	seen := map[string]bool{}
	for _, p := range Providers() {
		for k := range p.Vars("x") {
			seen[k] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Clean undoes what the tq shell hook set for whatever workspace the base
// environment belongs to: every variable recorded in __TQ_STATE is restored to
// its previous value (or unset when it had none), __TQ_STATE is dropped, and
// every variable any catalog provider can set is removed regardless, so no
// other client's config home survives.
func Clean(base []string) []string {
	var state *State
	for _, kv := range base {
		if k, v, _ := splitKV(kv); envKey(k) == envKey(StateVar) {
			s := DecodeState(v)
			state = &s
		}
	}
	drop := map[string]bool{envKey(StateVar): true}
	provider := map[string]bool{}
	for _, k := range AllProviderVars() {
		drop[envKey(k)] = true
		provider[envKey(k)] = true
	}
	restore := map[string]string{}
	if state != nil {
		for k, prev := range state.Prev {
			drop[envKey(k)] = true
			if prev != nil && !provider[envKey(k)] {
				restore[k] = *prev // provider vars are removed regardless of Prev
			}
		}
	}
	out := make([]string, 0, len(base)+len(restore))
	for _, kv := range base {
		k, _, _ := splitKV(kv)
		if drop[envKey(k)] {
			continue
		}
		out = append(out, kv)
	}
	keys := make([]string, 0, len(restore))
	for k := range restore {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+restore[k])
	}
	return out
}

// Environ returns base cleaned of any other workspace's shell state (see
// Clean) with the workspace's desired vars applied, plus HeadlessModeVar when
// the manifest sets guard.headless_mode (an inherited value is dropped). A
// stale TQ_WS / TQ_WS_ROOT is dropped too.
func Environ(ws *resolve.Workspace, base []string) []string {
	desired := Desired(ws)
	if ws != nil && ws.Manifest != nil {
		if m := strings.TrimSpace(ws.Manifest.Guard.HeadlessMode); m != "" {
			desired[HeadlessModeVar] = m
		}
	}
	// The workspace markers come from desired only: a neutral launch from a
	// shell whose state was lost must not carry another workspace's name.
	dk := map[string]bool{envKey(HeadlessModeVar): true}
	for _, k := range wsVars {
		dk[envKey(k)] = true
	}
	for k := range desired {
		dk[envKey(k)] = true
	}
	cleaned := Clean(base)
	out := make([]string, 0, len(cleaned)+len(desired))
	for _, kv := range cleaned {
		k, _, _ := splitKV(kv)
		if dk[envKey(k)] {
			continue
		}
		out = append(out, kv)
	}
	keys := make([]string, 0, len(desired))
	for k := range desired {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+desired[k])
	}
	return out
}
