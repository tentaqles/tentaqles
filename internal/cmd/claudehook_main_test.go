package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The deadlines are ordered under the shortest harness hook timeout (Claude
// Code 15 s in the plugin's hooks.json, Codex codexcfg.HookTimeoutSeconds):
// scan budget < decision deadline < process watchdog < tq_hook.sh's
// watchdog (+1 s for its kill) < the harness timeout, with a margin. A
// harness kills a slow hook and RUNS the call, so raising any of these past
// the harness timeout must fail here.
func TestHookDeadlinesUnderHarnessTimeouts(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string  `json:"command"`
				Timeout float64 `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &hooks); err != nil {
		t.Fatal(err)
	}
	claude := 0.0
	for _, g := range hooks.Hooks["PreToolUse"] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "tq_hook.sh") {
				claude = h.Timeout
			}
		}
	}
	if claude == 0 {
		t.Fatal("no tq_hook.sh PreToolUse timeout in hooks.json")
	}
	harness := time.Duration(claude) * time.Second
	if c := time.Duration(codexcfg.HookTimeoutSeconds) * time.Second; c < harness {
		harness = c
	}
	sh, err := os.ReadFile(filepath.Join(root, "plugin", "scripts", "tq_hook.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^_tq_deadline=(\d+)$`).FindSubmatch(sh)
	if m == nil {
		t.Fatal("no _tq_deadline in tq_hook.sh")
	}
	n, _ := strconv.Atoi(string(m[1]))
	wrapper := time.Duration(n)*time.Second + time.Second // its TERM, then KILL a second later

	const margin = 2 * time.Second
	steps := []struct {
		name string
		d    time.Duration
	}{
		{"maxScanBudget", maxScanBudget},
		{"hookDeadline", hookDeadline},
		{"hookWatchdog", hookWatchdog},
		{"tq_hook.sh _tq_deadline (+1 s)", wrapper},
		{"harness timeout - margin", harness - margin},
	}
	for i := 1; i < len(steps); i++ {
		if steps[i-1].d >= steps[i].d {
			t.Errorf("%s (%s) must be below %s (%s)", steps[i-1].name, steps[i-1].d, steps[i].name, steps[i].d)
		}
	}
	if scanBudget > maxScanBudget {
		t.Errorf("scanBudget %s over maxScanBudget %s", scanBudget, maxScanBudget)
	}
}

// syncBuffer is a bytes.Buffer safe for the watchdog's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

// A pre-tool-use run that never reaches a decision is a deny with exit 2,
// for both agents: an unknown flag, a stray argument.
func TestRunMainHookErrorsDeny(t *testing.T) {
	isolateHome(t)
	for _, args := range [][]string{
		{"claude-hook", "pre-tool-use", "--no-such-flag"},
		{"claude-hook", "pre-tool-use", "--agent", "codex", "--no-such-flag"},
		{"claude-hook", "pre-tool-use", "--json=maybe"},
	} {
		var out, errOut bytes.Buffer
		code := RunMain(args, &out, &errOut)
		if code != 2 || !strings.Contains(errOut.String(), "BLOCKED [tq/guard-error]: tq could not run the hook") {
			t.Errorf("%v: code=%d err=%q", args, code, errOut.String())
		}
		if strings.Contains(strings.Join(args, " "), "codex") {
			if dec, _ := codexAnswerJSON(t, out.String()); dec != "deny" {
				t.Errorf("%v: codex answer %q", args, out.String())
			}
		}
	}
	// Any other command keeps exit 1 for an error.
	var out, errOut bytes.Buffer
	if code := RunMain([]string{"doctor", "--no-such-flag"}, &out, &errOut); code != 1 {
		t.Errorf("doctor error: code %d", code)
	}
}

// The process watchdog answers a deny when the run is still going after
// hookWatchdog, whatever holds it (here a decision that never returns and a
// decision deadline set far out).
func TestRunMainWatchdogDenies(t *testing.T) {
	isolateHome(t)
	prevW, prevD, prevE, prevP := hookWatchdog, hookDeadline, hookExit, hookPolicyDecision
	release := make(chan struct{})
	hookWatchdog, hookDeadline = 150*time.Millisecond, time.Hour
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		<-release
		return policy.Decision{}
	}
	codes := make(chan int, 1)
	hookExit = func(c int) { codes <- c }
	t.Cleanup(func() {
		close(release)
		hookWatchdog, hookDeadline, hookExit, hookPolicyDecision = prevW, prevD, prevE, prevP
	})
	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })

	dir := testutil.TempDir(t)
	stdin := hookPayloadFor(t, dir, "ls")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(stdin)
	_ = w.Close()
	prevIn := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prevIn })

	var out, errOut syncBuffer
	go RunMain([]string{"claude-hook", "pre-tool-use", "--agent", "codex"}, &out, &errOut)
	select {
	case code := <-codes:
		if code != 2 || !strings.Contains(errOut.String(), "tq did not answer within") {
			t.Fatalf("code=%d err=%q", code, errOut.String())
		}
		if dec, _ := codexAnswerJSON(t, out.String()); dec != "deny" {
			t.Fatalf("codex answer %q", out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watchdog did not fire")
	}
}
