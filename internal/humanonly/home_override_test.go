package humanonly

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The tests set harness markers on purpose and often run under Claude Code, whose agent marker
// makes paths.Home ignore TQ_HOME (ADR-0002); allow the override for the test binary
// (paths.AllowHomeOverride, set nowhere else). TestMain points TQ_HOME, HOME and USERPROFILE at
// one temp folder for the whole run (testutil.HomeRun), so a denial log never lands in the
// developer's real ~/.tentaqles.
func init() { paths.AllowHomeOverride = true }

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
