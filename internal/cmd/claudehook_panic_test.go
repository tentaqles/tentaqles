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

// Any panic in the guard's analysis denies: never allow, never ask, never
// crash (PRD v2 §6.3, fail closed).
func TestPreToolUseGuardPanicDenies(t *testing.T) {
	isolateHome(t)
	prev := hookPolicyDecision
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		var s []int
		_ = s[3] // index out of range
		return policy.Decision{}
	}
	t.Cleanup(func() { hookPolicyDecision = prev })
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

// End to end: a data heredoc that mentions `set` passes the env-dump rule; a
// heredoc that feeds `set` to a shell is still denied.
func TestPreToolUseHeredocDataRules(t *testing.T) {
	isolateHome(t)
	dir := testutil.TempDir(t)
	for _, cmd := range []string{
		"cat > notes.md <<'EOF'\nRun the tests.\n\nset\nEOF",
		"git commit -m \"$(cat <<'EOF'\nfix: tester notes\n\nset\nEOF\n)\"",
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, cmd))
		if code == 2 && strings.Contains(errOut, "tq/env-dump") {
			t.Errorf("%q: denied: %q", cmd, errOut)
		}
	}
	for cmd, rule := range map[string]string{
		"bash <<'EOF'\nset\nEOF":                     "tq/env-dump",
		"echo \"$(cat <<'EOF'\nset\nEOF\n)\" > n.md": "tq/env-dump",
		"cat <<EOF | sudo -u root bash\nset\nEOF":    "tq/env-dump",
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, cmd))
		if code != 2 || !strings.Contains(errOut, rule) {
			t.Errorf("%q: code=%d err=%q, want %s", cmd, code, errOut, rule)
		}
	}
}
