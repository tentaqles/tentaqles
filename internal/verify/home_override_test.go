package verify

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The tests run under Claude Code, whose agent marker makes paths.Home ignore TQ_HOME, so the
// override is allowed for the test binary (paths.AllowHomeOverride, set nowhere else). TestMain
// points TQ_HOME, HOME and USERPROFILE at one temp folder for the whole run (testutil.HomeRun): the
// catalog merges the user's provider files from paths.Home, so a test that never sets TQ_HOME would
// read the developer's own ~/.tentaqles/providers.
func init() { paths.AllowHomeOverride = true }

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
