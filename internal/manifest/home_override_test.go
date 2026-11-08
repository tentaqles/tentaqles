package manifest

import (
	"os"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// TestMain points TQ_HOME, HOME and USERPROFILE at one temp folder for the whole run, so no test
// reads or writes the developer's own ~/.tentaqles.
func TestMain(m *testing.M) { os.Exit(testutil.HomeRun(m)) }
