package envplan

import (
	"slices"
	"strings"
	"testing"
)

func lookupIn(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestOffOps_UndoesStateAndSetsOff(t *testing.T) {
	orig := "/mine"
	st := State{WS: "acme", Prev: map[string]*string{
		"TQ_WS": nil, "TQ_WS_ROOT": nil, "GH_CONFIG_DIR": nil,
		"CLAUDE_CONFIG_DIR": &orig, // provider var: removed even though it had a value
		"TQ_SOMETHING":      &orig, // not a provider var: restored
	}}
	cur := map[string]string{
		StateVar: st.Encode(), "TQ_WS": "acme", "TQ_WS_ROOT": "/w/acme", "GH_CONFIG_DIR": "/id/gh",
		"CLAUDE_CONFIG_DIR": "/id/claude", "TQ_SOMETHING": "x", "AWS_CONFIG_FILE": "/stray", "PATH": "/bin",
	}
	ops := OffOps(lookupIn(cur))
	if !ops.Changed || ops.Set["TQ_MODE"] != "off" {
		t.Fatalf("TQ_MODE=off must be set: %+v", ops)
	}
	if ops.Set["TQ_SOMETHING"] != "/mine" {
		t.Fatalf("TQ_SOMETHING must be restored: %+v", ops)
	}
	for _, k := range []string{StateVar, "TQ_WS", "TQ_WS_ROOT", "GH_CONFIG_DIR", "CLAUDE_CONFIG_DIR", "AWS_CONFIG_FILE"} {
		if !slices.Contains(ops.Unset, k) {
			t.Errorf("%s must be unset: %v", k, ops.Unset)
		}
		if _, set := ops.Set[k]; set {
			t.Errorf("%s must not be set: %v", k, ops.Set)
		}
	}
	if slices.Contains(ops.Unset, "PATH") || !slices.IsSorted(ops.Unset) {
		t.Fatalf("unsets: %v", ops.Unset)
	}
	if ops.To != "" || ops.From != "acme" {
		t.Fatalf("from/to: %q -> %q", ops.From, ops.To)
	}
}

func TestOffOps_NeutralShellOnlySetsOff(t *testing.T) {
	ops := OffOps(lookupIn(map[string]string{"PATH": "/bin"}))
	if len(ops.Unset) != 0 || len(ops.Set) != 1 || ops.Set["TQ_MODE"] != "off" {
		t.Fatalf("%+v", ops)
	}
}

func TestOnOps(t *testing.T) {
	ops := OnOps()
	if !ops.Changed || len(ops.Set) != 0 || strings.Join(ops.Unset, ",") != "TQ_MODE" {
		t.Fatalf("%+v", ops)
	}
}

func TestOffEnviron(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	orig := "/mine"
	st := State{WS: "acme", Prev: map[string]*string{"TQ_WS": nil, "TQ_SOMETHING": &orig}}
	base := []string{"PATH=/bin", "TQ_WS=acme", "TQ_WS_ROOT=/w", "CLAUDE_CONFIG_DIR=/id/claude", "CODEX_HOME=/id/codex",
		"TQ_MODE=strict", "TQ_ENABLED=1", "TQ_SOMETHING=x", HeadlessModeVar + "=relaxed", StateVar + "=" + st.Encode()}
	got := envMap(OffEnviron(base))
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "TQ_WS", "TQ_WS_ROOT", StateVar, HeadlessModeVar, "TQ_ENABLED"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must be removed: %v", k, got)
		}
	}
	if got["TQ_MODE"] != "off" || got["PATH"] != "/bin" || got["TQ_SOMETHING"] != "/mine" {
		t.Fatalf("%v", got)
	}
	n := 0
	for _, kv := range OffEnviron(base) {
		if strings.HasPrefix(kv, "TQ_MODE=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("TQ_MODE appears %d times", n)
	}
}

// Environ carries guard.headless_mode to the hub runner's LaunchEnv in
// HeadlessModeVar, from the manifest only: an inherited value is dropped.
func TestEnviron_HeadlessModeFromManifestOnly(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	base := []string{"PATH=/bin", HeadlessModeVar + "=relaxed"}
	w := ws("acme")
	if _, ok := envMap(Environ(w, base))[HeadlessModeVar]; ok {
		t.Fatal("an inherited headless mode must be dropped")
	}
	w.Manifest.Guard.HeadlessMode = "strict"
	if got := envMap(Environ(w, base))[HeadlessModeVar]; got != "strict" {
		t.Fatalf("headless mode = %q, want strict", got)
	}
	if _, ok := Desired(w)[HeadlessModeVar]; ok {
		t.Fatal("the headless mode must never reach the interactive shell (Desired)")
	}
	if _, ok := envMap(Environ(nil, base))[HeadlessModeVar]; ok {
		t.Fatal("neutral: dropped")
	}
}

func TestEnviron_DropsStaleWorkspaceMarkers(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	base := []string{"PATH=/bin", "TQ_WS=other", "TQ_WS_ROOT=/w/other"}
	got := envMap(Environ(nil, base))
	if _, ok := got["TQ_WS"]; ok {
		t.Fatal("neutral: a stale TQ_WS must be dropped")
	}
	if _, ok := got["TQ_WS_ROOT"]; ok {
		t.Fatal("neutral: a stale TQ_WS_ROOT must be dropped")
	}
	if got := envMap(Environ(ws("acme"), base)); got["TQ_WS"] != "acme" {
		t.Fatalf("TQ_WS = %q", got["TQ_WS"])
	}
}
