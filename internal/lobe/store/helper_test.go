package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// TestHelperProcess is not a test: the test binary re-runs itself with -test.run=^TestHelperProcess$
// and LOBE_HELPER set to a mode, which makes this function do one job for the parent test. Without
// LOBE_HELPER it returns at once.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("LOBE_HELPER")
	if mode == "" {
		return
	}
	client := os.Getenv("LOBE_CLIENT")
	switch mode {
	case "append":
		helperAppend(t, client, mustAtoi(t, os.Getenv("LOBE_WRITER")))
	case "update":
		helperUpdate(t, client, os.Getenv("LOBE_WRITER"))
	case "hold":
		st, err := Open(client, ReadWrite)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		unlock, err := st.Lock(context.Background())
		if err != nil {
			t.Fatalf("lock: %v", err)
		}
		defer unlock()
		fmt.Println("HELD")
		time.Sleep(60 * time.Second)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

// helperAppend takes the lock around each of 500 appends to events/t.jsonl.
func helperAppend(t *testing.T, client string, writer int) {
	st, err := Open(client, ReadWrite)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for n := 0; n < 500; n++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		unlock, err := st.Lock(ctx)
		cancel()
		if err != nil {
			t.Fatalf("writer %d line %d: lock: %v", writer, n, err)
		}
		err = st.Append("events/t.jsonl", cleanJSON(t, fmt.Sprintf(`{"w":%d,"n":%d}`, writer, n)))
		unlock()
		if err != nil {
			t.Fatalf("writer %d line %d: append: %v", writer, n, err)
		}
	}
}

// helperUpdate adds its own key to sessions/s1.json under the lock.
func helperUpdate(t *testing.T, client, writer string) {
	st, err := Open(client, ReadWrite)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = st.Update(ctx, "sessions/s1.json", func(old []byte) (scrub.Clean, error) {
		m := map[string]any{}
		if len(old) > 0 {
			if err := json.Unmarshal(old, &m); err != nil {
				return scrub.Clean{}, err
			}
		}
		m["k"+writer] = writer
		b, err := json.Marshal(m)
		if err != nil {
			return scrub.Clean{}, err
		}
		return scrub.Input(json.RawMessage(b)), nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
}

// signalWriter collects a child's stdout and closes ready when the child prints HELD.
type signalWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func newSignalWriter() *signalWriter {
	return &signalWriter{ready: make(chan struct{})}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if bytes.Contains(w.buf.Bytes(), []byte("HELD")) {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

// helperCmd is a command that runs the test binary as the helper process for mode, with the client
// name in LOBE_CLIENT. Child processes inherit TQ_HOME from the parent's environment.
func helperCmd(mode, client string, extra ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "LOBE_HELPER="+mode, "LOBE_CLIENT="+client)
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

// startHolder starts a child that takes the store lock and waits until it reports it holds the lock.
func startHolder(t *testing.T, client string) (*exec.Cmd, *signalWriter) {
	t.Helper()
	cmd := helperCmd("hold", client)
	out := newSignalWriter()
	cmd.Stdout = out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	select {
	case <-out.ready:
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("holder never reported the lock; stderr: %s", cmd.Stderr.(*bytes.Buffer).String())
	}
	return cmd, out
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscan(s, &n); err != nil {
		t.Fatalf("bad number %q: %v", s, err)
	}
	return n
}

// cleanJSON scrubs a JSON text that the test wrote itself.
func cleanJSON(t *testing.T, s string) scrub.Clean {
	t.Helper()
	if !json.Valid([]byte(s)) {
		t.Fatalf("test bug: not JSON: %s", s)
	}
	return scrub.Input(json.RawMessage(s))
}
