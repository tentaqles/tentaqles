package hook

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// codexCallID is the call id the codexExit tests look up.
const codexCallID = "call_exit01"

// codexExitLine is one line of a Codex session file: an item_completed event of a CommandExecution item
// with the given id and exit_code (any value, so a string can be given too). It ends in a line break.
func codexExitLine(t *testing.T, id string, exitCode any) string {
	t.Helper()
	return transcriptLine(t, map[string]any{
		"timestamp": "2026-11-10T10:00:00.000Z",
		"type":      "event_msg",
		"payload": map[string]any{
			"type": "item_completed",
			"item": map[string]any{
				"type": "CommandExecution", "id": id, "command": "make build",
				"exit_code": exitCode, "status": "completed",
			},
		},
	})
}

// codexSessionPath is the path of a session file under login's sessions folder.
func codexSessionPath(login string) string {
	return filepath.Join(login, "sessions", "2026", "11", "10", "rollout-x.jsonl")
}

// TestLobeCodexExitFromSession checks what codexExit reads from a session file: the whole-number code of
// the call's own shell line, the last such line when there are two, and no code in every other case.
func TestLobeCodexExitFromSession(t *testing.T) {
	cases := []struct {
		name    string
		content func(t *testing.T) string
		want    int
		ok      bool
	}{
		{"a line with code 1 gives 1", func(t *testing.T) string {
			return codexExitLine(t, codexCallID, 1)
		}, 1, true},
		{"code 0 gives 0", func(t *testing.T) string {
			return codexExitLine(t, codexCallID, 0)
		}, 0, true},
		{"code -1 gives -1", func(t *testing.T) string {
			return codexExitLine(t, codexCallID, -1)
		}, -1, true},
		{"two lines for one id, the last wins", func(t *testing.T) string {
			return codexExitLine(t, codexCallID, 1) + codexExitLine(t, codexCallID, 0)
		}, 0, true},
		{"no line for the id, no code", func(t *testing.T) string {
			return transcriptLine(t, map[string]any{"type": "response_item", "payload": map[string]any{"type": "message"}})
		}, 0, false},
		{"the only line more than 1 MiB before the end, no code", func(t *testing.T) string {
			s := codexExitLine(t, codexCallID, 1)
			pad := transcriptLine(t, map[string]any{
				"type": "event_msg", "payload": map[string]any{"type": "agent_message", "pad": strings.Repeat("x", 4000)},
			})
			// 300 filler lines of about 4 KiB put the call's line more than 1 MiB from the end.
			return s + strings.Repeat(pad, 300)
		}, 0, false},
		{"a line whose exit_code is a string, no code", func(t *testing.T) string {
			return codexExitLine(t, codexCallID, "1")
		}, 0, false},
		{"a file holding only a line for another id, no code", func(t *testing.T) string {
			return codexExitLine(t, "call_other", 1)
		}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newHome(t)
			login := paths.IdentityDir("acme", "codex")
			p := codexSessionPath(login)
			appendText(t, p, c.content(t))
			got, ok := codexExit(login, p, codexCallID)
			if got != c.want || ok != c.ok {
				t.Errorf("codexExit = (%d, %v), want (%d, %v)", got, ok, c.want, c.ok)
			}
		})
	}
}
