package cmd

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/serveinfo"
)

// A parent app (the desktop) holds the hub's stdin open; when it closes it, or dies, the hub
// shuts down the way Ctrl-C would.
func TestParentStdinContext_CancelsOnEOF(t *testing.T) {
	r, w := io.Pipe()
	ctx, stop := parentStdinContext(context.Background(), r)
	defer stop()

	_, _ = w.Write([]byte("ignored bytes do not stop the hub\n"))
	select {
	case <-ctx.Done():
		t.Fatal("cancelled before stdin closed")
	case <-time.After(50 * time.Millisecond):
	}
	_ = w.Close()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stdin closed but the context is still live")
	}
}

func TestParentStdinContext_FollowsTheParentContext(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := parentStdinContext(parent, r)
	defer stop()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the parent context was cancelled but this one is live")
	}
}

// End to end: a hub started by a parent app shuts down cleanly (serve record removed) when the
// parent closes its stdin, with no signal.
func TestServeStopsWhenTheParentClosesStdin(t *testing.T) {
	hubWS(t)
	t.Setenv(hubParentStdinEnv, "1")
	port := freePort(t)
	var out lockedBuf
	r, w := io.Pipe()
	root := NewRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(r)
	root.SetArgs([]string{"hub", "serve", "--port", itoa(port), "--no-notify"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(context.Background()) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := serveinfo.FindByPort(context.Background(), port); ok {
			break
		}
		if time.Now().After(deadline) {
			_ = w.Close()
			t.Fatalf("no live serve record:\n%s", out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop after its stdin closed")
	}
	if _, err := os.Stat(serveinfo.Path(port)); !os.IsNotExist(err) {
		t.Fatal("the serve record is removed on a parent stop")
	}
}

func TestParentStdinWatch_OnlyWithTheEnv(t *testing.T) {
	t.Setenv(hubParentStdinEnv, "")
	if parentStdinWanted() {
		t.Fatal("watching stdin without the env: a hub run with stdin from /dev/null would exit at once")
	}
	t.Setenv(hubParentStdinEnv, "1")
	if !parentStdinWanted() {
		t.Fatal("env set, watch not wanted")
	}
}
