package envplan

import (
	"strings"
	"testing"
)

func TestEnviron_ReplacesAndStripsState(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	base := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/old", StateVar + "=abc", "KEEP=1"}
	got := Environ(ws("acme", "claude"), base)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "CLAUDE_CONFIG_DIR=/old") || strings.Contains(joined, StateVar) {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(joined, "KEEP=1") || !strings.Contains(joined, "TQ_WS=acme") || !strings.Contains(joined, "CLAUDE_CONFIG_DIR=") {
		t.Fatalf("%v", got)
	}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestEnviron_UndoesOtherWorkspaceShellState(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	orig := "orig"
	st := State{WS: "a", Prev: map[string]*string{"GH_CONFIG_DIR": nil, "CLAUDE_CONFIG_DIR": &orig}}
	base := []string{"PATH=/bin", "GH_CONFIG_DIR=A", "CLAUDE_CONFIG_DIR=A2", StateVar + "=" + st.Encode()}
	b := ws("b", "claude")
	got := envMap(Environ(b, base))
	if _, ok := got["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR leaked from workspace a: %v", got)
	}
	if _, ok := got[StateVar]; ok {
		t.Fatalf("%s kept: %v", StateVar, got)
	}
	if want := Desired(b)["CLAUDE_CONFIG_DIR"]; got["CLAUDE_CONFIG_DIR"] != want {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want %q", got["CLAUDE_CONFIG_DIR"], want)
	}
	if got["PATH"] != "/bin" {
		t.Fatalf("PATH lost: %v", got)
	}
}

func TestEnviron_DropsStrayProviderVars(t *testing.T) {
	t.Setenv("TQ_HOME", "h")
	base := []string{"PATH=/bin", "AWS_CONFIG_FILE=/a/config", "KEEP=1"}
	got := envMap(Environ(ws("b", "claude"), base))
	if _, ok := got["AWS_CONFIG_FILE"]; ok {
		t.Fatalf("stray AWS_CONFIG_FILE kept: %v", got)
	}
	if got["KEEP"] != "1" {
		t.Fatalf("KEEP lost: %v", got)
	}
}

func TestClean_RestoresPrevValues(t *testing.T) {
	orig := "orig"
	st := State{WS: "a", Prev: map[string]*string{"TQ_SOMETHING": &orig}}
	got := envMap(Clean([]string{"TQ_SOMETHING=new", StateVar + "=" + st.Encode()}))
	if got["TQ_SOMETHING"] != "orig" {
		t.Fatalf("prev not restored: %v", got)
	}
	if _, ok := got[StateVar]; ok {
		t.Fatalf("state kept: %v", got)
	}
}

func TestAllProviderVars_CoversCatalog(t *testing.T) {
	have := map[string]bool{}
	for _, k := range AllProviderVars() {
		have[k] = true
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "GH_CONFIG_DIR", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "KUBECONFIG", "CLOUDSDK_CONFIG"} {
		if !have[k] {
			t.Errorf("AllProviderVars missing %s", k)
		}
	}
}
