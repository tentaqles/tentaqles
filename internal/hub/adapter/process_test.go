package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// TestMain doubles as the helper process the process adapter runs (the test binary re-executed).
func TestMain(m *testing.M) {
	switch os.Getenv("TQ_HUB_HELPER") {
	case "echo":
		prompt, _ := io.ReadAll(os.Stdin)
		wd, _ := os.Getwd()
		fmt.Printf("prompt=%s env=%s wd=%s\n", prompt, os.Getenv("TQ_HUB_PROMPT"), filepath.Base(wd))
		os.Exit(0)
	case "fail":
		fmt.Println("boom on stdout")
		fmt.Fprintln(os.Stderr, "boom on stderr")
		os.Exit(3)
	case "sleep":
		fmt.Printf("sleeping pid=%d\n", os.Getpid())
		time.Sleep(2 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// isolate keeps a test off the real state and out of any agent session's markers.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"TQ_HOME", "HOME", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "TQ_HUB_TASK", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

// pidAlive reports whether pid still runs 5 s after the caller saw its run
// end. A killed process can take a moment to finish exiting (TerminateProcess
// is asynchronous on Windows, and a grandchild is killed by taskkill after the
// direct child): the check waits on that condition with a deadline instead of
// sampling once (CI on Windows: "helper pid still alive after Start returned",
// where the exited helper was still openable through another handle).
func pidAlive(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for processRunning(pid) {
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func helperReq(t *testing.T, name, prompt string) RunRequest {
	isolate(t)
	dir := t.TempDir()
	return RunRequest{Workspace: "acme", Dir: dir, Prompt: prompt, Env: append(os.Environ(), "TQ_HUB_HELPER="+name)}
}

func TestProcessAdapterRunsInDirWithPrompt(t *testing.T) {
	var log syncBuf
	req := helperReq(t, "echo", "do it")
	res, err := Process{Command: []string{os.Args[0]}}.Start(context.Background(), req, &log)
	if err != nil {
		t.Fatal(err)
	}
	want := "prompt=do it env=do it wd=" + filepath.Base(req.Dir)
	if res.ExitCode != 0 || !strings.Contains(log.String(), want) || !strings.Contains(res.Transcript, want) {
		t.Fatalf("res %+v log %q", res, log.String())
	}
	if res.SessionID != "" || res.Usage != (Usage{}) || res.CostKnown {
		t.Fatal("the process adapter has no session and no usage")
	}
}

func TestProcessAdapterReportsExitCode(t *testing.T) {
	var log syncBuf
	res, err := Process{Command: []string{os.Args[0]}}.Start(context.Background(), helperReq(t, "fail", ""), &log)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.StopReason != "exit 3" || !strings.Contains(log.String(), "boom on stderr") {
		t.Fatalf("res %+v log %q", res, log.String())
	}
}

func TestProcessAdapterCancelTerminates(t *testing.T) {
	var log syncBuf
	grace := time.Second
	// exec's own fallback (WaitDelay = grace + waitDelaySlack) would kill the child only after a
	// minute, so a stop well before that is terminate's work. A fixed "within grace + 3 s" bound
	// measured how fast taskkill starts on a loaded runner instead (7.4 s under load).
	prevSlack := waitDelaySlack
	waitDelaySlack = time.Minute
	t.Cleanup(func() { waitDelaySlack = prevSlack })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	req := helperReq(t, "sleep", "")
	start := time.Now()
	go func() {
		_, err := Process{Command: []string{os.Args[0]}, Grace: grace}.Start(ctx, req, &log)
		done <- err
	}()
	for !strings.Contains(log.String(), "sleeping") {
		if time.Since(start) > 20*time.Second {
			t.Fatal("helper never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, after, _ := strings.Cut(log.String(), "sleeping pid=")
	pid, perr := strconv.Atoi(strings.TrimSpace(strings.SplitN(after, "\n", 2)[0]))
	if perr != nil {
		t.Fatalf("helper pid in %q", log.String())
	}
	cancelled := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled run returns the context error")
		}
		// terminate must do the work: without it WaitDelay (grace + a minute) is what would end the child
		if d := time.Since(cancelled); d > grace+waitDelaySlack/2 {
			t.Fatalf("took %v to stop: terminate did not end the child, exec's WaitDelay fallback did", d)
		}
		if pidAlive(pid) {
			t.Fatalf("helper pid %d still running 5 s after Start returned", pid)
		}
	case <-time.After(grace + waitDelaySlack + 30*time.Second):
		t.Fatal("cancel did not stop the process")
	}
}

func TestLaunchEnv(t *testing.T) {
	isolate(t)
	base := []string{"PATH=/bin", "TENTAQLES_HEADLESS_CHILD=1", "TQ_BUILD_ACTOR=hub:x", "TQ_BUILD_TOKEN=abc", "TENTAQLES_HEADLESS=0", "TQ_HUB_TASK=old"}
	got := LaunchEnv(base, `{"hub":"c-abcdefghijkl"}`)
	joined := strings.Join(got, "\n")
	for _, bad := range []string{"TENTAQLES_HEADLESS_CHILD", "TQ_BUILD_ACTOR", "TQ_BUILD_TOKEN", "TENTAQLES_HEADLESS=0", "TQ_HUB_TASK=old"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("%s survived: %v", bad, got)
		}
	}
	if !strings.Contains(joined, "TENTAQLES_HEADLESS=1") || !strings.Contains(joined, `TQ_HUB_TASK={"hub":"c-abcdefghijkl"}`) || !strings.Contains(joined, "PATH=/bin") {
		t.Fatalf("env %v", got)
	}
}

// spec E §6.4: a hub round runs with TQ_MODE set to the workspace's
// guard.headless_mode (carried by envplan.Environ), default when absent or
// invalid; an inherited TQ_MODE or TQ_ENABLED never reaches the round.
func TestLaunchEnvHeadlessMode(t *testing.T) {
	isolate(t)
	cases := []struct {
		base []string
		want string
	}{
		{[]string{"PATH=/bin"}, "default"},
		{[]string{"PATH=/bin", "TQ_MODE=off", "TQ_ENABLED=0"}, "default"},
		{[]string{"PATH=/bin", envplan.HeadlessModeVar + "=strict"}, "strict"},
		{[]string{"PATH=/bin", envplan.HeadlessModeVar + "=relaxed", "TQ_MODE=off"}, "relaxed"},
		{[]string{"PATH=/bin", envplan.HeadlessModeVar + "=custom"}, "custom"},
		{[]string{"PATH=/bin", envplan.HeadlessModeVar + "=off"}, "default"},
		{[]string{"PATH=/bin", envplan.HeadlessModeVar + "=bogus"}, "default"},
	}
	for _, c := range cases {
		got := LaunchEnv(c.base, `{"hub":"c-x"}`)
		var modes []string
		for _, kv := range got {
			k, v, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(k) {
			case "TQ_MODE":
				modes = append(modes, v)
			case "TQ_ENABLED", envplan.HeadlessModeVar:
				t.Errorf("%v: %s reached the round", c.base, k)
			}
		}
		if len(modes) != 1 || modes[0] != c.want {
			t.Errorf("%v: TQ_MODE %v, want [%s]", c.base, modes, c.want)
		}
	}
}
