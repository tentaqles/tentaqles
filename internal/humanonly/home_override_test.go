package humanonly

import "github.com/tentaqles/tentaqles/internal/paths"

// The tests point TQ_HOME at temp dirs, set harness markers on purpose and
// often run under Claude Code, whose agent marker makes paths.Home ignore
// TQ_HOME (ADR-0002); allow the override for the test binary
// (paths.AllowHomeOverride, set nowhere else), so a denial log never lands in
// the developer's real ~/.tentaqles.
func init() { paths.AllowHomeOverride = true }
