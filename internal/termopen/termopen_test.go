package termopen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func look(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, f := range found {
			if f == name {
				return "/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestPlan_WindowsPrefersWindowsTerminal(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("windows", dir, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "wt" || !reflect.DeepEqual(l.Args, []string{"-d", dir}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
}

// Without wt, cmd's start opens PowerShell in a new console (ADR-0019); the folder travels only as the
// child's working directory, never through cmd.exe's own parsing.
func TestPlan_WindowsFallsBackToPowerShell(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a & calc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Plan("windows", dir, look())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "cmd" || !reflect.DeepEqual(l.Args, []string{"/c", "start", "", "powershell", "-NoLogo"}) || l.Dir != dir || l.Shell != "powershell" {
		t.Fatalf("got %+v", l)
	}
}

// wt splits its own argv on ';' into subcommands, so a folder with ';' goes through the PowerShell
// fallback, which passes it only as the working directory.
func TestPlan_WindowsSemicolonFolderAvoidsWT(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a; new-tab calc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := Plan("windows", dir, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "cmd" || !reflect.DeepEqual(l.Args, []string{"/c", "start", "", "powershell", "-NoLogo"}) || l.Dir != dir || l.Shell != "powershell" {
		t.Fatalf("got %+v", l)
	}
}

// wt runs the window in the system's default shell, which tq does not choose: Shell stays empty.
func TestPlan_WindowsTerminalChoosesNoShell(t *testing.T) {
	l, err := Plan("windows", t.TempDir(), look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Shell != "" {
		t.Fatalf("wt chose shell %q", l.Shell)
	}
}

func TestPlan_Darwin(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("darwin", dir, look())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "open" || !reflect.DeepEqual(l.Args, []string{"-a", "Terminal", dir}) {
		t.Fatalf("got %+v", l)
	}
}

func TestPlan_LinuxCandidates(t *testing.T) {
	dir := t.TempDir()
	l, err := Plan("linux", dir, look("gnome-terminal", "xterm"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "gnome-terminal" || !reflect.DeepEqual(l.Args, []string{"--working-directory=" + dir}) || l.Dir != dir {
		t.Fatalf("got %+v", l)
	}
	if _, err := Plan("linux", dir, look()); err == nil {
		t.Fatal("no emulator on PATH must be an error")
	}
}

func TestPlan_RefusesBadFolders(t *testing.T) {
	if _, err := Plan("windows", "", look("wt")); err == nil {
		t.Error("empty folder")
	}
	if _, err := Plan("windows", "relative", look("wt")); err == nil {
		t.Error("relative folder")
	}
	if _, err := Plan("windows", filepath.Join(t.TempDir(), "missing"), look("wt")); err == nil {
		t.Error("missing folder")
	}
	if _, err := Plan("windows", t.TempDir()+"\n", look("wt")); err == nil {
		t.Error("control character")
	}
}

func TestOpen_StartsAndNeverWaits(t *testing.T) {
	dir := t.TempDir()
	var got Launch
	prev := start
	start = func(l Launch) error { got = l; return nil }
	t.Cleanup(func() { start = prev })
	l, err := open("windows", dir, []string{"A=1"}, look("wt"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, got) || got.Name != "wt" || !reflect.DeepEqual(got.Env, []string{"A=1"}) {
		t.Fatalf("started %+v, planned %+v", got, l)
	}
}

// The launcher is watched briefly: one that exits non-zero at once did not open a window, and the
// error says why (L-03). One that exits 0 at once handed the window off; one still running after the
// wait is a window.
func TestStart_ReportsALauncherThatFailsAtOnce(t *testing.T) {
	prev := startGrace
	startGrace = 5 * time.Second
	t.Cleanup(func() { startGrace = prev })
	child := func(mode string) Launch {
		return Launch{Name: os.Args[0], Env: append(os.Environ(), "TERMOPEN_TEST_CHILD="+mode)}
	}
	err := start(child("exit1"))
	if err == nil || !strings.Contains(err.Error(), "cannot open display") {
		t.Fatalf("a launcher that exits 1 at once: err = %v", err)
	}
	if err := start(child("exit0")); err != nil {
		t.Fatalf("a launcher that hands off and exits 0: %v", err)
	}
	startGrace = 300 * time.Millisecond
	if err := start(child("sleep")); err != nil {
		t.Fatalf("a window still running after the wait: %v", err)
	}
}

// TestMain also serves as the launcher of the start tests: a child run with TERMOPEN_TEST_CHILD set
// behaves as the mode says, and is not a test run.
func TestMain(m *testing.M) {
	switch os.Getenv("TERMOPEN_TEST_CHILD") {
	case "exit0":
		os.Exit(0)
	case "exit1":
		fmt.Fprintln(os.Stderr, "cannot open display")
		os.Exit(1)
	case "sleep":
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A folder name with shell metacharacters reaches the new terminal as one argv element or as the
// working directory, never as part of a command line a shell or cmd.exe would parse (REQ-11, S-02).
func TestPlan_HostileFolderNames(t *testing.T) {
	names := []string{"a & calc", "a;b", "%PATH%", "it's quoted"}
	if runtime.GOOS != "windows" {
		// Windows folder names cannot hold a double quote; elsewhere one must still be one argument.
		names = append(names, `say "hi"`)
	}
	lookups := []struct {
		goos string
		look func(string) (string, error)
	}{
		{"windows", look("wt")},
		{"windows", look()},
		{"darwin", look()},
		{"linux", look("gnome-terminal", "xterm")},
		{"linux", look("x-terminal-emulator")},
		{"linux", look("konsole")},
	}
	for _, name := range names {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, lk := range lookups {
			l, err := Plan(lk.goos, dir, lk.look)
			if err != nil {
				t.Fatalf("%s %q: %v", lk.goos, name, err)
			}
			if l.Dir != dir {
				t.Errorf("%s %q: working directory %q, want %q", lk.goos, name, l.Dir, dir)
			}
			// The folder is either one whole argument, or in no argument at all (the working directory
			// alone): any other argument that carries its text is a command line a shell could parse.
			for _, a := range l.Args {
				if strings.Contains(a, name) && a != dir && a != "--working-directory="+dir {
					t.Errorf("%s %q: the folder is inside argument %q of %s", lk.goos, name, a, l.Name)
				}
			}
		}
	}
}

func TestShellName(t *testing.T) {
	for in, want := range map[string]string{
		"pwsh.exe": "pwsh", "PowerShell.exe": "powershell", `C:\Program Files\PowerShell\7\pwsh.exe`: "pwsh",
		"bash": "bash", "-zsh": "zsh", "/usr/bin/fish": "fish", "cmd.exe": "cmd",
	} {
		if got, ok := ShellName(in); !ok || got != want {
			t.Errorf("ShellName(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"explorer.exe", "node", "python", "WindowsTerminal.exe", "sh", "", "code"} {
		if got, ok := ShellName(in); ok {
			t.Errorf("ShellName(%q) = %q: not a shell reach may end", in, got)
		}
	}
}

// A shell started to run something (-c, -Command, -File, /c) is not an interactive session, and
// neither is an unreadable command line: reach then leaves the terminal open.
func TestInteractive(t *testing.T) {
	interactive := [][2]string{
		{"pwsh", "pwsh.exe -NoLogo"},
		{"pwsh", "pwsh -NoLogo -ExecutionPolicy Bypass"},
		{"powershell", "powershell.exe"},
		{"bash", "bash"},
		{"bash", "-bash"},
		{"bash", "bash -l -i"},
		{"zsh", "zsh"},
		{"fish", "fish"},
		{"cmd", "cmd.exe /k"},
		{"cmd", "cmd"},
		{"pwsh", "pwsh -ExecutionPolicy RemoteSigned -NoProfile"},
		{"bash", "bash -o pipefail"},
		{"zsh", "zsh -l"},
		{"cmd", "cmd /d /k"},
	}
	batch := [][2]string{
		{"pwsh", `pwsh -Command "tq reach"`},
		{"pwsh", "pwsh -c tq"},
		{"pwsh", "pwsh -NonInteractive"},
		{"pwsh", "pwsh -EncodedCommand ZQBjAGgAbwA="},
		{"pwsh", "pwsh -ec ZQBjAGgAbwA="},
		{"pwsh", "pwsh -e ZQBjAGgAbwA="},
		{"powershell", "powershell -File x.ps1"},
		{"pwsh", "pwsh setup.ps1"},
		{"powershell", "powershell tentaqles reach"},
		{"pwsh", "pwsh -ExecutionPolicy Bypass setup.ps1"},
		{"bash", "bash -c 'tq reach'"},
		{"bash", "bash setup.sh"},
		{"zsh", "zsh -lc tq"},
		{"fish", "fish --command tq"},
		{"cmd", "cmd.exe /c tq reach"},
		{"cmd", "cmd /k tq reach"},
		{"bash", ""},
		{"pwsh", "   "},
	}
	for _, k := range interactive {
		if !Interactive(k[0], k[1]) {
			t.Errorf("Interactive(%q, %q) = false, want true", k[0], k[1])
		}
	}
	for _, k := range batch {
		if Interactive(k[0], k[1]) {
			t.Errorf("Interactive(%q, %q) = true, want false", k[0], k[1])
		}
	}
}
