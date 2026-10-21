package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Process runs an arbitrary command with the run's environment (tests, scripted tasks).
type Process struct {
	Command []string
	Grace   time.Duration
}

func (p Process) Start(ctx context.Context, req RunRequest, log io.Writer) (RunResult, error) {
	if len(p.Command) == 0 {
		return RunResult{}, errors.New("process adapter: no command configured")
	}
	r := req
	r.Env = append(append([]string(nil), req.Env...), "TQ_HUB_PROMPT="+req.Prompt)
	done := make(chan struct{})
	defer close(done)
	cmd := command(ctx, p.Command, r, p.Grace, done)
	cmd.Stdin = strings.NewReader(req.Prompt)
	t := &tail{max: 8 << 10}
	w := &locked{w: io.MultiWriter(log, t)}
	cmd.Stdout, cmd.Stderr = w, w
	code, err := exitCode(ctx, cmd.Run())
	return RunResult{ExitCode: code, StopReason: fmt.Sprintf("exit %d", code), Transcript: t.String()}, err
}
