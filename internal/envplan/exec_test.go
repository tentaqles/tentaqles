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
