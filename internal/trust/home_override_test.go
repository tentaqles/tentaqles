package trust

import "github.com/tentaqles/tentaqles/internal/paths"

// The tests point TQ_HOME at temp dirs and often run under Claude Code, whose
// agent marker makes paths.Home ignore TQ_HOME; allow the override for the
// test binary (paths.AllowHomeOverride, set nowhere else). For the same
// reason the human-only gate of a signature would refuse every test: it
// passes here, and TestSigningRefusesWithoutAHuman pins the refusal.
func init() {
	paths.AllowHomeOverride = true
	HumanCheck = func(string) error { return nil }
}
