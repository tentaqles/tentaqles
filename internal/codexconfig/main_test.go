package codexconfig

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// TestMain points HOME, USERPROFILE and TQ_HOME at one temp folder, so no test reads or
// writes the developer's own ~/.tentaqles or ~/.codex.
func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
