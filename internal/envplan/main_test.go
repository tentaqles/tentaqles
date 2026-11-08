package envplan

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The test binary is the stub program of the PATH masks the tests write (ADR-0008): run as
// path-mask/aws.exe it answers as the stub, as tq does.
func TestMain(m *testing.M) {
	if ok, code := StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	_ = os.Unsetenv("CLAUDECODE")
	StubSource = os.Executable // the stubs the tests run are this binary (StubMain above)
	// The run's own TQ_HOME, HOME and USERPROFILE (testutil.HomeRun): a test that never sets
	// TQ_HOME must not reach the developer's ~/.tentaqles.
	os.Exit(testutil.HomeRun(m))
}
