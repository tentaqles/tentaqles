package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tentaqles/tentaqles/internal/commitscan"
)

// Bounded git for the hook rules: hooks must never stall, so every git call
// runs under a deadline and the caller never waits for a pipe that a
// grandchild process may still hold open.

// scanBudget bounds all scanning work in one hook invocation: every git call
// and every byte scanned by the commit and push rules share it (hooks must
// never stall). The plugin's PreToolUse timeout is 15 s; when Claude Code
// kills a hook the call proceeds, so the budget keeps a wide margin for
// process start, the other rules and the Jev call. Tests lower it;
// TQ_HOOK_SCAN_BUDGET (a Go duration, at most 12s) overrides the default.
var scanBudget = 8 * time.Second

// maxScanBudget keeps a 5 s margin under the plugin's 15 s PreToolUse
// timeout for process start, the identity lookups and the Jev call.
const maxScanBudget = 10 * time.Second

const scanTimedOut = "scan did not finish in time"

// hookScanBudget is the budget for this invocation.
func hookScanBudget() time.Duration {
	if v := strings.TrimSpace(os.Getenv("TQ_HOOK_SCAN_BUDGET")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			if d > maxScanBudget {
				d = maxScanBudget
			}
			return d
		}
	}
	return scanBudget
}

var (
	gitBinOnce sync.Once
	gitBinPath = "git"
)

// gitBinary resolves the real git executable once. On Windows, the launcher
// in Git\cmd\git.exe starts the real git as a child, so killing it leaves the
// child holding our pipe; prefer the real binary next to it.
func gitBinary() string {
	gitBinOnce.Do(func() {
		p, err := exec.LookPath("git")
		if err != nil {
			return
		}
		gitBinPath = p
		if runtime.GOOS != "windows" {
			return
		}
		dir := filepath.Dir(p)
		if !strings.EqualFold(filepath.Base(dir), "cmd") {
			return
		}
		root := filepath.Dir(dir)
		for _, c := range []string{
			filepath.Join(root, "mingw64", "bin", "git.exe"),
			filepath.Join(root, "bin", "git.exe"),
		} {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				gitBinPath = c
				return
			}
		}
	})
	return gitBinPath
}

type limitBuf struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	limit  int
	over   atomic.Bool
	onOver func()
}

func (b *limitBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len()+len(p) > b.limit {
		if room := b.limit - b.buf.Len(); room > 0 && !b.over.Load() {
			b.buf.Write(p[:room]) // keep the head, so a caller can sniff it
		}
		if b.over.CompareAndSwap(false, true) && b.onOver != nil {
			b.onOver()
		}
		return len(p), nil // discard the rest; the caller sees tooBig
	}
	return b.buf.Write(p)
}

func (b *limitBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runBounded runs bin in dir and returns its stdout (at most limit bytes). It
// returns as soon as ctx ends, killing the process without waiting for the
// output pipe to drain.
func runBounded(ctx context.Context, bin, dir string, limit int, args ...string) (out string, tooBig bool, err error) {
	inner, cancel := context.WithCancel(ctx)
	defer cancel()
	c := exec.CommandContext(inner, bin, args...)
	c.Dir = dir
	c.WaitDelay = 2 * time.Second
	buf := &limitBuf{limit: limit, onOver: cancel}
	c.Stdout = buf
	if err := c.Start(); err != nil {
		return "", false, err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case werr := <-done:
		if buf.over.Load() {
			return buf.String(), true, nil // the first limit bytes
		}
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if werr != nil {
			return "", false, werr
		}
		return buf.String(), false, nil
	case <-inner.Done():
		_ = c.Process.Kill()
		if buf.over.Load() && ctx.Err() == nil {
			return buf.String(), true, nil
		}
		return "", false, ctx.Err()
	}
}

func runGitBounded(ctx context.Context, cwd string, limit int, args ...string) (string, bool, error) {
	return runBounded(ctx, gitBinary(), cwd, limit, args...)
}

// maxCommitGitOutput caps one git call of the commit rules; above it the
// rule asks instead of scanning (see commitscan.MaxTotalBytes).
const maxCommitGitOutput = commitscan.MaxTotalBytes

// commitRunner adapts pushGit to commitscan.Runner under ctx.
func commitRunner(ctx context.Context) commitscan.Runner {
	return func(dir string, args ...string) (string, error) {
		out, big, err := pushGit(ctx, dir, maxCommitGitOutput, args...)
		if err != nil {
			return "", err
		}
		if big {
			return "", commitscan.ErrTooLarge
		}
		return out, nil
	}
}
