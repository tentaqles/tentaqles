package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome points HOME/USERPROFILE (and TQ_HOME, if the cli honors it)
// at a fresh temp dir so tests never touch the real user's environment.
func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TQ_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestApp_ProvidersAndDefaultBase(t *testing.T) {
	home := isolateHome(t)

	app := NewApp()

	providers, err := app.Providers()
	if err != nil {
		t.Fatalf("Providers() error = %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("Providers() returned no providers")
	}

	base := app.DefaultBase()
	if base == "" {
		t.Fatal("DefaultBase() returned empty string")
	}
	want := filepath.Join(home, "work")
	if base != want {
		t.Errorf("DefaultBase() = %q, want %q", base, want)
	}
}

func TestValidateTerminalCommand_Windows(t *testing.T) {
	// A plain tq invocation is fine and reaches cmd.exe as one argv element.
	if err := validateTerminalCommand("windows", "tq login acme gh"); err != nil {
		t.Fatalf("validateTerminalCommand(plain) = %v, want nil", err)
	}
	name, args := terminalCommand("windows", "tq login acme gh")
	if name != "cmd" {
		t.Fatalf("name = %q, want cmd", name)
	}
	want := []string{"/c", "start", "", "cmd", "/k", "tq login acme gh"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
	// The command must arrive as a single argv element, never concatenated
	// into a shell line that could be reinterpreted.
	for _, a := range args[:len(args)-1] {
		if strings.Contains(a, "login acme gh") {
			t.Fatalf("command leaked into an earlier arg: %q", a)
		}
	}

	// cmd.exe re-parses these before the child sees argv, so they are refused.
	for _, bad := range []string{"foo&calc", "foo|calc", "foo^calc", "foo>out", "foo<in", "foo%PATH%", "foo!x!", `foo"x`} {
		if err := validateTerminalCommand("windows", bad); err == nil {
			t.Errorf("validateTerminalCommand(%q) = nil, want error", bad)
		}
	}

	// Those characters are only a cmd.exe problem; POSIX quoting handles them.
	if err := validateTerminalCommand("linux", "foo&calc"); err != nil {
		t.Errorf("validateTerminalCommand(linux, %q) = %v, want nil", "foo&calc", err)
	}
	if err := validateTerminalCommand("windows", "  "); err == nil {
		t.Error("validateTerminalCommand(empty) = nil, want error")
	}
}

func TestLinuxTerminalCandidates_Order(t *testing.T) {
	got := linuxTerminalCandidates("tq login acme gh")
	want := []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("candidates[%d].Name = %q, want %q", i, got[i].Name, name)
		}
		if !strings.Contains(got[i].Args[len(got[i].Args)-1], "'tq login acme gh'") {
			t.Fatalf("candidates[%d] args = %v, missing quoted command", i, got[i].Args)
		}
	}
	if got[1].Args[0] != "--" {
		t.Errorf("gnome-terminal args[0] = %q, want --", got[1].Args[0])
	}
}

func TestResolveTerminal_LinuxFallback(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })

	// Only konsole is installed: the chain must skip past the first two.
	lookPath = func(name string) (string, error) {
		if name == "konsole" {
			return "/usr/bin/konsole", nil
		}
		return "", errors.New("not found")
	}
	name, args, err := resolveTerminal("linux", "tq login acme gh")
	if err != nil {
		t.Fatalf("resolveTerminal() error = %v", err)
	}
	if name != "konsole" || args[0] != "-e" {
		t.Fatalf("resolveTerminal() = %q %v, want konsole -e ...", name, args)
	}

	// Nothing installed: a clear error, not a silent failure.
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, _, err := resolveTerminal("linux", "tq login acme gh"); err == nil {
		t.Fatal("resolveTerminal() with no emulator = nil error, want error")
	}

	// Windows rejection is enforced at resolve time too.
	if _, _, err := resolveTerminal("windows", "foo&calc"); err == nil {
		t.Fatal("resolveTerminal(windows, metachar) = nil error, want error")
	}
}

func TestOpenTerminal_ArgsDarwin(t *testing.T) {
	name, args := terminalCommand("darwin", `tq login "my ws" work`)

	if name != "osascript" {
		t.Fatalf("name = %q, want osascript", name)
	}
	if len(args) != 2 || args[0] != "-e" {
		t.Fatalf("args = %v, want [-e, <script>]", args)
	}
	if !strings.Contains(args[1], `tell application "Terminal" to do script`) {
		t.Fatalf("script = %q, missing Terminal do script", args[1])
	}
}

func TestOpenTerminal_ArgsLinux(t *testing.T) {
	name, args := terminalCommand("linux", "tq login myws work")

	if name != "x-terminal-emulator" {
		t.Fatalf("name = %q, want x-terminal-emulator", name)
	}
	want := []string{"-e", "sh", "-c", "sh -c 'tq login myws work'; exec sh"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestOpenTerminal_ArgsLinuxEscapes(t *testing.T) {
	_, args := terminalCommand("linux", `tq login '; rm -rf /; '`)

	line := args[len(args)-1]

	// Every single quote from the command must be neutralised with the
	// POSIX '\'' idiom, leaving the injected text inside one quoted word.
	want := `sh -c 'tq login '\''; rm -rf /; '\'''; exec sh`
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}

	// The injected "rm -rf /" must never sit outside single quotes.
	if strings.Contains(line, `'; rm -rf /`) && !strings.Contains(line, `'\''; rm -rf /`) {
		t.Fatalf("command escaped its quoting: %q", line)
	}
}

func TestBundledTQPath_EnvFallback(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "not-a-real-tq")
	// No file exists yet: env fallback should not resolve to a missing path.
	t.Setenv("TQ_BUNDLED_PATH", fake)
	if got := bundledTQPath(); got == fake {
		t.Fatalf("bundledTQPath() = %q, want \"\" for nonexistent file", got)
	}
}
