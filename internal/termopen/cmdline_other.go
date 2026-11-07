//go:build !windows

package termopen

// splitCommandLine splits a command line the way a POSIX shell's quoting does (splitWords).
func splitCommandLine(s string) []string {
	return splitWords(s)
}
