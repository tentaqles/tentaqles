// Package termopen opens a new terminal window in a folder, for `tentaqles reach`: Windows Terminal
// (`wt -d`) when it is on PATH, else cmd's start; Terminal.app on macOS; the first emulator
// found on Linux.
//
// Open only ever starts a window: it never waits for one. Kill ends one process, and reach uses it
// on exactly one pid, the shell reach was run from, once the new terminal has started and the
// developer has read the output (ADR-0019). ShellName and Interactive decide whether that shell is
// one reach may end.
package termopen

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Launch is one process to start: Name with Args, in Dir.
type Launch struct {
	Name string
	Args []string
	Dir  string
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
// cmd start fallback gets it only as the child's working directory, because cmd.exe re-parses its
// command line and Go's quoting cannot protect a name like `a & calc`.
func Plan(goos, dir string, lookPath func(string) (string, error)) (Launch, error) {
	if err := validDir(dir); err != nil {
		return Launch{}, err
	}
	switch goos {
	case "windows":
		// wt splits its own argv on ';' into subcommands, so such a folder takes the start plan.
		if _, err := lookPath("wt"); err == nil && !strings.Contains(dir, ";") {
			return Launch{Name: "wt", Args: []string{"-d", dir}, Dir: dir}, nil
		}
		return Launch{Name: "cmd", Args: []string{"/c", "start", "", "cmd"}, Dir: dir}, nil
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

// start is the process seam, swapped in tests.
var start = func(l Launch) error {
	c := exec.Command(l.Name, l.Args...)
	c.Dir = l.Dir
	if err := c.Start(); err != nil {
		return err
	}
	// The window outlives tq; release it so no zombie or handle is left behind.
	return c.Process.Release()
}

func open(goos, dir string, lookPath func(string) (string, error)) (Launch, error) {
	l, err := Plan(goos, dir, lookPath)
	if err != nil {
		return Launch{}, err
	}
	if err := start(l); err != nil {
		return l, fmt.Errorf("open a terminal with %s: %w", l.Name, err)
	}
	return l, nil
}

// Open starts a new terminal window in dir and returns what it ran.
func Open(dir string) (Launch, error) { return open(runtime.GOOS, dir, exec.LookPath) }

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

// Interactive reports whether a shell's command line starts an interactive session. A shell started
// to run something (-c, -Command, -EncodedCommand, -File, -NonInteractive, --command, /c) is not one,
// and neither is an empty command line: when reach cannot tell, it leaves the terminal open. A script
// named without a flag is not caught; reach still needs a terminal on its own stdin.
func Interactive(shell, cmdline string) bool {
	args := strings.Fields(cmdline)
	if len(args) == 0 {
		return false
	}
	for _, a := range args[1:] {
		a = strings.ToLower(strings.Trim(a, `"'`))
		switch shell {
		case "cmd":
			if strings.HasPrefix(a, "/c") || strings.HasPrefix(a, "/r") {
				return false
			}
		case "powershell", "pwsh":
			if strings.HasPrefix(a, "-c") || strings.HasPrefix(a, "-f") || strings.HasPrefix(a, "-noni") || strings.HasPrefix(a, "-enc") {
				return false
			}
		default:
			if strings.HasPrefix(a, "--command") || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c")) {
				return false
			}
		}
	}
	return true
}

// Kill ends the process pid. Its one caller ends the shell reach was run from, after the output is
// read; it never walks up the tree.
func Kill(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
