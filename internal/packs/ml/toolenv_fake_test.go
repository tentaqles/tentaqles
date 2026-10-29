package ml

import "github.com/tentaqles/tentaqles/internal/packs/toolrun"

// The fake tools of this package's tests read these variables; toolrun's
// filtered environment passes them to every run in the test binary.
func init() {
	toolrun.PassForTests("TQ_M1_FAKE_ARGS", "TQ_M1_FAKE_EXIT", "TQ_M1_FAKE_OUT", "TQ_M1_FAKE_SLEEP", "TQ_M1_FAKE_TOOL",
		"TQ_M1_FAKE_VERSION", "TQ_ML_FAKE", "TQ_ML_FAKE_EXIT")
}
