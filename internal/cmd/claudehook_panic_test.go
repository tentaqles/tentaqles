package cmd

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The commands that crashed the hook in a real session: several heredocs on one line, a body
// naming another marker, a heredoc inside $( ).
var panicCommands = []string{
	"git status && cat <<A <<B >/tmp/acme-notes-with-a-long-name.txt\na\nA\nb\nB",
	"git add x && git commit -F - <<A <<B\nshort\nA\nb\nB",
	"git commit -m \"$(cat <<'EOF'\nfix: use <<EOF and <<A <<B in docs\nEOF\n)\"",
	"cat <<A <<B\na",
}

func TestPreToolUseHeredocsDoNotCrash(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	for _, cmd := range panicCommands {
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, cmd))
		if strings.Contains(errOut, "panic") || strings.Contains(errOut, "goroutine") {
			t.Errorf("%q: crashed: code=%d out=%q err=%q", cmd, code, out, errOut)
		}
	}
}

// A panic in the check denies: never allow, never crash.
func TestPreToolUseGuardPanicDenies(t *testing.T) {
	isolateHome(t)
	prev := hookGuardDecide
	hookGuardDecide = func(guard.Input) guard.Decision {
		var s []int
		_ = s[3] // index out of range
		return guard.Decision{}
	}
	t.Cleanup(func() { hookGuardDecide = prev })
	dir := testutil.TempDir(t)

	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "ls"))
	if code != 2 || out != "" || !strings.Contains(errOut, "BLOCKED [tq/guard-error]: tq guard error") || strings.Contains(errOut, "goroutine") {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, dir, "ls"))
	if code != 2 || !strings.Contains(out, `"rule":"tq/guard-error"`) || !strings.Contains(out, `"block":true`) {
		t.Errorf("--json: code=%d out=%q", code, out)
	}
}
