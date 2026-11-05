package humanonly

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
)

// terminalChain is a developer's process tree as the walker would read it:
// self <- pwsh <- WindowsTerminal.exe <- explorer.exe <- (exited).
func terminalChain() ParentFunc {
	return fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq.exe", "tq", false, false},
		90:        {80, "pwsh.exe", "pwsh -NoLogo", false, false},
		80:        {70, "WindowsTerminal.exe", "wt", false, false},
		70:        {7777, "explorer.exe", "explorer", false, false},
	})
}

// human puts the process in the state of a developer at a terminal: an
// isolated TQ_HOME, both standard streams a terminal, a terminal in the
// process tree, no marker in the environment and a confirmation that says
// yes. Each test then breaks exactly one of those.
func human(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range Markers {
		if v, ok := os.LookupEnv(k); ok {
			if err := os.Unsetenv(k); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Setenv(k, v) })
		}
	}
	prevTTY, prevParent, prevConfirm := IsTerminal, ParentOf, Confirm
	IsTerminal = func(*os.File) bool { return true }
	ParentOf = terminalChain()
	Confirm = func(string) (bool, error) { return true, nil }
	t.Cleanup(func() { IsTerminal, ParentOf, Confirm = prevTTY, prevParent, prevConfirm })
	return filepath.Join(home, ".tentaqles")
}

// denials reads every line of the denial log.
func denials(t *testing.T) []audit.Denial {
	t.Helper()
	b, err := os.ReadFile(audit.DenialsPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Denial
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var d audit.Denial
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		out = append(out, d)
	}
	return out
}

func TestHumanPasses(t *testing.T) {
	human(t)
	if processTreeMethod == "" {
		t.Skip("no process-tree walker on this OS: every human-only command refuses here")
	}
	if r := Detect(); r != nil {
		t.Fatalf("a human at a terminal must pass, got %+v", r)
	}
	if err := Check("tq allow"); err != nil {
		t.Fatal(err)
	}
	if ds := denials(t); len(ds) != 0 {
		t.Fatalf("a pass logs nothing, got %+v", ds)
	}
}

func TestTTYDetector(t *testing.T) {
	human(t)
	IsTerminal = func(f *os.File) bool { return f != os.Stdin }
	if r := Detect(); r == nil || r.Detector != DetectorTTY || !strings.Contains(r.Reason, "stdin") {
		t.Fatalf("a piped stdin must refuse: %+v", r)
	}
	IsTerminal = func(f *os.File) bool { return f != os.Stdout }
	if r := Detect(); r == nil || r.Detector != DetectorTTY || !strings.Contains(r.Reason, "stdout") {
		t.Fatalf("a redirected stdout must refuse: %+v", r)
	}
	err := Check("tq allow")
	if !errors.Is(err, ErrHumanOnly) || !strings.Contains(err.Error(), "tq allow") || !strings.Contains(err.Error(), DetectorTTY) {
		t.Fatalf("want ErrHumanOnly naming the command and the detector, got %v", err)
	}
}

func TestProcessTreeDetector(t *testing.T) {
	human(t)
	if processTreeMethod == "" {
		t.Skip("no process-tree walker on this OS")
	}
	ParentOf = fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq.exe", "", false, false},
		90:        {80, "bash.exe", "bash", false, false},
		80:        {70, "claude.exe", "claude", false, false},
		70:        {0, "WindowsTerminal.exe", "", false, false},
	})
	r := Detect()
	if r == nil || r.Detector != DetectorProcessTree || !strings.Contains(r.Reason, "claude.exe") {
		t.Fatalf("claude.exe in the tree must refuse: %+v", r)
	}
	ParentOf = terminalChain()
	if r := Detect(); r != nil {
		t.Fatalf("pwsh under WindowsTerminal must pass: %+v", r)
	}
	ParentOf = fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq", "", false, false},
		90:        {80, "node", "node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js", false, false},
		80:        {0, "zsh", "-zsh", false, false},
	})
	if r := Detect(); r == nil || r.Detector != DetectorProcessTree || !strings.Contains(r.Reason, "node") {
		t.Fatalf("node running claude must refuse: %+v", r)
	}
	// An orphan: the parent is gone before any terminal was seen.
	ParentOf = fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq", "", false, false},
		90:        {12345, "bash", "bash", false, false},
	})
	if r := Detect(); r == nil || r.Detector != DetectorProcessTree || !strings.HasPrefix(r.Reason, "cannot establish a human ancestry") {
		t.Fatalf("an orphaned chain must refuse: %+v", r)
	}
	// Pid reuse on Windows.
	ParentOf = fakeTree(map[int]fakeProc{
		selfPID(): {90, "tq.exe", "", false, false},
		90:        {80, "pwsh.exe", "pwsh", false, true},
	})
	if r := Detect(); r == nil || !strings.Contains(r.Reason, "reused") {
		t.Fatalf("a reused pid must refuse: %+v", r)
	}
}

func TestEnvMarkerDetector(t *testing.T) {
	human(t)
	for _, k := range Markers {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, "1")
			r := Detect()
			if r == nil || r.Detector != DetectorEnvMarker || r.Reason != k+" is set" {
				t.Fatalf("%s must refuse: %+v", k, r)
			}
			if err := Check("tq deny"); !errors.Is(err, ErrHumanOnly) {
				t.Fatalf("%s: %v", k, err)
			}
		})
	}
	if processTreeMethod != "" {
		// An empty value is not a marker.
		t.Setenv("CLAUDECODE", "  ")
		if r := Detect(); r != nil {
			t.Fatalf("a blank marker is not set: %+v", r)
		}
	}
	// The two variables tq sets in human terminals are never markers.
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		for _, m := range Markers {
			if m == k {
				t.Fatalf("%s must not be a marker: tq sets it in human terminals", k)
			}
		}
	}
}

// Every refusal is one JSON line in the denial log: what, why and where, and
// never an environment value.
func TestRefusalIsLogged(t *testing.T) {
	human(t)
	t.Setenv("CLAUDECODE", "secret-looking-value")
	if err := Check("tq allow"); !errors.Is(err, ErrHumanOnly) {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "")
	IsTerminal = func(*os.File) bool { return false }
	if err := Check("tq hooks install"); !errors.Is(err, ErrHumanOnly) {
		t.Fatal(err)
	}
	ds := denials(t)
	if len(ds) != 2 {
		t.Fatalf("want two denials, got %+v", ds)
	}
	cwd, _ := os.Getwd()
	for i, want := range []struct{ cmd, reason string }{{"tq allow", "env-marker: CLAUDECODE is set"}, {"tq hooks install", "tty: stdin is not a terminal"}} {
		d := ds[i]
		if d.Kind != "human-only" || d.Command != want.cmd || d.Reason != want.reason || d.PID != os.Getpid() || d.Cwd != cwd || d.Time.IsZero() {
			t.Fatalf("denial %d = %+v, want %+v", i, d, want)
		}
	}
	b, _ := os.ReadFile(audit.DenialsPath())
	if strings.Contains(string(b), "secret-looking-value") {
		t.Fatal("the denial log must never carry an environment value")
	}
}

// A denial log that cannot be written never lets the command through: the
// refusal stands and the error says the log failed.
func TestRefusalStandsWhenTheLogCannotBeWritten(t *testing.T) {
	home := human(t)
	t.Setenv("CLAUDECODE", "1")
	// A directory where the log file should be.
	if err := os.MkdirAll(audit.DenialsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	err := Check("tq allow")
	if !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("the refusal must stand: %v", err)
	}
	if !strings.Contains(err.Error(), "refusal could not be logged: ") || !strings.Contains(err.Error(), "env-marker: CLAUDECODE is set") {
		t.Fatalf("the error names the log failure and the detector: %v", err)
	}
	// A regular file where the state directory should be.
	t.Setenv("TQ_HOME", filepath.Join(home, "not-a-dir", "state"))
	if err := os.WriteFile(filepath.Join(home, "not-a-dir"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = Check("tq deny")
	if !errors.Is(err, ErrHumanOnly) || !strings.Contains(err.Error(), "refusal could not be logged: ") {
		t.Fatalf("an unwritable state dir: %v", err)
	}
	asked := false
	Confirm = func(string) (bool, error) { asked = true; return true, nil }
	if err := Gate("tq deny", "x", &bytes.Buffer{}); !errors.Is(err, ErrHumanOnly) || asked {
		t.Fatalf("a refusal with a failed log never reaches the confirmation: %v asked=%v", err, asked)
	}
}

func TestGateConfirms(t *testing.T) {
	human(t)
	if processTreeMethod == "" {
		t.Skip("no process-tree walker on this OS")
	}
	var prompt string
	Confirm = func(p string) (bool, error) { prompt = p; return false, nil }
	var out bytes.Buffer
	err := Gate("tq add", "tq add: about to create /work/acme", &out)
	if !errors.Is(err, ErrAborted) || errors.Is(err, ErrHumanOnly) {
		t.Fatalf("a declined confirmation aborts: %v", err)
	}
	if !strings.Contains(out.String(), "tq add: about to create /work/acme") || prompt != "Type yes to continue: " {
		t.Fatalf("the gate prints the line and asks for yes: out=%q prompt=%q", out.String(), prompt)
	}
	Confirm = func(string) (bool, error) { return false, errors.New("no terminal") }
	if err := Gate("tq add", "x", &out); !errors.Is(err, ErrAborted) {
		t.Fatalf("a failed read aborts: %v", err)
	}
	Confirm = func(string) (bool, error) { return true, nil }
	if err := Gate("tq add", "x", &out); err != nil {
		t.Fatal(err)
	}
	// A refused session never reaches the confirmation.
	asked := false
	Confirm = func(string) (bool, error) { asked = true; return true, nil }
	t.Setenv("CODEX_THREAD_ID", "t1")
	if err := Gate("tq add", "x", &out); !errors.Is(err, ErrHumanOnly) || asked {
		t.Fatalf("refusal first, no prompt: err=%v asked=%v", err, asked)
	}
}

func TestAnswerIsYes(t *testing.T) {
	for in, want := range map[string]bool{"yes\n": true, "YES\r\n": true, " yes \n": true, "y\n": false, "no\n": false, "\n": false, "yes please\n": false} {
		if got := answerIsYes(in); got != want {
			t.Errorf("answerIsYes(%q) = %v", in, got)
		}
	}
}

func TestDetectorsReportThisOS(t *testing.T) {
	ds := Detectors()
	if len(ds) != 3 {
		t.Fatalf("three detectors: %+v", ds)
	}
	for _, d := range ds {
		switch d.Name {
		case DetectorTTY, DetectorEnvMarker:
			if !d.Available {
				t.Errorf("%s is available everywhere: %+v", d.Name, d)
			}
		case DetectorProcessTree:
			if d.Available != (processTreeMethod != "") || d.Method != processTreeMethod {
				t.Errorf("process-tree availability must follow the walker: %+v", d)
			}
		default:
			t.Errorf("unknown detector %+v", d)
		}
	}
}
