// Package adapter runs one round of an agent for the hub runner: the contract of
// spec §8.3, the plain-process adapter used by tests and scripted tasks, and the
// headless `claude -p --output-format stream-json --verbose` adapter.
package adapter

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
)

// DefaultGrace is how long a cancelled run gets between the termination signal and the kill.
const DefaultGrace = 10 * time.Second

type Adapter interface {
	// Start launches one round in req.Dir with req.Env, writes stdout/stderr to log, and
	// returns when the process exits. It never blocks on a prompt.
	Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error)
}

type RunRequest struct {
	Workspace, Dir, Prompt, SessionID string
	Env                               []string
	AllowedTools                      []string
	// Limit is what is left of the run's budget for this round; the claude adapter stops the round
	// once its spend reaches it. A zero field is no limit.
	Limit Limit
	// Classes are the workspace's data classes, used to mask the progress lines (empty: every detector).
	Classes []string
}

// Limit is a round's share of the run budget: tokens (input + output + cache write), dollars, minutes.
type Limit struct {
	Tokens  int64
	USD     float64
	Minutes float64
}

type Usage struct {
	Input, Output, CacheRead, CacheWrite int64
}

type PermissionDenial struct {
	Tool  string         `json:"tool_name"`
	Input map[string]any `json:"tool_input"`
}

type RunResult struct {
	ExitCode          int
	SessionID         string
	Usage             Usage
	CostUSD           float64
	StopReason        string
	Transcript        string
	CostKnown         bool
	UsageKnown        bool
	Model             string
	NumTurns          int
	PermissionDenials []PermissionDenial
	// OverBudget names the limit (tokens, usd or minutes) at which the adapter stopped the round
	// inside it; Usage and CostUSD then hold what the round spent up to the stop.
	OverBudget string
}

// command builds the process; done must be closed by the caller once cmd.Run has returned, so a
// late kill never reaches a process that already exited (and whose PID may have been reused).
func command(ctx context.Context, argv []string, req RunRequest, grace time.Duration, done <-chan struct{}) *exec.Cmd {
	if grace <= 0 {
		grace = DefaultGrace
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	prepare(cmd)
	cmd.Cancel = func() error { return terminate(cmd.Process, grace, done) }
	cmd.WaitDelay = grace + waitDelaySlack
	return cmd
}

// waitDelaySlack is how long after the grace period exec's own fallback kills a child that
// terminate did not stop. A test raises it so that only terminate can end the child in time.
var waitDelaySlack = 5 * time.Second

// exitCode maps cmd.Run's error: a non-zero exit is a result, a failure to start or a cancel is an error.
func exitCode(ctx context.Context, err error) (int, error) {
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { t.mu.Lock(); defer t.mu.Unlock(); return string(t.b) }

// locked serializes writes from stdout and stderr into one log writer.
type locked struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *locked) Write(p []byte) (int, error) { l.mu.Lock(); defer l.mu.Unlock(); return l.w.Write(p) }

func envKey(kv string) string {
	k, _, _ := strings.Cut(kv, "=")
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}

// LaunchEnv is the environment of every hub-launched round: the workspace's plan (base), never a
// hub token, never the plugin's headless-child marker (the session hooks must run), plus the
// headless switch, the card and the guard mode (spec E §6.4): TQ_MODE is the workspace's
// guard.headless_mode, which envplan.Environ carries in envplan.HeadlessModeVar, or default when
// it is absent or not a stored mode. An inherited TQ_MODE or TQ_ENABLED never reaches the round,
// so a hub started from an off window still runs guarded. A mode looser than default is clamped
// by the workspace grant in the hook, as for any launch.
func LaunchEnv(base []string, hubTask string) []string {
	drop := map[string]bool{"TENTAQLES_HEADLESS_CHILD": true, "TQ_BUILD_ACTOR": true, "TQ_BUILD_TOKEN": true,
		"TENTAQLES_HEADLESS": true, "TQ_HUB_TASK": true,
		guardcfg.EnvMode: true, "TQ_ENABLED": true, envplan.HeadlessModeVar: true}
	mode := guardcfg.ModeDefault
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		k := strings.ToUpper(envKey(kv))
		if k == envplan.HeadlessModeVar {
			_, v, _ := strings.Cut(kv, "=")
			if v = strings.TrimSpace(v); guardcfg.ValidStoredMode(v) {
				mode = v
			}
		}
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return append(out, "TENTAQLES_HEADLESS=1", "TQ_HUB_TASK="+hubTask, guardcfg.EnvMode+"="+mode)
}
