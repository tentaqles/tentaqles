package envplan

import "sort"

// Owned by WS2 (spec E §8): the env side of tq off / tq on / tq run --off.

// ModeVar is the per-launch guard mode (guardcfg.EnvMode; envplan cannot
// import guardcfg).
const ModeVar = "TQ_MODE"

// HeadlessModeVar carries the workspace's guard.headless_mode from Environ to
// the hub runner's adapter.LaunchEnv, which turns it into TQ_MODE for a hub
// round (spec E §6.4). It comes from the trusted manifest only: Environ drops
// an inherited value, and Desired never sets it, so it never reaches a shell.
const HeadlessModeVar = "TQ_HEADLESS_MODE"

// wsVars are the workspace markers Desired sets; off drops them even when the
// shell state that recorded them is lost.
var wsVars = []string{"TQ_WS", "TQ_WS_ROOT"}

// OffOps is the shell code for `tq off`: every variable recorded in
// __TQ_STATE restored to its previous value or unset (provider variables are
// unset regardless), every catalog provider variable and workspace marker
// that is set removed, __TQ_STATE unset, and TQ_MODE=off set.
func OffOps(current func(string) (string, bool)) Ops {
	enc, hasState := current(StateVar)
	st := DecodeState(enc)
	ops := Ops{Set: map[string]string{}, Changed: true, From: st.WS}
	provider := map[string]bool{}
	for _, k := range AllProviderVars() {
		provider[envKey(k)] = true
	}
	unset := map[string]bool{}
	if hasState {
		unset[StateVar] = true
	}
	for k, prev := range st.Prev {
		if prev != nil && !provider[envKey(k)] {
			ops.Set[k] = *prev
		} else {
			unset[k] = true
		}
	}
	for _, k := range append(AllProviderVars(), wsVars...) {
		if _, ok := current(k); ok {
			if _, restored := ops.Set[k]; !restored {
				unset[k] = true
			}
		}
	}
	delete(unset, ModeVar)
	for k := range unset {
		ops.Unset = append(ops.Unset, k)
	}
	sort.Strings(ops.Unset)
	ops.Set[ModeVar] = "off"
	return ops
}

// OnOps is the shell code for `tq on`: TQ_MODE unset. The next `tq env`
// re-applies the folder's identity.
func OnOps() Ops { return Ops{Set: map[string]string{}, Unset: []string{ModeVar}, Changed: true} }

// OffEnviron is the environment of an off launch (tq run --off): base
// cleaned of every tq change (Clean), without the workspace markers, the
// headless mode and TQ_ENABLED, plus TQ_MODE=off.
func OffEnviron(base []string) []string {
	drop := map[string]bool{envKey(ModeVar): true, envKey(HeadlessModeVar): true, envKey("TQ_ENABLED"): true}
	for _, k := range wsVars {
		drop[envKey(k)] = true
	}
	cleaned := Clean(base)
	out := make([]string, 0, len(cleaned)+1)
	for _, kv := range cleaned {
		k, _, _ := splitKV(kv)
		if drop[envKey(k)] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, ModeVar+"=off")
}
