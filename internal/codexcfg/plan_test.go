package codexcfg

import (
	"strings"
	"testing"
)

// The launcher starts Codex with its full-bypass flag (PRD v2 §7.2, the
// owner's decision of 2026-11-05) and never sets how Codex asks: a flag the
// developer typed that says how Codex should ask or sandbox wins, and the
// line goes through as typed.
func TestLaunchArgsInjectsTheBypassFlag(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{nil, BypassFlag},
		{[]string{"hi"}, BypassFlag + " hi"},
		{[]string{"exec", "hi"}, BypassFlag + " exec hi"},
		{[]string{"--model", "x", "exec", "hi"}, BypassFlag + " --model x exec hi"},
		{[]string{"--", "-a"}, BypassFlag + " -- -a"},
		// The developer's own choice passes through unchanged.
		{[]string{"-a", "never", "hi"}, "-a never hi"},
		{[]string{"--ask-for-approval=on-request"}, "--ask-for-approval=on-request"},
		{[]string{"-s", "read-only"}, "-s read-only"},
		{[]string{"--sandbox=read-only"}, "--sandbox=read-only"},
		{[]string{"--full-auto"}, "--full-auto"},
		{[]string{"-c", `sandbox_mode="read-only"`}, `-c sandbox_mode="read-only"`},
		{[]string{"--config=approval_policy=\"never\""}, "--config=approval_policy=\"never\""},
		{[]string{BypassFlag, "hi"}, BypassFlag + " hi"},
		{[]string{"--yolo"}, "--yolo"},
	} {
		if got := strings.Join(LaunchArgs(c.in), " "); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}
