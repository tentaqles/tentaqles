package envplan

import "testing"

func TestOffEnviron(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	orig := "/mine"
	st := State{WS: "acme", Prev: map[string]*string{"TQ_WS": nil, "TQ_SOMETHING": &orig}}
	base := []string{"PATH=/bin", "TQ_WS=acme", "TQ_WS_ROOT=/w", "CLAUDE_CONFIG_DIR=/id/claude", "CODEX_HOME=/id/codex",
		"TQ_ENABLED=1", "TQ_SOMETHING=x", StateVar + "=" + st.Encode()}
	got := envMap(OffEnviron(base))
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "TQ_WS", "TQ_WS_ROOT", StateVar} {
		if _, ok := got[k]; ok {
			t.Errorf("%s must be removed: %v", k, got)
		}
	}
	if got["PATH"] != "/bin" || got["TQ_SOMETHING"] != "/mine" || got["TQ_ENABLED"] != "1" {
		t.Fatalf("%v", got)
	}
	if n := len(OffEnviron(base)); n != 3 {
		t.Fatalf("off env = %v", OffEnviron(base))
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
