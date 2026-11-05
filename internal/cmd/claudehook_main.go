package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/audit"
)

// RunMain is the tq binary's entry point and returns its exit code.
//
// For `tq claude-hook pre-tool-use` it guarantees an answer of 0 (allow) or
// 2 (deny) and nothing else (PRD v2 §6.3, ADR-0006), whatever goes wrong
// outside the decision itself: Claude Code and Codex run a call whose hook
// exits with any other status, or that they kill at their own hook timeout.
// So a flag tq does not know, an error from the command, and a process that
// is still running hookWatchdog after it started (the decision's own
// deadline is hookDeadline) all answer a deny. This is the guarantee the
// Codex hook (a bare `tq claude-hook pre-tool-use --agent codex`) gets; the
// Claude plugin's tq_hook.sh adds its own watchdog on top.
func RunMain(args []string, stdout, stderr io.Writer) int {
	pre, codex := hookInvocation(args)
	if pre {
		t := time.AfterFunc(hookWatchdog, func() {
			hookExit(hookStartupDeny(stdout, stderr, codex, fmt.Sprintf("tq did not answer within %s", hookWatchdog)))
		})
		defer t.Stop()
	}
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		if pre {
			return hookStartupDeny(stdout, stderr, codex, "tq could not run the hook ("+err.Error()+")")
		}
		fmt.Fprintln(stderr, "tq:", err)
		return 1
	}
	return 0
}

// hookWatchdog is the process-level backstop of a pre-tool-use run: one
// second over hookDeadline. Tests lower it.
var hookWatchdog = hookDeadline + time.Second

// hookExit ends the process from the watchdog; tests replace it.
var hookExit = os.Exit

// hookInvocation reports whether args run `claude-hook pre-tool-use`, and
// whether --agent codex is among them.
func hookInvocation(args []string) (pre, codex bool) {
	var pos []string
	for i, a := range args {
		switch {
		case a == "--agent" && i+1 < len(args):
			codex = codex || args[i+1] == hookAgentCodex
		case a == "--agent="+hookAgentCodex:
			codex = true
		case !strings.HasPrefix(a, "-"):
			pos = append(pos, a)
		}
	}
	pre = len(pos) >= 2 && pos[0] == "claude-hook" && pos[1] == "pre-tool-use"
	return pre, codex
}

// hookStartupDeny is the deny of a pre-tool-use run that never reached a
// decision: the reason on stderr, the Codex deny JSON under --agent codex,
// one line in the denial log, exit code 2.
func hookStartupDeny(stdout, stderr io.Writer, codex bool, why string) int {
	reason := "BLOCKED [" + guardErrorID + "]: " + why + "; tq denies a call it cannot judge. Report this to the developer instead of retrying; `tq doctor` shows what is wrong"
	harness := hookAgentClaude
	if codex {
		harness = hookAgentCodex
		writeCodexDeny(stdout, reason)
	}
	fmt.Fprintln(stderr, reason)
	cwd, _ := os.Getwd()
	_ = audit.AppendDenial(audit.Denial{Category: guardErrorID, Layer: audit.LayerHook, Caller: audit.CallerHarness,
		Harness: harness, Rule: guardErrorID, Reason: reason, PID: os.Getpid(), Cwd: cwd})
	return 2
}
