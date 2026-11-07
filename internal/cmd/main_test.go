package cmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/termopen"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// terminalChain is a developer's process tree as the walker would read it:
// this process <- pwsh <- WindowsTerminal.exe <- explorer.exe <- (exited).
func terminalChain(pid int) (humanonly.Parent, bool) {
	switch pid {
	case os.Getpid():
		return humanonly.Parent{PID: 90, Name: "pwsh.exe", Cmdline: "pwsh -NoLogo", CmdlineKnown: true}, true
	case 90:
		return humanonly.Parent{PID: 80, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}, true
	case 80:
		return humanonly.Parent{PID: 70, Name: "explorer.exe", Cmdline: "explorer", CmdlineKnown: true}, true
	}
	return humanonly.Parent{}, false
}

// TestMain starts the suite from a human baseline: the test binary may itself
// run under a harness (go test inside a Claude Code session, with its markers
// in the environment, claude.exe in the process tree and no terminal on
// stdin), which would refuse every human-only command before a test could
// decide. Each detector has a seam; a test that exercises one replaces it and
// restores it, and the markers are set per test with t.Setenv.
//
// It also replaces the one icacls run (internal/protect) with a plain mkdir:
// tq init, tq add and tq migrate manifests --apply all create $TQ_HOME, and
// no test may change a real folder's ACL (protect's own tests cover icacls
// with a fake). And it lets TQ_HOME apply under a marker (paths.Home ignores
// it otherwise, ADR-0002): every test points TQ_HOME at a temp dir and some
// set a marker on purpose.
func TestMain(m *testing.M) {
	// The test binary is the stub program of the PATH masks the tests write (ADR-0008).
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	// A fake browser (open_test.go): run as a copy of the test binary named chrome-fake or
	// msedge-fake, it records its name and argv and exits.
	if isFakeBrowser() {
		recordFakeBrowser()
		os.Exit(0)
	}
	// The browser shims the tests write are copies of this binary (ADR-0016).
	if ok, code := ShimMain(os.Stderr, os.Args[1:]); ok {
		os.Exit(code)
	}
	// And the fake package manager and fake CLI of the install tests (testutil.NewFakeProgram).
	if ok, code := testutil.FakeMain(); ok {
		os.Exit(code)
	}
	// And tq itself for a test that runs a real shell hook (TQ_TEST_BE_TQ=1): the arguments
	// are tq's.
	if os.Getenv("TQ_TEST_BE_TQ") == "1" {
		paths.AllowHomeOverride = true
		root := NewRoot()
		root.SetArgs(os.Args[1:])
		if err := root.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, "tq:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	for _, k := range humanonly.Markers {
		_ = os.Unsetenv(k)
	}
	humanonly.IsTerminal = func(*os.File) bool { return true }
	humanonly.ParentOf = terminalChain
	humanonly.Confirm = func(string) (bool, error) { return true, nil }
	ensureProtectedHome = func() error { return os.MkdirAll(paths.Home(), 0o700) }
	// reach's terminal seams never open or end a real terminal under test: a test that checks the
	// handover swaps in its own fakes (isolateHandover), and no shell profile is detected here.
	reachOpenTerminal = func(dir string) (termopen.Launch, error) {
		return termopen.Launch{Name: "test-terminal", Dir: dir}, nil
	}
	reachKill = func(int) error { return nil }
	reachDetectShells = func(hooks.Profiles) []hooks.Shell { return nil }
	// No test reads a real stdin: a closed pipe reads as the end of input, which is an Enter at reach's pause.
	if r, w, err := os.Pipe(); err == nil {
		_ = w.Close()
		os.Stdin = r
	}
	paths.AllowHomeOverride = true
	envplan.StubSource = os.Executable // the mask stubs the tests run are this binary
	// The run's own TQ_HOME, HOME and USERPROFILE (testutil.HomeRun): a test that never sets
	// TQ_HOME must not reach the developer's ~/.tentaqles.
	os.Exit(testutil.HomeRun(m))
}
