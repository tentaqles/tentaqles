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
	out, errb := &limitWriter{}, &limitWriter{}
	c.Stdout, c.Stderr = out, errb
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

const (
	headMax = 16 << 10
	tailMax = 48 << 10
)

// limitWriter keeps what a stream said at the start (the first 16 KiB: a version line) and at
// the end (the last 48 KiB: a package manager's verdict comes last) and discards the middle.
type limitWriter struct {
	head, tail []byte
	cut        bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if room := headMax - len(l.head); room > 0 {
		take := min(room, len(p))
		l.head = append(l.head, p[:take]...)
		p = p[take:]
	}
	if len(p) > 0 {
		l.tail = append(l.tail, p...)
		if len(l.tail) > 2*tailMax {
			l.tail = append([]byte(nil), l.tail[len(l.tail)-tailMax:]...)
			l.cut = true
		}
	}
	return n, nil
}

// String is the head and the tail; when the middle was dropped the tail starts at a line start.
func (l *limitWriter) String() string {
	tail := l.tail
	if len(tail) > tailMax {
		tail = tail[len(tail)-tailMax:]
		l.cut = true
	}
	if !l.cut {
		return string(l.head) + string(tail)
	}
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return string(l.head) + "\n" + string(tail)
}
