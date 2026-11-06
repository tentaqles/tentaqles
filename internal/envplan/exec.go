package envplan

import (
	"os"
	"path/filepath"
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
				if envKey(k) == envKey("PATH") && state.PathSet != "" {
					restore[k] = MergePath(*prev, state.PathSet, lookup(base, k), string(os.PathListSeparator))
				}
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

// launchState appends to out the __TQ_STATE recording every variable out changed from
// cleaned (the environment before the identity), with the PATH tq wrote. A neutral launch
// (ws nil) carries none.
func launchState(ws *resolve.Workspace, cleaned, out []string) []string {
	if ws == nil {
		return out
	}
	type kv struct{ k, v string }
	index := func(env []string) map[string]kv {
		m := map[string]kv{}
		for _, e := range env {
			k, v, _ := splitKV(e)
			if k != "" && envKey(k) != envKey(StateVar) {
				m[envKey(k)] = kv{k, v}
			}
		}
		return m
	}
	before, after := index(cleaned), index(out)
	st := State{WS: ws.Name, Prev: map[string]*string{}}
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	for k := range keys {
		b, hadB := before[k]
		a, hasA := after[k]
		if hadB == hasA && b.v == a.v {
			continue
		}
		name := a.k
		if !hasA {
			name = b.k
		}
		if k == envKey("PATH") {
			name = "PATH"
			if hasA {
				st.PathSet, st.Sep = a.v, string(os.PathListSeparator)
				for _, e := range filepath.SplitList(a.v) {
					if isAnyMask(e) {
						st.MaskWanted = true
					}
				}
			}
		}
		if hadB {
			v := b.v
			st.Prev[name] = &v
		} else {
			st.Prev[name] = nil
		}
	}
	res := make([]string, 0, len(out)+1)
	for _, e := range out {
		if k, _, _ := splitKV(e); envKey(k) != envKey(StateVar) {
			res = append(res, e)
		}
	}
	return append(res, StateVar+"="+st.Encode())
}

// lookup is the value of k in an environment list ("" when absent).
func lookup(env []string, k string) string {
	for _, kv := range env {
		if kk, v, _ := splitKV(kv); envKey(kk) == envKey(k) {
			return v
		}
	}
	return ""
}

// Environ returns base cleaned of any other workspace's shell state (see
// Clean) with the workspace's desired vars applied. A stale TQ_WS /
// TQ_WS_ROOT is dropped too. For an identity (ADR-0008) the
// out-of-scope variables are removed and PATH is the identity PATH
// (FilterPath of the cleaned PATH), so a launch from outside the folder
// (tq run <ws>, an editor) gets the same layer as the identity terminal.
//
// The launch carries a __TQ_STATE of what it changed (launchState), so a shell started in it
// (the harness's Bash tool, a terminal from an editor) can leave the identity and is never
// taken for a shell whose state was lost.
func Environ(ws *resolve.Workspace, base []string) []string {
	return launchState(ws, Clean(base), environOf(ws, base))
}

func environOf(ws *resolve.Workspace, base []string) []string {
	desired := Desired(ws)
	if l := LayerFor(ws); l != nil {
		return l.environ(desired, Clean(base))
	}
	// The workspace markers come from desired only: a neutral launch from a
	// shell whose state was lost must not carry another workspace's name.
	dk := map[string]bool{}
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

// environ is Environ for a layer: cleaned without the out-of-scope variables and
// the workspace markers, PATH filtered (its key keeps its spelling, Path on Windows), then
// desired applied.
func (l *Layer) environ(desired map[string]string, cleaned []string) []string {
	skip := map[string]bool{}
	for _, k := range wsVars {
		skip[envKey(k)] = true
	}
	for k := range desired {
		skip[envKey(k)] = true
	}
	for _, k := range l.UnsetNames(environNames(cleaned), desired) {
		skip[envKey(k)] = true
	}
	out := make([]string, 0, len(cleaned)+len(desired))
	for _, kv := range cleaned {
		k, v, _ := splitKV(kv)
		switch {
		case skip[envKey(k)]:
		case envKey(k) == envKey("PATH"):
			np, _ := l.FilterPath(v, NativeSyntax())
			out = append(out, k+"="+np)
		default:
			out = append(out, kv)
		}
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
