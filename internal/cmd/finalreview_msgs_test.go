package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Final review: security m3 (pack approvals are answered at the terminal
// only, never on the board) and correctness m6 (tq pack baseline in an agent
// session refuses with the packs message, not the hub's "or on the hub page").
func TestHumanOnlyMessages(t *testing.T) {
	if strings.Contains(errPackAnswerTTY.Error(), "board in your browser") {
		t.Fatalf("the TTY refusal offers the board: %v", errPackAnswerTTY)
	}
	if msg := baselineAgentErr().Error(); strings.Contains(msg, "hub page") || !strings.Contains(msg, "your own terminal") {
		t.Fatalf("baseline refusal: %q", msg)
	}
	if !errors.Is(baselineAgentErr(), packs.ErrHumanOnly) {
		t.Fatal("not packs.ErrHumanOnly")
	}
}
