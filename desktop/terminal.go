package main

import (
	"runtime"
	"strings"
)

// defaultGOOS is the real GOOS seam, overridden in tests.
func defaultGOOS() string { return runtime.GOOS }

// terminalCommand returns the exec.Command name/args to open a new terminal
// window running command, for the given goos value. It is pure so it can be
// tested against every OS regardless of the host running the test.
//
// The command argument is never concatenated into a shell line — it is
// passed as a single argv element to the terminal/shell invocation, letting
// the OS's exec layer handle quoting.
func terminalCommand(goos, command string) (string, []string) {
	switch goos {
	case "windows":
		// cmd /c start "" cmd /k <command>
		return "cmd", []string{"/c", "start", "", "cmd", "/k", command}
	case "darwin":
		script := `tell application "Terminal" to do script "` + escapeAppleScript(command) + `"`
		return "osascript", []string{"-e", script}
	default:
		// linux and other unix-likes. The command has to live inside a shell
		// line here (so the shell stays open afterwards), so it is wrapped in
		// single quotes with any embedded quote escaped — otherwise a command
		// containing ";" or "'" would be reinterpreted by the outer shell.
		return "x-terminal-emulator", []string{"-e", "sh", "-c", "sh -c " + quoteSingle(command) + "; exec sh"}
	}
}

// quoteSingle wraps s in single quotes, escaping any single quote inside it
// using the POSIX '\'' idiom, so the result is a single shell word.
func quoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// escapeAppleScript escapes double quotes and backslashes for safe
// embedding inside an AppleScript string literal.
func escapeAppleScript(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
