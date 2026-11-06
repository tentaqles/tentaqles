package resolve

import "github.com/tentaqles/tentaqles/internal/paths"

// The tests point TQ_HOME at temp dirs and often run under Claude Code, whose
// agent marker makes paths.Home ignore TQ_HOME; allow the override for the
// test binary (paths.AllowHomeOverride, set nowhere else).
func init() { paths.AllowHomeOverride = true }
