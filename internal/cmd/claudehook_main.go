package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// RunMain is the tq binary's entry point and returns its exit code.
//
// For `tentaqles claude-hook pre-tool-use` and `tentaqles codex-hook pre-tool-use` it guarantees
// an answer of allow or deny and nothing else, whatever goes wrong outside the decision itself:
// the harness runs a call whose hook exits with any other status, or that it kills at its own
// hook timeout. So a flag tq does not know, an error from the command, and a process that is
// still running hookWatchdog after it started (the decision's own deadline is hookDeadline) all
// answer a deny, in the form the harness reads (see hookDialect). The plugin's tq_hook.sh adds
// its own watchdog on top.
func RunMain(args []string, stdout, stderr io.Writer) int {
	d, pre := hookInvocation(args)
	if pre {
		if d.codex {
			stdout = &answerOnce{w: stdout}
		}
		t := time.AfterFunc(hookWatchdog, func() {
			hookExit(hookStartupDeny(stdout, stderr, d, fmt.Sprintf("tq did not answer within %s", hookWatchdog)))
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
			return hookStartupDeny(stdout, stderr, d, "tq could not run the hook ("+err.Error()+")")
		}
		fmt.Fprintln(stderr, "tq:", err)
		return 1
	}
	return 0
}

// answerOnce is the stdout of a codex-hook run. The decision and the watchdog can both answer on
// it, and Codex reads two JSON objects as an invalid answer (an allow), so the first write wins and
// every later write is dropped.
type answerOnce struct {
	mu    sync.Mutex
	w     io.Writer
	taken bool
}

func (a *answerOnce) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.taken {
		return len(p), nil
	}
	a.taken = true
	return a.w.Write(p)
}

// hookWatchdog is the process-level backstop of a pre-tool-use run: one
// second over hookDeadline. Tests lower it.
var hookWatchdog = hookDeadline + time.Second

// hookStartTestHook runs on the main goroutine right after the watchdog is armed when a test
// sets it (startup that never reaches the decision). Nil in tq.
var hookStartTestHook func()

// hookExit ends the process from the watchdog; tests replace it.
var hookExit = os.Exit

// hookInvocation reports whether args run a pre-tool-use hook, and which harness it serves.
func hookInvocation(args []string) (hookDialect, bool) {
	var pos []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
		}
	}
	if len(pos) < 2 || pos[1] != "pre-tool-use" {
		return hookDialect{}, false
	}
	switch pos[0] {
	case "claude-hook":
		return hookDialect{}, true
	case "codex-hook":
		return codexDialect, true
	}
	return hookDialect{}, false
}

// hookStartupDeny is the deny of a pre-tool-use run that never reached a decision (and of every
// call tq cannot judge): the reason on stderr with exit 2 for Claude Code, the Codex deny object
// on stdout with exit 0 for Codex.
func hookStartupDeny(stdout, stderr io.Writer, d hookDialect, why string) int {
	reason := "BLOCKED [" + guardErrorID + "]: " + why + "; tq denies a call it cannot judge. Report this to the developer instead of retrying; `tentaqles doctor` shows what is wrong"
	emitDecision(stdout, stderr, d, true, guardErrorID, reason)
	return d.denyCode()
}
