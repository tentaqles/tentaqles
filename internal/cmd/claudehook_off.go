package cmd

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
)

// Owned by WS2 (spec E §8.4, §17).

// envOff reports whether tq is off for this process: TQ_MODE is exactly off
// (guardcfg.IsOff, the one matcher every tq part uses). It comes from tq's
// own env, which the hook inherits from the agent process at its launch; a
// tool call's command line never reaches it. TQ_ENABLED=0 is not off here:
// it only skips shell activation (security review m3), and the off launch
// sets TQ_MODE=off.
func envOff() bool { return guardcfg.IsOff(os.Getenv(guardcfg.EnvMode)) }

// hookOff reports whether tq is off for this process (envOff). Every
// `tq claude-hook <event>` calls it first (newClaudeHookCmd wraps each
// subcommand) and exits 0 with no output when it is true, before reading
// stdin.
func hookOff() bool { return envOff() }
