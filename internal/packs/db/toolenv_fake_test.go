package db

import "github.com/tentaqles/tentaqles/internal/packs/toolrun"

// The fake tools of this package's tests read these variables; toolrun's
// filtered environment passes them to every run in the test binary.
func init() {
	toolrun.PassForTests("TQ_FAKE_ARGV", "TQ_FAKE_DBTOOL", "TQ_FAKE_SEEN", "TQ_FAKE_VERSION")
}
