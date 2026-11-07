package trust

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The tests run under Claude Code, whose agent marker makes paths.Home ignore TQ_HOME, so the
// override is allowed for the test binary (paths.AllowHomeOverride, set nowhere else). For the same
// reason the human-only gate of a signature would refuse every test: it passes here, and
// TestSigningRefusesWithoutAHuman pins the refusal. TestMain points TQ_HOME, HOME and USERPROFILE
// at one temp folder for the whole run (testutil.HomeRun).
func init() {
	paths.AllowHomeOverride = true
	HumanCheck = func(string) error { return nil }
}

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
