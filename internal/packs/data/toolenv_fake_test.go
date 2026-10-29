package data

import "github.com/tentaqles/tentaqles/internal/packs/toolrun"

// The fake tools of this package's tests read these variables; toolrun's
// filtered environment passes them to every run in the test binary.
func init() {
	toolrun.PassForTests("TQ_FAKE_ARGS", "TQ_FAKE_DC_EXIT", "TQ_FAKE_DC_FAIL_ON", "TQ_FAKE_DC_LOG", "TQ_FAKE_DC_OUT",
		"TQ_FAKE_DC_SLEEP", "TQ_FAKE_EXIT", "TQ_FAKE_STDERR", "TQ_FAKE_STDOUT", "TQ_FAKE_TOOL", "TQ_FAKE_VERSION",
		"TQ_COST_FAKE_DIR", "TQ_CONTRACT_UPDATE", "FAKE_DAGSTER_ARGS", "FAKE_DAGSTER_EXIT", "FAKE_DAGSTER_OUT", "FAKE_DAGSTER_SLEEP")
}
