//go:build windows

package termopen

import "golang.org/x/sys/windows"

// splitCommandLine splits a Windows command line the way the OS does (CommandLineToArgv), so a quoted
// executable path with a space, such as "C:\Program Files\PowerShell\7\pwsh.exe", is one word. ok is false
// when the line cannot be split: the caller then keeps the shell open.
func splitCommandLine(s string) (words []string, ok bool) {
	if doubtful(s, `"`) {
		return nil, false
	}
	words, err := windows.DecomposeCommandLine(s)
	return words, err == nil
}
