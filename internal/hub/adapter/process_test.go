package adapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		p.Release()
		return true // FindProcess opens the process on Windows: success means it still exists
	}
	return p.Signal(syscall.Signal(0)) == nil
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
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := Process{Command: []string{os.Args[0]}, Grace: grace}.Start(ctx, helperReq(t, "sleep", ""), &log)
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
		// terminate must do the work: without it WaitDelay (grace + 5s) is what would end the child
		if d := time.Since(cancelled); d > grace+3*time.Second {
			t.Fatalf("took %v to stop, terminate did not act within the grace period", d)
		}
		if pidAlive(pid) {
			t.Fatalf("helper pid %d still alive after Start returned", pid)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancel did not stop the process within the grace period")
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
