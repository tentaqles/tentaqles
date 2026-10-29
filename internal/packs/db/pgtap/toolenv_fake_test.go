package pgtap

import "github.com/tentaqles/tentaqles/internal/packs/toolrun"

// The fake pg_prove and supabase read these variables; toolrun's filtered
// environment passes them to every run in this test binary. Without them a
// copy of the test binary would run the test suite again.
func init() {
	toolrun.PassForTests("TQ_FAKE_PGTAP", "TQ_FAKE_ARGV", "TQ_FAKE_ENV", "TQ_FAKE_STARTED",
		"TQ_FAKE_OUT", "TQ_FAKE_EXIT", "TQ_FAKE_ECHO_SECRET")
}
