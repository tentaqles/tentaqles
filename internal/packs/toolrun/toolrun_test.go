package toolrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the fake tool: a copy of the test binary run with
// TQ_FAKE_TOOL set behaves as the mode says.
func TestMain(m *testing.M) {
	testPass = []string{"TQ_FAKE_TOOL", "TQ_FAKE_VERSION", "TQ_FAKE_PIDFILE"}
	if mode := os.Getenv("TQ_FAKE_TOOL"); mode != "" {
		fakeTool(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeTool(mode string) {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("faketool, version " + os.Getenv("TQ_FAKE_VERSION"))
		os.Exit(0)
	}
	switch mode {
	case "echo":
		fmt.Println(strings.Join(args, "|"))
		fmt.Fprintln(os.Stderr, "DO_NOT_TRACK="+os.Getenv("DO_NOT_TRACK")+" DBT="+os.Getenv("DBT_SEND_ANONYMOUS_USAGE_STATS"))
	case "env":
		for _, kv := range os.Environ() {
			fmt.Println(kv)
		}
	case "exit3":
		fmt.Fprintln(os.Stderr, "line one\nline two\nfailed for tester@acme.test")
		os.Exit(3)
	case "big":
		chunk := strings.Repeat("x", 1<<16)
		for i := 0; i < 40; i++ {
			fmt.Print(chunk)
		}
	case "sleep":
		time.Sleep(60 * time.Second)
	case "grandchild":
		// A grandchild that inherits stdout and outlives us unless the tree dies.
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "TQ_FAKE_TOOL=sleep")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		_ = c.Start()
		if f := os.Getenv("TQ_FAKE_PIDFILE"); f != "" {
			_ = os.WriteFile(f, []byte(fmt.Sprint(c.Process.Pid)), 0o600)
		}
		time.Sleep(60 * time.Second)
	}
	os.Exit(0)
}

// fakeBin copies the test binary to dir/name(.exe).
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
	return dst
}

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestProbeOrder(t *testing.T) {
	isolate(t)
	t.Setenv("TQ_FAKE_TOOL", "echo")
	t.Setenv("TQ_FAKE_VERSION", "1.8.2")
	root := t.TempDir()
	onPath := t.TempDir()
	pathBin := fakeBin(t, onPath, "sqlfluff")
	t.Setenv("PATH", onPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	r := &Runner{Root: root}
	p := r.Probe("data", "sqlfluff")
	if !p.Found || p.Source != "path" || p.Version != "1.8.2" {
		t.Fatalf("path probe: %+v", p)
	}
	venv := filepath.Join(root, ".venv", "bin")
	if runtime.GOOS == "windows" {
		venv = filepath.Join(root, ".venv", "Scripts")
	}
	os.MkdirAll(venv, 0o755)
	fakeBin(t, venv, "sqlfluff")
	r = &Runner{Root: root}
	if p := r.Probe("data", "sqlfluff"); p.Source != "venv" {
		t.Fatalf("venv must win over PATH: %+v", p)
	}
	r = &Runner{Root: root, ToolPaths: map[string]map[string]string{"data": {"sqlfluff": pathBin}}}
	if p := r.Probe("data", "sqlfluff"); p.Source != "tool_paths" || p.Path != pathBin {
		t.Fatalf("tool_paths must win: %+v", p)
	}
	r = &Runner{Root: root}
	if p := r.Probe("data", "no-such-tool-xyz"); p.Found {
		t.Fatalf("found a missing tool: %+v", p)
	}
}

func TestRunArgvEnvAndExit(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	bin := fakeBin(t, dir, "dbt")
	r := &Runner{Root: t.TempDir(), LookPath: func(string) (string, error) { return bin, nil }}
	t.Setenv("TQ_FAKE_TOOL", "echo")
	out, err := r.Run(context.Background(), Spec{Pack: "data", Tool: "dbt", Args: []string{"parse", "a b", "$HOME;rm"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out.Stdout)); got != "parse|a b|$HOME;rm" {
		t.Fatalf("argv not passed verbatim: %q", got)
	}
	if !strings.Contains(string(out.Stderr), "DO_NOT_TRACK=1 DBT=false") {
		t.Fatalf("telemetry env: %q", out.Stderr)
	}
	t.Setenv("TQ_FAKE_TOOL", "exit3")
	out, err = r.Run(context.Background(), Spec{Pack: "data", Tool: "dbt"})
	if err != nil || out.ExitCode != 3 {
		t.Fatalf("exit: %v %d", err, out.ExitCode)
	}
	tail := out.Tail(2, nil)
	if strings.Contains(tail, "tester@acme.test") || !strings.Contains(tail, "line two") || strings.Contains(tail, "line one") {
		t.Fatalf("tail must be the last lines, redacted: %q", tail)
	}
}

func TestOutputCapped(t *testing.T) {
	isolate(t)
	bin := fakeBin(t, t.TempDir(), "big")
	t.Setenv("TQ_FAKE_TOOL", "big")
	r := &Runner{LookPath: func(string) (string, error) { return bin, nil }}
	out, err := r.Run(context.Background(), Spec{Tool: "big"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Stdout) != MaxOutput || !out.StdoutTruncated {
		t.Fatalf("len %d truncated %v", len(out.Stdout), out.StdoutTruncated)
	}
}

func TestTimeoutKillsTree(t *testing.T) {
	isolate(t)
	bin := fakeBin(t, t.TempDir(), "hang")
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("TQ_FAKE_TOOL", "grandchild")
	t.Setenv("TQ_FAKE_PIDFILE", pidFile)
	r := &Runner{LookPath: func(string) (string, error) { return bin, nil }}
	start := time.Now()
	out, err := r.Run(context.Background(), Spec{Tool: "hang", Timeout: 1500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !out.TimedOut {
		t.Fatalf("not timed out: %+v", out)
	}
	// The grandchild holds stdout; without the tree kill Wait would block
	// until WaitDelay or the 60 s sleep.
	if el := time.Since(start); el > 1500*time.Millisecond+WaitDelay-500*time.Millisecond {
		t.Fatalf("took %v: the grandchild held the pipes", el)
	}
	raw, _ := os.ReadFile(pidFile)
	var pid int
	fmt.Sscan(string(raw), &pid)
	if pid == 0 {
		t.Fatal("grandchild did not start")
	}
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %d still alive", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestTimeoutResolution(t *testing.T) {
	r := &Runner{Timeouts: map[string]map[string]time.Duration{"data": {"dbt": 30 * time.Second}}}
	if got := r.Timeout(Spec{Pack: "data", Tool: "dbt", Timeout: 600 * time.Second}); got != 30*time.Second {
		t.Fatalf("manifest timeout must win: %v", got)
	}
	if got := r.Timeout(Spec{Pack: "ml", Tool: "ruff"}); got != DefaultTimeout {
		t.Fatalf("default: %v", got)
	}
	if got := r.Timeout(Spec{Pack: "ml", Tool: "ruff", Timeout: time.Hour}); got != MaxTimeout {
		t.Fatalf("cap: %v", got)
	}
}

func TestVersions(t *testing.T) {
	if ParseVersion("Core:\n  - installed: 1.8.7\n") != "1.8.7" || ParseVersion("ruff 0.6") != "0.6.0" || ParseVersion("none") != "" {
		t.Fatal("parse")
	}
	if CompareVersions("1.8.0", "1.10.0") != -1 || CompareVersions("2.0.0", "1.99.9") != 1 || CompareVersions("1.2.3", "1.2.3") != 0 || CompareVersions("", "0.0.1") != -1 {
		t.Fatal("compare")
	}
}

func TestScratch(t *testing.T) {
	isolate(t)
	dir, cleanup, err := Scratch("acme")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(dir), "/packs/acme/scratch/") {
		t.Fatalf("dir %s", dir)
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
}

// m6: a .cmd/.bat tool never takes cmd.exe metacharacters.
func TestBatchArgsRefused(t *testing.T) {
	if err := batchSafe(`C:\x\az.cmd`, []string{"deployment", "a&calc"}); !errors.Is(err, ErrUnsafeBatchArgs) {
		t.Fatal(err)
	}
	if err := batchSafe(`C:\x\az.CMD`, []string{"100%"}); !errors.Is(err, ErrUnsafeBatchArgs) {
		t.Fatal(err)
	}
	if err := batchSafe(`C:\x\az.cmd`, []string{"deployment", "group", "create"}); err != nil {
		t.Fatal(err)
	}
	if err := batchSafe(`/usr/bin/dbt`, []string{"a&b"}); err != nil {
		t.Fatal("argv-only tools are not batch files")
	}
	dir := t.TempDir()
	bat := filepath.Join(dir, "tool.cmd")
	os.WriteFile(bat, []byte("@echo off\r\n"), 0o755)
	r := &Runner{LookPath: func(string) (string, error) { return bat, nil }, NoExecRepo: true}
	if _, err := r.Run(context.Background(), Spec{Tool: "tool", Args: []string{"x|y"}}); !errors.Is(err, ErrUnsafeBatchArgs) {
		t.Fatalf("Run: %v", err)
	}
}

// m7: with NoExecRepo the .venv binary is found but never executed.
func TestNoExecRepoSkipsVenvVersion(t *testing.T) {
	isolate(t)
	t.Setenv("TQ_FAKE_TOOL", "echo")
	t.Setenv("TQ_FAKE_VERSION", "9.9.9")
	root := t.TempDir()
	venv := filepath.Join(root, ".venv", "bin")
	if runtime.GOOS == "windows" {
		venv = filepath.Join(root, ".venv", "Scripts")
	}
	os.MkdirAll(venv, 0o755)
	fakeBin(t, venv, "ruff")
	p := (&Runner{Root: root, NoExecRepo: true}).Probe("ml", "ruff")
	if !p.Found || p.Source != "venv" || p.Version != "" {
		t.Fatalf("%+v", p)
	}
}
