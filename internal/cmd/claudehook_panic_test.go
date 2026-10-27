package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The commands that crashed the hook in a real session: several heredocs on
// one line, a body naming another marker, a heredoc inside $( ).
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

// Any panic in the guard's analysis asks: never allow, never crash, never
// the unattended skip.
func TestPreToolUseGuardPanicAsks(t *testing.T) {
	isolateHome(t)
	prev := hookPolicyDecision
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		var s []int
		_ = s[3] // index out of range
		return policy.Decision{}
	}
	t.Cleanup(func() { hookPolicyDecision = prev })
	dir := testutil.TempDir(t)

	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "ls"))
	if code != 0 {
		t.Fatalf("code=%d, want 0 with an ask", code)
	}
	if r := askReason(t, out); !strings.Contains(r, "tq guard error; please review") || !strings.Contains(r, "tq/guard-error") {
		t.Errorf("reason = %q", r)
	}

	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, dir, "ls"))
	if code != 0 || !strings.Contains(out, `"rule":"tq/guard-error"`) || !strings.Contains(out, `"block":false`) {
		t.Errorf("--json: code=%d out=%q", code, out)
	}
}
