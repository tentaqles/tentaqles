package cmd

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
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
	for _, k := range humanonly.Markers {
		_ = os.Unsetenv(k)
	}
	humanonly.IsTerminal = func(*os.File) bool { return true }
	humanonly.ParentOf = terminalChain
	humanonly.Confirm = func(string) (bool, error) { return true, nil }
	ensureProtectedHome = func() error { return os.MkdirAll(paths.Home(), 0o700) }
	paths.AllowHomeOverride = true
	// The test binary is not tq: the codex launch tests do not look for a tq
	// on PATH (TestCodexHookResolves tests the real check).
	realCodexHookResolves = codexHookResolves
	codexHookResolves = func([]string) error { return nil }
	os.Exit(m.Run())
}
