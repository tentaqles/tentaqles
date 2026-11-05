package cmd

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// TestMain replaces the one icacls run (internal/protect) with a plain
// mkdir for every test in the package: tq init, tq add and tq migrate
// manifests --apply all create $TQ_HOME, and no test may change a real
// folder's ACL. protect's own tests cover the icacls path with a fake.
func TestMain(m *testing.M) {
	ensureProtectedHome = func() error { return os.MkdirAll(paths.Home(), 0o700) }
	os.Exit(m.Run())
}

// stubProtectedHome counts the seam's calls for one test.
func stubProtectedHome(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := ensureProtectedHome
	ensureProtectedHome = func() error {
		calls++
		return os.MkdirAll(paths.Home(), 0o700)
	}
	t.Cleanup(func() { ensureProtectedHome = prev })
	return &calls
}
