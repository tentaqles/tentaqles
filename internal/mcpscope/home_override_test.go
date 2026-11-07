package mcpscope

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// Test-only setup for this package (ADR-0017 round 3: no rule code here changes). The tests run
// under Claude Code, whose agent marker makes paths.Home ignore TQ_HOME, so the override is allowed
// for the test binary (paths.AllowHomeOverride). TestMain points TQ_HOME, HOME and USERPROFILE at
// one temp folder for the whole run (testutil.HomeRun), so a test that never sets TQ_HOME cannot
// reach the developer's ~/.tentaqles.
func init() { paths.AllowHomeOverride = true }

func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
