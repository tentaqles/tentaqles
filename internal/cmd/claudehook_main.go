package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// RunMain is the tq binary's entry point and returns its exit code.
//
// For `tq claude-hook pre-tool-use` it guarantees an answer of 0 (allow) or
// 2 (deny) and nothing else, whatever goes wrong
// outside the decision itself: Claude Code runs a call whose hook
// exits with any other status, or that they kill at their own hook timeout.
// So a flag tq does not know, an error from the command, and a process that
// is still running hookWatchdog after it started (the decision's own
// deadline is hookDeadline) all answer a deny. The plugin's tq_hook.sh adds
// its own watchdog on top.
func RunMain(args []string, stdout, stderr io.Writer) int {
	pre := hookInvocation(args)
	if pre {
		t := time.AfterFunc(hookWatchdog, func() {
			hookExit(hookStartupDeny(stderr, fmt.Sprintf("tq did not answer within %s", hookWatchdog)))
		})
		defer t.Stop()
		if hookStartTestHook != nil {
			hookStartTestHook()
		}
	}
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		if pre {
			return hookStartupDeny(stderr, "tq could not run the hook ("+err.Error()+")")
		}
		fmt.Fprintln(stderr, "tq:", err)
		return 1
	}
	return 0
}

// hookWatchdog is the process-level backstop of a pre-tool-use run: one
// second over hookDeadline. Tests lower it.
var hookWatchdog = hookDeadline + time.Second

// hookStartTestHook runs on the main goroutine right after the watchdog is armed when a test
// sets it (startup that never reaches the decision). Nil in tq.
var hookStartTestHook func()

// hookExit ends the process from the watchdog; tests replace it.
var hookExit = os.Exit

// hookInvocation reports whether args run `claude-hook pre-tool-use`.
func hookInvocation(args []string) bool {
	var pos []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	return len(pos) >= 2 && pos[0] == "claude-hook" && pos[1] == "pre-tool-use"
}

// hookStartupDeny is the deny of a pre-tool-use run that never reached a decision: the reason on
// stderr, exit code 2.
func hookStartupDeny(stderr io.Writer, why string) int {
	fmt.Fprintln(stderr, "BLOCKED ["+guardErrorID+"]: "+why+"; tq denies a call it cannot judge. Report this to the developer instead of retrying; `tq doctor` shows what is wrong")
	return 2
}
