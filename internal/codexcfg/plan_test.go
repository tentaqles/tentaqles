package codexcfg

import (
	"strings"
	"testing"
)

// The launcher starts Codex with its full-bypass flag (PRD v2 §7.2, the
// owner's decision of 2026-11-05), after a session subcommand that takes it
// (codex 0.154: exec, e, resume, fork), and never sets how Codex asks: a
// flag the developer typed that says how Codex should ask or sandbox, glued
// forms included, wins and the line goes through as typed.
func TestLaunchArgsInjectsTheBypassFlag(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{nil, BypassFlag},
		{[]string{"hi"}, BypassFlag + " hi"},
		{[]string{"exec", "hi"}, "exec " + BypassFlag + " hi"},
		{[]string{"e", "hi"}, "e " + BypassFlag + " hi"},
		{[]string{"resume", "--last"}, "resume " + BypassFlag + " --last"},
		{[]string{"fork", "--last"}, "fork " + BypassFlag + " --last"},
		{[]string{"--model", "x", "exec", "hi"}, "--model x exec " + BypassFlag + " hi"},
		{[]string{"-m", "exec", "hi"}, BypassFlag + " -m exec hi"}, // exec is -m's value; hi is the prompt
		{[]string{"--", "-a"}, BypassFlag + " -- -a"},
		{[]string{"--full-auto"}, BypassFlag + " --full-auto"}, // not a 0.154 flag: codex says so itself
		// The developer's own choice passes through unchanged.
		{[]string{"-a", "never", "hi"}, "-a never hi"},
		{[]string{"-anever", "hi"}, "-anever hi"},
		{[]string{"-a=never"}, "-a=never"},
		{[]string{"--ask-for-approval=on-request"}, "--ask-for-approval=on-request"},
		{[]string{"-s", "read-only"}, "-s read-only"},
		{[]string{"-sread-only"}, "-sread-only"},
		{[]string{"exec", "--sandbox=read-only", "x"}, "exec --sandbox=read-only x"},
		{[]string{"-c", `sandbox_mode="read-only"`}, `-c sandbox_mode="read-only"`},
		{[]string{`-capproval_policy="never"`}, `-capproval_policy="never"`},
		{[]string{`-c=approval_policy="never"`}, `-c=approval_policy="never"`},
		{[]string{"--config=approval_policy=\"never\""}, "--config=approval_policy=\"never\""},
		{[]string{BypassFlag, "hi"}, BypassFlag + " hi"},
		{[]string{"--yolo"}, "--yolo"},
		// A -c that is about something else adds the flag.
		{[]string{"-cmodel=o3", "hi"}, BypassFlag + " -cmodel=o3 hi"},
	} {
		if got := strings.Join(LaunchArgs(c.in), " "); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}
