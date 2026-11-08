package envplan

import (
	"path/filepath"
	"strings"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// A shell whose __TQ_STATE is gone (unset, truncated, edited) while TQ_WS says an identity is
// active cannot be restored: tq no longer knows the values from before it. It fails closed
//: every variable tq sets for any identity and every mask folder on PATH are
// removed, everything else stays, the identity of the folder (if any) is applied afresh, and
// the prompt says the old values are lost and that a new terminal has them.

// LostState reports a shell whose tq state is gone: TQ_WS set, __TQ_STATE missing or not a
// state tq wrote.
func LostState(env Env) bool {
	if ws, _ := env.Lookup("TQ_WS"); strings.TrimSpace(ws) == "" {
		return false
	}
	raw, has := env.Lookup(StateVar)
	if !has || strings.TrimSpace(raw) == "" {
		// No state at all: only the values tq set under the identities folder go, and the
		// folder's identity is applied again, so the shell loses nothing it needs.
		return true
	}
	st, ok := DecodeStateOK(raw)
	return !ok || st.WS == ""
}

// tqVars are the variables tq sets for some identity: the workspace markers, the state and
// every config home of the catalog.
func tqVars() map[string]bool {
	m := map[string]bool{envKey(StateVar): true}
	for _, k := range wsVars {
		m[envKey(k)] = true
	}
	for _, k := range AllProviderVars() {
		m[envKey(k)] = true
	}
	m[envKey("BROWSER")] = true // a client's shim (tqOwned says whose)
	return m
}

// tqOwned reports whether a variable holds something tq set for an identity: a workspace
// marker or the state, or a config home of the catalog whose value is under the tq identities
// folder. A user's own AWS_CONFIG_FILE elsewhere is the user's and stays.
func tqOwned(drop map[string]bool, name, value string) bool {
	k := envKey(name)
	if !drop[k] {
		return false
	}
	if k == envKey(StateVar) || k == envKey("TQ_WS") || k == envKey("TQ_WS_ROOT") {
		return true
	}
	if k == envKey("BROWSER") {
		// the user's own BROWSER stays; one that names a program of the shims folder is tq's
		v := strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"`))
		return v != "" && isAnyShims(filepath.Dir(filepath.FromSlash(v)))
	}
	root := filepath.Clean(paths.IdentitiesRoot())
	v := filepath.Clean(value)
	return samePath(v, root) || strings.HasPrefix(strings.ToLower(v), strings.ToLower(root)+string(filepath.Separator))
}

// isAnyMask reports whether an OS path is some identity's mask folder.
func isAnyMask(p string) bool {
	p = filepath.Clean(strings.Trim(p, `"`))
	return filepath.Base(p) == MaskDirName && samePath(filepath.Dir(filepath.Dir(p)), filepath.Clean(paths.IdentitiesRoot()))
}

// stripMasks removes every identity's mask folder and shims folder from a PATH in syn's spelling.
func stripMasks(path string, syn PathSyntax) string {
	var kept []string
	for _, e := range strings.Split(path, syn.Sep) {
		if e != "" && (isAnyMask(syn.ToOS(e)) || isAnyShims(syn.ToOS(e))) {
			continue
		}
		kept = append(kept, e)
	}
	return strings.Join(kept, syn.Sep)
}

// Forget is env as tq finds it with no state: tq's variables absent, no mask folder on PATH.
func Forget(env Env) Env { return hide(env, func(string) bool { return true }, true) }

// Unrecorded is env with the tq variables prev does not record hidden (a state edited by hand
// that names fewer variables than tq set), and the mask folders
// stripped from a PATH prev does not record. found reports whether anything was hidden; the
// caller then completes the diff with FinishForget.
func Unrecorded(env Env, prev State) (Env, bool) {
	rec := map[string]bool{envKey(StateVar): true}
	for k := range prev.Prev {
		rec[envKey(k)] = true
	}
	out := hide(env, func(k string) bool { return !rec[envKey(k)] }, !rec[envKey("PATH")])
	found := len(out.Names) != len(env.Names)
	if !found && env.PathOK {
		a, _ := env.Lookup("PATH")
		b, _ := out.Lookup("PATH")
		found = a != b
	}
	return out, found
}

// hide is env without the tq-owned variables pick selects and, when masks, without mask
// folders on PATH.
func hide(env Env, pick func(string) bool, masks bool) Env {
	drop := tqVars()
	out := env
	out.Lookup = func(k string) (string, bool) {
		v, ok := env.Lookup(k)
		if ok && pick(k) && tqOwned(drop, k, v) {
			return "", false
		}
		if ok && masks && envKey(k) == envKey("PATH") && env.PathOK {
			v = stripMasks(v, env.Syntax)
		}
		return v, ok
	}
	out.Names = nil
	for _, n := range env.Names {
		if v, _ := env.Lookup(n); !(pick(n) && tqOwned(drop, n, v)) {
			out.Names = append(out.Names, n)
		}
	}
	return out
}

// FinishForget completes ops (computed against forgot, Forget or Unrecorded of real) so the
// shell matches it: every variable forgot hid that ops does not set is unset, a PATH ops does
// not set gets forgot's (no mask folder), and the notice says the old values are lost.
func FinishForget(ops *Ops, real, forgot Env) {
	set := map[string]bool{}
	for k := range ops.Set {
		set[envKey(k)] = true
	}
	unset := map[string]bool{}
	for _, k := range ops.Unset {
		unset[envKey(k)] = true
	}
	for _, n := range real.Names {
		if _, seen := forgot.Lookup(n); !seen && !set[envKey(n)] && !unset[envKey(n)] {
			ops.Unset = append(ops.Unset, n)
			unset[envKey(n)] = true
		}
	}
	if !set[envKey("PATH")] && real.PathOK {
		if cur, ok := real.Lookup("PATH"); ok {
			if clean, _ := forgot.Lookup("PATH"); clean != cur {
				ops.Set["PATH"] = clean
			}
		}
	}
	ops.Changed = true
	ops.Notice = &Notice{Kind: NoticeStateLost}
}
