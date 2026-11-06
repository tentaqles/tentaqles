package markers

import (
	"slices"
	"testing"
)

// TestIDETerminalVariableIsNoMarker: the Claude Code IDE integration sets CLAUDE_CODE_SSE_PORT in
// every integrated terminal, so it marks a human's terminal as much as an agent's (ADR-0010).
func TestIDETerminalVariableIsNoMarker(t *testing.T) {
	if slices.Contains(Markers, "CLAUDE_CODE_SSE_PORT") {
		t.Fatal("CLAUDE_CODE_SSE_PORT is set in a human's IDE terminal; it must not be a marker")
	}
	for _, k := range Markers {
		t.Setenv(k, "")
	}
	t.Setenv("CLAUDE_CODE_SSE_PORT", "1234")
	if m, ok := Set(); ok {
		t.Fatalf("a terminal with only the IDE variable reads as an agent (%s)", m)
	}
}

// TestClaudeCodeStaysMarked: what Claude Code sets in its own tool shells stays on the list. These
// are what catch an agent in an IDE, where the removed variable is set as well.
func TestClaudeCodeStaysMarked(t *testing.T) {
	for _, want := range []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_PLUGIN_ROOT"} {
		if !slices.Contains(Markers, want) {
			t.Errorf("%s must stay a marker", want)
		}
		for _, k := range Markers {
			t.Setenv(k, "")
		}
		t.Setenv(want, "1")
		if m, ok := Set(); !ok || m != want {
			t.Errorf("with %s set: Set() = %q, %v", want, m, ok)
		}
	}
}
