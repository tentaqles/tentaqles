package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// spin keeps the injected loops running for the whole child process.
var spin atomic.Bool

// spinChild is the child half of TestHookStall_*: it runs the real RunMain of
// `tq claude-hook pre-tool-use` with a tight loop injected (no calls, no allocation, so only
// an asynchronous preemption can stop it) and a goroutine forcing garbage collections, which
// stop the world and so must preempt the loop, then exits with RunMain's code or the
// watchdog's.
func spinChild(where string) {
	spin.Store(true)
	loop := func() {
		var x uint64
		for spin.Load() {
			x++
		}
		_ = x
	}
	switch where {
	case "decision":
		hookDecideTestHook = loop
	case "startup":
		hookStartTestHook = loop
	}
	hookDeadline = 1 * time.Second
	hookWatchdog = 2 * time.Second
	go func() {
		for {
			runtime.GC()
			_ = make([]byte, 1<<20)
		}
	}()
	os.Exit(RunMain([]string{"claude-hook", "pre-tool-use", "--json"}, os.Stdout, os.Stderr))
}

// runSpinChild re-runs this test binary as the child and returns its exit code, stderr and
// how long it took. A child that does not end in 15 s is killed and fails the test.
func runSpinChild(t *testing.T, test, where string) (int, string, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+test+"$", "-test.count=1")
	c.Env = append(os.Environ(), "TQ_TEST_SPIN_CHILD="+where, "TQ_HOME="+t.TempDir())
	c.Stdin = strings.NewReader(`{"tool_name":"Bash","cwd":"` + strings.ReplaceAll(t.TempDir(), `\`, `\\`) + `","tool_input":{"command":"git status"}}`)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	start := time.Now()
	err := c.Run()
	took := time.Since(start)
	if ctx.Err() != nil {
		t.Fatalf("the hook did not end within 15 s of a spinning %s (stderr %q)", where, stderr.String())
	}
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, stderr.String(), took
}

// TestHookStall_SpinningDecisionIsDenied: a decision stuck in a tight loop is answered by the
// hook deadline with a deny (exit 2, tq/guard-error), on time, with garbage collections
// running (a stop-the-world must preempt the loop).
func TestHookStall_SpinningDecisionIsDenied(t *testing.T) {
	if w := os.Getenv("TQ_TEST_SPIN_CHILD"); w != "" {
		spinChild(w)
		return
	}
	code, stderr, took := runSpinChild(t, "TestHookStall_SpinningDecisionIsDenied", "decision")
	if code != 2 || !strings.Contains(stderr, "BLOCKED [tq/guard-error]") || !strings.Contains(stderr, "did not finish within") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if took > 5*time.Second {
		t.Fatalf("the deny took %s, the deadline is 1 s", took)
	}
}

// TestHookStall_WatchdogEndsAStuckMainGoroutine: when the main goroutine itself is stuck in a
// tight loop before any decision (so the hook deadline never runs), the RunMain watchdog
// still denies and ends the process.
func TestHookStall_WatchdogEndsAStuckMainGoroutine(t *testing.T) {
	if w := os.Getenv("TQ_TEST_SPIN_CHILD"); w != "" {
		spinChild(w)
		return
	}
	code, stderr, took := runSpinChild(t, "TestHookStall_WatchdogEndsAStuckMainGoroutine", "startup")
	if code != 2 || !strings.Contains(stderr, "BLOCKED [tq/guard-error]") || !strings.Contains(stderr, "did not answer within") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if took > 6*time.Second {
		t.Fatalf("the watchdog deny took %s, the watchdog is 2 s", took)
	}
}
