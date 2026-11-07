//go:build windows

package termopen

import "golang.org/x/sys/windows"

// splitCommandLine splits a Windows command line the way the OS does (CommandLineToArgv), so a quoted
// executable path with a space, such as "C:\Program Files\PowerShell\7\pwsh.exe", is one word.
func splitCommandLine(s string) []string {
	words, err := windows.DecomposeCommandLine(s)
	if err != nil {
		return splitWords(s)
	}
	return words
}
