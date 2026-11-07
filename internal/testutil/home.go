package testutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// HomeRun runs a package's tests (m.Run) with TQ_HOME, HOME and USERPROFILE all pointing into one
// temp folder, and removes the folder after. A package's TestMain calls it:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
//
// Without it a test that never sets TQ_HOME reaches the developer's ~/.tentaqles (paths.Home falls
// back to the user's home folder), and a test that sets TQ_HOME under an agent marker is ignored
// there unless the package allows the override (paths.AllowHomeOverride). The variables set here
// are the run's; a test that needs another home sets it with t.Setenv, as before.
func HomeRun(m interface{ Run() int }) int {
	dir, err := os.MkdirTemp("", "tq-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testutil: no temp folder for the test home:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "testutil: no temp home:", err)
		return 1
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	return m.Run()
}
