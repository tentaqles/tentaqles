package envplan

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/markers"
)

// The test binary is the stub program of the PATH masks the tests write (ADR-0008): run as
// path-mask/aws.exe it answers as the stub, as tq does.
func TestMain(m *testing.M) {
	if ok, code := StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	// A human terminal: the test binary may run under a harness, whose markers change what a
	// missing state means (lost.go).
	for _, k := range markers.Markers {
		_ = os.Unsetenv(k)
	}
	StubSource = os.Executable // the stubs the tests run are this binary (StubMain above)
	os.Exit(m.Run())
}
