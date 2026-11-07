// Package termopen opens a new terminal window in a folder, for `tentaqles reach`: Windows Terminal
// (`wt -d`) when it is on PATH, else PowerShell in a new console; Terminal.app on macOS; the first
// emulator found on Linux.
//
// Open starts a window and waits briefly for a launcher that fails at once: a launcher that exits
// non-zero within that wait did not open one. Kill ends one process, and reach uses it on exactly one
// pid, the shell reach was run from, once the new terminal has started and the developer has read the
// output (ADR-0019). ShellName and Interactive decide whether that shell is one reach may end.
package termopen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Launch is one process to start: Name with Args, in Dir, with Env as its environment (nil inherits
// this process's).
type Launch struct {
	Name string
	Args []string
	Dir  string
	Env  []string
	// Shell is the shell the new window runs when tq chose it: "powershell" for the Windows fallback.
	// It is "" when the window runs the system's default shell, which tq does not choose.
	Shell string
}

// String is the command line, for the message tq prints.
func (l Launch) String() string {
	s := l.Name
	for _, a := range l.Args {
		s += " " + a
	}
	return s
}

func validDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("terminal folder is empty")
	}
	for _, r := range dir {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("terminal folder must not contain control characters")
		}
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("terminal folder %q is not an absolute path", dir)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("terminal folder: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("terminal folder %s is not a directory", dir)
	}
	return nil
}

// Plan picks the launch for goos. It is pure apart from the folder check, so every OS's choice is
// testable on any host.
//
// No folder is ever handed to a shell to parse: wt and open take it as one argv element, and the
// Windows fallback gets it only as the child's working directory, because cmd.exe re-parses its command
// line and Go's quoting cannot protect a name like `a & calc`.
func Plan(goos, dir string, lookPath func(string) (string, error)) (Launch, error) {
	if err := validDir(dir); err != nil {
		return Launch{}, err
	}
	switch goos {
	case "windows":
		// wt splits its own argv on ';' into subcommands, so such a folder takes the fallback too.
		if _, err := lookPath("wt"); err == nil && !strings.Contains(dir, ";") {
			return Launch{Name: "wt", Args: []string{"-d", dir}, Dir: dir}, nil
		}
		// The fallback is PowerShell (ADR-0019): cmd's start opens it in a new console, in the folder.
		return Launch{Name: "cmd", Args: []string{"/c", "start", "", "powershell", "-NoLogo"}, Dir: dir, Shell: "powershell"}, nil
	case "darwin":
		return Launch{Name: "open", Args: []string{"-a", "Terminal", dir}, Dir: dir}, nil
	}
	candidates := []Launch{
		{Name: "x-terminal-emulator"},
		{Name: "gnome-terminal", Args: []string{"--working-directory=" + dir}},
		{Name: "konsole", Args: []string{"--workdir", dir}},
		{Name: "xterm"},
	}
	for _, c := range candidates {
		if _, err := lookPath(c.Name); err == nil {
			c.Dir = dir
			return c, nil
		}
	}
	return Launch{}, fmt.Errorf("no terminal emulator found (tried x-terminal-emulator, gnome-terminal, konsole, xterm)")
}

// startGrace is how long start waits for a launcher that fails at once. A launcher that hands the window
// off exits at once with 0; one that cannot open it exits non-zero at once; a terminal that runs (xterm)
// does not exit within it.
var startGrace = 2 * time.Second

// start is the process seam, swapped in tests.
var start = func(l Launch) error {
	c := exec.Command(l.Name, l.Args...)
	c.Dir = l.Dir
	c.Env = l.Env
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%w: %s", err, lastLine(stderr.String()))
		}
		return nil
	case <-time.After(startGrace):
		return nil
	}
}

// lastLine is the last non-empty line of a launcher's stderr, the reason it gives.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func open(goos, dir string, env []string, lookPath func(string) (string, error)) (Launch, error) {
	l, err := Plan(goos, dir, lookPath)
	if err != nil {
		return Launch{}, err
	}
	l.Env = env
	if err := start(l); err != nil {
		return l, fmt.Errorf("open a terminal with %s: %w", l.Name, err)
	}
	return l, nil
}

// Open starts a new terminal window in dir, with env as its environment, and returns what it ran.
func Open(dir string, env []string) (Launch, error) {
	return open(runtime.GOOS, dir, env, exec.LookPath)
}

// ShellName names the shell an executable is, when it is one reach may end: powershell, pwsh, bash,
// zsh, fish or cmd. The directory, a .exe suffix, case and a login shell's leading dash are dropped.
// Anything else (a terminal, a runtime, sh) is not a shell reach ends.
func ShellName(exe string) (string, bool) {
	n := strings.ToLower(path.Base(strings.ReplaceAll(strings.TrimSpace(exe), `\`, "/")))
	n = strings.TrimSuffix(strings.TrimPrefix(n, "-"), ".exe")
	switch n {
	case "powershell", "pwsh", "bash", "zsh", "fish", "cmd":
		return n, true
	}
	return "", false
}

// valueOptions are the shell options that take the next word as their value, so that word is not a
// script. An option not listed here leaves its value counted as a script, which keeps the shell open.
var valueOptions = map[string]map[string]bool{
	"powershell": powershellValues,
	"pwsh":       powershellValues,
	"bash":       posixValues,
	"zsh":        posixValues,
	"fish":       posixValues,
}

var powershellValues = map[string]bool{
	"-executionpolicy": true, "-ex": true, "-ep": true, "-workingdirectory": true, "-wd": true,
	"-configurationname": true, "-settingsfile": true, "-windowstyle": true, "-w": true,
	"-inputformat": true, "-if": true, "-outputformat": true, "-of": true,
}

var posixValues = map[string]bool{
	"-o": true, "+o": true, "-O": true, "+O": true, "--rcfile": true, "--init-file": true, "-d": true,
}

// isNonInteractiveFlag reports whether one word of a shell's command line makes it run something instead
// of starting a session: a command (-c, -Command, -EncodedCommand), a file (-File), -NonInteractive, /c.
// The PowerShell names are matched by their unique prefixes, as PowerShell reads them.
func isNonInteractiveFlag(shell, a string) bool {
	switch shell {
	case "cmd":
		return strings.HasPrefix(a, "/c") || strings.HasPrefix(a, "/r")
	case "powershell", "pwsh":
		short := len(a) >= 2 && (strings.HasPrefix("-command", a) || strings.HasPrefix("-file", a) ||
			strings.HasPrefix("-encodedcommand", a))
		return short || (len(a) >= 5 && strings.HasPrefix("-noninteractive", a))
	}
	return strings.HasPrefix(a, "--command") || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c"))
}

// Interactive reports whether a shell's command line starts an interactive session: no command, no file
// and no script named on the line. A bare word after the options is a script (or a command) and makes
// the shell non-interactive; so does an empty command line. When reach cannot tell, it leaves the
// terminal open, so every doubt answers no.
func Interactive(shell, cmdline string) bool {
	args := strings.Fields(cmdline)
	if len(args) == 0 {
		return false
	}
	valued := valueOptions[shell]
	skipValue := false
	for _, raw := range args[1:] {
		a := strings.ToLower(strings.Trim(raw, `"'`))
		if skipValue {
			skipValue = false
			continue
		}
		if a == "" {
			return false
		}
		if isNonInteractiveFlag(shell, a) {
			return false
		}
		flag := strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") || (shell == "cmd" && strings.HasPrefix(a, "/"))
		if !flag {
			return false
		}
		skipValue = valued[a]
	}
	return true
}

// Kill ends the process pid. Its one caller ends the shell reach was run from, after the output is
// read; it never walks up the tree.
func Kill(pid int) error {
	return killProcess(pid)
}
