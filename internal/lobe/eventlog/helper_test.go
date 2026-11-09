package eventlog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
	"github.com/tentaqles/tentaqles/internal/lobe/store"
)

const (
	writers   = 20
	perWriter = 500
)

// TestHelperProcess is not a test: the test binary re-runs itself with -test.run=^TestHelperProcess$
// and EVENTLOG_HELPER set to a mode, which makes this function do one job for the parent test.
// Without EVENTLOG_HELPER it returns at once.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("EVENTLOG_HELPER")
	if mode == "" {
		return
	}
	w, _ := strconv.Atoi(os.Getenv("EVENTLOG_WRITER"))
	st, err := store.Open("acme", store.ReadWrite)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	switch mode {
	case "nolock":
		helperNoLock(t, st, w)
	case "event":
		helperEvent(t, st, w, os.Getenv("EVENTLOG_SIDE"))
	case "hold":
		helperHold(t, st)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

// helperNoLock appends perWriter lines straight through the store, never taking the lock.
func helperNoLock(t *testing.T, st *store.Store, w int) {
	type rec struct {
		W   int    `json:"w"`
		N   int    `json:"n"`
		Pad string `json:"pad"`
	}
	pad := strings.Repeat("x", 200)
	for n := 0; n < perWriter; n++ {
		line, err := scrub.Line(rec{W: w, N: n, Pad: pad})
		if err != nil {
			t.Fatalf("writer %d line %d: line: %v", w, n, err)
		}
		if err := st.Append("events/nolock.jsonl", line); err != nil {
			t.Fatalf("writer %d line %d: append: %v", w, n, err)
		}
	}
}

// helperEvent appends perWriter events through Append and writes each call's duration in
// microseconds, one per line, to a side file outside the store.
func helperEvent(t *testing.T, st *store.Store, w int, side string) {
	f, err := os.Create(filepath.Join(side, fmt.Sprintf("dur-%d.txt", w)))
	if err != nil {
		t.Fatalf("writer %d: side file: %v", w, err)
	}
	defer f.Close()
	session := fmt.Sprintf("p%d", w)
	for n := 0; n < perWriter; n++ {
		in := scrub.Input(json.RawMessage(fmt.Sprintf(`{"command":"false %d"}`, n)))
		e := Event{
			V:       1,
			ID:      ID("claude", session, fmt.Sprintf("t%d", n)),
			At:      testAt,
			Session: session,
			Host:    "claude",
			Tool:    "Bash",
			Kind:    KindFail,
			Cwd:     ".",
			Input:   in,
		}
		start := time.Now()
		err = Append(context.Background(), st, e)
		d := time.Since(start)
		if err != nil {
			t.Fatalf("writer %d line %d: Append: %v", w, n, err)
		}
		fmt.Fprintf(f, "%d\n", d.Microseconds())
	}
}

// helperHold takes the store lock, reports it, holds it for two seconds and releases it.
func helperHold(t *testing.T, st *store.Store) {
	unlock, err := st.Lock(context.Background())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	fmt.Println("HELD")
	time.Sleep(2 * time.Second)
	unlock()
}

// helperCmd is the test binary run as a helper process for mode.
func helperCmd(mode string, writer int, side string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(),
		"EVENTLOG_HELPER="+mode,
		"EVENTLOG_WRITER="+strconv.Itoa(writer),
		"EVENTLOG_SIDE="+side,
	)
	return cmd
}
