package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/pkg/setupapi"
)

// The Board tab (spec B §10): find a hub that already serves the workspace, or start
// `tq hub serve` for it as a child of this app, and hand back the board URL. The frontend then
// navigates the app's own top-level webview there (no iframe: the hub's CSP keeps
// frame-ancestors 'none').

// BoardTarget is where the Board tab navigates.
type BoardTarget struct {
	URL     string
	Started bool // true when this app started the hub for this call
}

// boardWorkspaceRe is the workspace naming rule; it also keeps a name from ever reading as a
// flag on the child's command line.
var boardWorkspaceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// boardHubs owns discovery and the hubs this app started.
type boardHubs struct {
	find     func(ctx context.Context, ws string) (setupapi.Hub, bool)
	tqPath   func() string
	freePort func() (int, error)
	wait     time.Duration // how long a started hub may take to answer its first ping
	poll     time.Duration

	mu       sync.Mutex
	children map[string]*hubChild
}

type hubChild struct {
	cmd  *exec.Cmd
	out  *tailBuffer
	done chan struct{}
	err  error // set before done closes
}

func (c *hubChild) exited() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func newBoardHubs() *boardHubs {
	return &boardHubs{
		find:     setupapi.FindHub,
		tqPath:   setupapi.TQPath,
		freePort: freeLoopbackPort,
		wait:     20 * time.Second,
		poll:     150 * time.Millisecond,
		children: map[string]*hubChild{},
	}
}

// open returns the board URL for ws, starting a hub when none serves it.
func (b *boardHubs) open(ctx context.Context, ws string) (BoardTarget, error) {
	if !boardWorkspaceRe.MatchString(ws) {
		return BoardTarget{}, fmt.Errorf("workspace %q: name must match %s", ws, boardWorkspaceRe.String())
	}
	if h, ok := b.find(ctx, ws); ok {
		return BoardTarget{URL: setupapi.BoardURL(h, ws)}, nil
	}
	b.mu.Lock()
	ch := b.children[ws]
	if ch == nil || ch.exited() {
		var err error
		if ch, err = b.spawn(ws); err != nil {
			b.mu.Unlock()
			return BoardTarget{}, err
		}
		b.children[ws] = ch
	}
	b.mu.Unlock()
	return b.await(ctx, ws, ch)
}

func (b *boardHubs) spawn(ws string) (*hubChild, error) {
	path := b.tqPath()
	if path == "" {
		return nil, errors.New("tq is not installed: install it from the Setup tab, then open the board again")
	}
	port, err := b.freePort()
	if err != nil {
		return nil, fmt.Errorf("pick a port for the hub: %w", err)
	}
	cmd := exec.Command(path, "hub", "serve", "--workspace", ws, "--port", strconv.Itoa(port))
	out := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, out
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start tq hub serve: %w", err)
	}
	ch := &hubChild{cmd: cmd, out: out, done: make(chan struct{})}
	go func() {
		ch.err = cmd.Wait()
		close(ch.done)
	}()
	return ch, nil
}

// await polls for the started hub until it answers, exits or runs out of time.
func (b *boardHubs) await(ctx context.Context, ws string, ch *hubChild) (BoardTarget, error) {
	deadline := time.NewTimer(b.wait)
	defer deadline.Stop()
	tick := time.NewTicker(b.poll)
	defer tick.Stop()
	for {
		if h, ok := b.find(ctx, ws); ok {
			return BoardTarget{URL: setupapi.BoardURL(h, ws), Started: true}, nil
		}
		select {
		case <-ch.done:
			return BoardTarget{}, fmt.Errorf("tq hub serve exited before the board answered (%v)%s", ch.err, outputNote(ch.out.String()))
		case <-deadline.C:
			b.stop(ws, ch)
			return BoardTarget{}, fmt.Errorf("tq hub serve did not answer within %s; stopped it%s", b.wait, outputNote(ch.out.String()))
		case <-ctx.Done():
			return BoardTarget{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func outputNote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return ": " + s
}

// stop ends one child: a polite stop first where the OS has one, then a kill.
func (b *boardHubs) stop(ws string, ch *hubChild) {
	if !ch.exited() {
		interruptProcess(ch.cmd)
		select {
		case <-ch.done:
		case <-time.After(5 * time.Second):
			_ = ch.cmd.Process.Kill()
			<-ch.done
		}
	}
	b.mu.Lock()
	if b.children[ws] == ch {
		delete(b.children, ws)
	}
	b.mu.Unlock()
}

// stopAll ends every hub this app started (the app is closing; hubs started elsewhere stay).
func (b *boardHubs) stopAll() {
	b.mu.Lock()
	kids := make(map[string]*hubChild, len(b.children))
	for ws, ch := range b.children {
		kids[ws] = ch
	}
	b.mu.Unlock()
	for ws, ch := range kids {
		b.stop(ws, ch)
	}
}

// running counts the children still alive.
func (b *boardHubs) running() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, ch := range b.children {
		if !ch.exited() {
			n++
		}
	}
	return n
}

// freeLoopbackPort asks the OS for a free loopback port. A desktop-started hub takes its own
// port, so it never collides with a hub already on the default 7470 serving other workspaces.
func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// tailBuffer keeps the last max bytes written, for the error shown when a start fails.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append([]byte(nil), t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
