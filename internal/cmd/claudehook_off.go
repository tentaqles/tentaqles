package cmd

import (
	"os"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
)

// Owned by WS2 (spec E §8.4, §17).

// envOff reports whether tq is off for this process: TQ_MODE=off (any case,
// surrounding space ignored) or TQ_ENABLED=0. Both come from tq's own env,
// which the hook inherits from the agent process at its launch; a tool call's
// command line never reaches it.
func envOff() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(guardcfg.EnvMode)), guardcfg.ModeOff) {
		return true
	}
	return strings.TrimSpace(os.Getenv("TQ_ENABLED")) == "0"
}

// hookOff reports whether tq is off for this process (envOff). Every
// `tq claude-hook <event>` calls it first (newClaudeHookCmd wraps each
// subcommand) and exits 0 with no output when it is true, before reading
// stdin.
func hookOff() bool { return envOff() }
