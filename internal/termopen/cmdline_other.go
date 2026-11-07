//go:build !windows

package termopen

// joinedArgv is true on this OS: the command line humanonly reads is argv joined with spaces, not split.
const joinedArgv = true

// splitCommandLine splits the command line humanonly reads on this OS. Linux and macOS hand over argv joined
// with plain spaces and no quoting (internal/humanonly), so splitPlain is the split: no shell quoting is
// re-applied. A word boundary in such a line is not reliable, so Interactive judges it with joinedSessionFlags,
// and every doubt keeps the shell open (ADR-0019).
func splitCommandLine(s string) (words []string, ok bool) {
	return splitPlain(s)
}
