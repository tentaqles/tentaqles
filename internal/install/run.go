package install

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// DefaultRun runs path with args and env: no shell and no stdin. When ctx ends, the process
// and everything it started are stopped (a package manager that runs an installer that runs a
// helper leaves nothing behind).
func DefaultRun(ctx context.Context, path string, args, env []string) (string, string, error) {
	c := exec.CommandContext(ctx, path, args...)
	c.Env = env
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &limitWriter{w: &out}, &limitWriter{w: &errb}
	// A grandchild that keeps the pipes open must not hold tq past the deadline.
	c.WaitDelay = 2 * time.Second
	tree := prepareTree(c)
	if err := c.Start(); err != nil {
		return "", "", err
	}
	tree.attach(c)
	err := c.Wait()
	tree.release()
	return out.String(), errb.String(), err
}

// limitWriter keeps the first 64 KiB of a stream and discards the rest.
type limitWriter struct {
	w *bytes.Buffer
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if room := 64<<10 - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}
