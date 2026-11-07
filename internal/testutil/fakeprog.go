package testutil

import (
	"encoding/json"
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

// A fake program is a copy of the test binary named like the program it stands for (winget,
// brew, az), with a sidecar file <program>.fake.json that says what it does. A test package
// calls FakeMain first in TestMain; run as a fake, the binary records its argv, prints the
// spec's output, optionally copies files (an install creating its CLI) and exits with the
// spec's code. No real package manager is ever run by a test.

// FakeCopy is a file a fake program copies when it runs.
type FakeCopy struct {
	From, To string
}

// FakeSpec is what a fake program does.
type FakeSpec struct {
	// Log is a file the fake appends one JSON array (its argv without the program) to.
	Log string `json:"log,omitempty"`
	// EnvLog is a file the fake appends one JSON object to: the values of the variables named
	// in EnvNames, as the fake saw them ("" when unset).
	EnvLog   string   `json:"env_log,omitempty"`
	EnvNames []string `json:"env_names,omitempty"`
	// Stdout and Stderr are printed as they are.
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	// Exit is the exit code.
	Exit int `json:"exit,omitempty"`
	// SleepMS makes the fake wait before it does anything else (a hang).
	SleepMS int `json:"sleep_ms,omitempty"`
	// Copy lists files copied when the fake runs, in order.
	Copy []FakeCopy `json:"copy,omitempty"`
	// FailArg makes the fake fail (exit 1, FailStderr on stderr, nothing copied) when one of its
	// arguments is exactly this: one package manager that fails for one package only.
	FailArg    string `json:"fail_arg,omitempty"`
	FailStderr string `json:"fail_stderr,omitempty"`
	// PidFile is a file every fake process appends its pid to, one per line.
	PidFile string `json:"pid_file,omitempty"`
	// Spawn makes the fake start one child of itself that sleeps for a minute, holding the
	// fake's stdout and stderr, and wait for it to record its pid before doing anything else.
	Spawn bool `json:"spawn,omitempty"`
}

const (
	fakeSuffix   = ".fake.json"
	fakeChildEnv = "TQ_FAKE_CHILD"
)

// FakeMain makes this process behave as a fake program when a sidecar names it. It reports
// whether it did, and the exit code to use.
func FakeMain() (bool, int) {
	exe, err := os.Executable()
	if err != nil {
		return false, 0
	}
	raw, err := os.ReadFile(exe + fakeSuffix)
	if err != nil {
		return false, 0
	}
	var spec FakeSpec
	if json.Unmarshal(raw, &spec) != nil {
		return true, 99
	}
	if spec.PidFile != "" {
		appendLine(spec.PidFile, fmt.Sprint(os.Getpid()))
	}
	if os.Getenv(fakeChildEnv) == "1" {
		time.Sleep(time.Minute)
		return true, 0
	}
	if spec.Log != "" {
		line, _ := json.Marshal(os.Args[1:])
		appendLine(spec.Log, string(line))
	}
	if spec.EnvLog != "" {
		vals := map[string]string{}
		for _, n := range spec.EnvNames {
			vals[n] = os.Getenv(n)
		}
		line, _ := json.Marshal(vals)
		appendLine(spec.EnvLog, string(line))
	}
	if spec.FailArg != "" {
		for _, a := range os.Args[1:] {
			if a == spec.FailArg {
				fmt.Fprint(os.Stderr, spec.FailStderr)
				return true, 1
			}
		}
	}
	if spec.Spawn {
		c := exec.Command(exe)
		c.Env = append(os.Environ(), fakeChildEnv+"=1")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "fake program:", err)
			return true, 97
		}
		for i := 0; i < 200 && spec.PidFile != "" && pidCount(spec.PidFile) < 2; i++ {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if spec.SleepMS > 0 {
		time.Sleep(time.Duration(spec.SleepMS) * time.Millisecond)
	}
	for _, c := range spec.Copy {
		if err := copyFile(c.From, c.To); err != nil {
			fmt.Fprintln(os.Stderr, "fake program:", err)
			return true, 98
		}
	}
	fmt.Fprint(os.Stdout, spec.Stdout)
	fmt.Fprint(os.Stderr, spec.Stderr)
	return true, spec.Exit
}

func appendLine(path, line string) {
	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.Write([]byte(line + "\n"))
		_ = f.Close()
	}
}

func pidCount(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(strings.Fields(string(raw)))
}

// ReadPids returns the pids a PidFile holds.
func ReadPids(t testing.TB, path string) []int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, f := range strings.Fields(string(raw)) {
		var n int
		if _, err := fmt.Sscan(f, &n); err != nil {
			t.Fatalf("pid file line %q: %v", f, err)
		}
		out = append(out, n)
	}
	return out
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// FakeFile is the file name of a fake program called name on this OS.
func FakeFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// NewFakeProgram writes a fake program called name into dir and returns its path. dir should
// be put on the PATH of the code under test.
func NewFakeProgram(t testing.TB, dir, name string, spec FakeSpec) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, FakeFile(name))
	if err := copyFile(self, dst); err != nil {
		t.Fatal(err)
	}
	WriteFakeSpec(t, dst, spec)
	return dst
}

// WriteFakeSpec sets what the fake program at path does.
func WriteFakeSpec(t testing.TB, path string, spec FakeSpec) {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+fakeSuffix, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// FakeSidecar is the path of the spec file of the fake program at path.
func FakeSidecar(path string) string { return path + fakeSuffix }

// ReadFakeLog returns the argv lines a fake program logged, oldest first; none when the file
// does not exist.
func ReadFakeLog(t testing.TB, log string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var argv []string
		if err := json.Unmarshal([]byte(l), &argv); err != nil {
			t.Fatalf("fake log line %q: %v", l, err)
		}
		out = append(out, argv)
	}
	return out
}

// ReadFakeEnv returns the variable maps a fake program logged to its EnvLog, oldest first.
func ReadFakeEnv(t testing.TB, log string) []map[string]string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("fake env line %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}
