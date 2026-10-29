package cmd

// Owned by WS2 (spec E §8.4, §17).

// hookOff reports whether tq is off for this process: TQ_MODE=off or
// TQ_ENABLED=0 in tq's own env. Every `tq claude-hook <event>` calls it first
// (newClaudeHookCmd wraps each subcommand) and exits 0 with no output when it
// is true, before reading stdin.
//
// STUB (interface commit): always false. WS2 implements the env check.
func hookOff() bool { return false }
