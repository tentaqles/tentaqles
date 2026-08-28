package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// defaultGOOS is the real GOOS seam, overridden in tests.
func defaultGOOS() string { return runtime.GOOS }

// windowsShellMetachars are the characters cmd.exe interprets before the
// child process ever sees its argv. "cmd /c start ... cmd /k <command>" hands
// command to a *second* cmd.exe, so Go's argv quoting is not enough: these
// have to be rejected outright.
const windowsShellMetachars = `&|^<>%!"`

// validateTerminalCommand rejects commands that cannot be passed safely to
// the platform's terminal invocation.
func validateTerminalCommand(goos, command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("terminal command is empty")
	}
	if strings.ContainsAny(command, "\x00\r\n") {
		return fmt.Errorf("terminal command must not contain newlines or NUL bytes")
	}
	if goos == "windows" {
		if i := strings.IndexAny(command, windowsShellMetachars); i >= 0 {
			return fmt.Errorf("terminal command contains the character %q, which cmd.exe would interpret; only plain commands are allowed", string(command[i]))
		}
	}
	return nil
}

// linuxTerminal is one candidate terminal emulator invocation.
type linuxTerminal struct {
	Name string
	Args []string
}

// linuxTerminalCandidates returns, in preference order, the terminal
// emulators to try on Linux for running command. It is pure so the ordering
// and quoting can be tested anywhere.
//
// The command has to live inside a shell line here (so the shell stays open
// afterwards), so it is wrapped in single quotes with any embedded quote
// escaped — otherwise a command containing ";" or "'" would be reinterpreted
// by the outer shell.
func linuxTerminalCandidates(command string) []linuxTerminal {
	line := "sh -c " + quoteSingle(command) + "; exec sh"
	return []linuxTerminal{
		{"x-terminal-emulator", []string{"-e", "sh", "-c", line}},
		{"gnome-terminal", []string{"--", "sh", "-c", line}},
		{"konsole", []string{"-e", "sh", "-c", line}},
		{"xterm", []string{"-e", "sh", "-c", line}},
	}
}

// terminalCommand returns the exec.Command name/args to open a new terminal
// window running command, for the given goos value. It is pure so it can be
// tested against every OS regardless of the host running the test.
//
// On Windows the command is passed as a single argv element to a nested
// cmd.exe; callers must run validateTerminalCommand first, since cmd.exe
// re-parses shell metacharacters that Go's quoting cannot protect. On Linux
// this returns the first candidate from linuxTerminalCandidates; OpenTerminal
// tries each in turn.
func terminalCommand(goos, command string) (string, []string) {
	switch goos {
	case "windows":
		// cmd /c start "" cmd /k <command>
		return "cmd", []string{"/c", "start", "", "cmd", "/k", command}
	case "darwin":
		script := `tell application "Terminal" to do script "` + escapeAppleScript(command) + `"`
		return "osascript", []string{"-e", script}
	default:
		c := linuxTerminalCandidates(command)[0]
		return c.Name, c.Args
	}
}

// lookPath is a seam over exec.LookPath for tests.
var lookPath = exec.LookPath

// resolveTerminal picks the terminal invocation to run for the given OS,
// resolving the Linux fallback chain against PATH.
func resolveTerminal(goos, command string) (string, []string, error) {
	if err := validateTerminalCommand(goos, command); err != nil {
		return "", nil, err
	}
	if goos == "windows" || goos == "darwin" {
		name, args := terminalCommand(goos, command)
		return name, args, nil
	}
	for _, c := range linuxTerminalCandidates(command) {
		if _, err := lookPath(c.Name); err == nil {
			return c.Name, c.Args, nil
		}
	}
	return "", nil, fmt.Errorf("no terminal emulator found (tried x-terminal-emulator, gnome-terminal, konsole, xterm)")
}

// quoteSingle wraps s in single quotes, escaping any single quote inside it
// using the POSIX '\'' idiom (close quote, escaped quote, reopen quote),
// so the result is a single shell word.
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
