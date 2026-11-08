package cmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// TestMain sets the suite up: the test binary can stand in for the programs the tests run (a PATH
// mask stub, a browser shim, a fake browser, a fake program, tq itself), no shell profile is
// detected, stdin is closed, CLAUDECODE is unset (the suite may run inside a Claude Code session),
// and TQ_HOME, HOME and USERPROFILE point at one temp folder (testutil.HomeRun).
func TestMain(m *testing.M) {
	// The test binary is the stub program of the PATH masks the tests write.
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	// A fake browser (open_test.go): run as a copy of the test binary named chrome-fake or
	// msedge-fake, it records its name and argv and exits.
	if isFakeBrowser() {
		recordFakeBrowser()
		os.Exit(0)
	}
	// The browser shims the tests write are copies of this binary.
	if ok, code := ShimMain(os.Stderr, os.Args[1:]); ok {
		os.Exit(code)
	}
	// The fake programs of testutil.NewFakeProgram.
	if ok, code := testutil.FakeMain(); ok {
		os.Exit(code)
	}
	// tq itself, for a test that runs a real shell hook (TQ_TEST_BE_TQ=1).
	if os.Getenv("TQ_TEST_BE_TQ") == "1" {
		root := NewRoot()
		root.SetArgs(os.Args[1:])
		if err := root.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, "tq:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	_ = os.Unsetenv("CLAUDECODE")
	reachDetectShells = func(hooks.Profiles) []hooks.Shell { return nil }
	if r, w, err := os.Pipe(); err == nil {
		_ = w.Close()
		os.Stdin = r
	}
	envplan.StubSource = os.Executable // the mask stubs the tests run are this binary
	os.Exit(testutil.HomeRun(m))
}
