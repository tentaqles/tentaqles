//go:build !windows

package termopen

// splitCommandLine splits the command line humanonly reads on this OS. Linux and macOS hand over argv joined
// with plain spaces and no quoting (internal/humanonly), so splitPlain is the split: no shell quoting is
// re-applied, a quote in a value is a literal character, and a value with a space becomes extra words. Those
// read as a script, so the shell stays open: every doubt keeps it open (ADR-0019).
func splitCommandLine(s string) (words []string, ok bool) {
	return splitPlain(s)
}
