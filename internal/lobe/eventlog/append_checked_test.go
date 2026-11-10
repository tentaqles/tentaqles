package eventlog

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// errBuild is the failure the test makes the reference builder return.
var errBuild = errors.New("reference line cannot be built")

// repeatPair is a first failure and a repeat of it ten seconds later, under another call id: the same
// session, tool, kind, input and fp, so the second is a repeat of the first.
func repeatPair() (Event, Event) {
	return dedupeBase(), later(dedupeBase(), "evt_b")
}

// dayLines returns the lines of the day file e is stored in, under home.
func dayLines(t *testing.T, home string, e Event) []string {
	t.Helper()
	rel, err := FileFor(e.At)
	if err != nil {
		t.Fatal(err)
	}
	return readLines(t, filepath.Join(storeDir(home), filepath.FromSlash(rel)))
}

// TestLobeAppendCheckedFallsBackToFullLine: when the reference line cannot be built, the full line is written
// instead and the verdict is new. A lost event costs more than a duplicate.
func TestLobeAppendCheckedFallsBackToFullLine(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	first, second := repeatPair()
	if v, _, err := AppendChecked(context.Background(), st, first); err != nil || v != VerdictNew {
		t.Fatalf("first append = %v, %v; want new and no error", v, err)
	}

	old := buildRefLine
	buildRefLine = func(Event) (scrub.Clean, error) { return scrub.Clean{}, errBuild }
	t.Cleanup(func() { buildRefLine = old })

	v, firstID, err := AppendChecked(context.Background(), st, second)
	if err != nil {
		t.Fatalf("AppendChecked = %v, want no error", err)
	}
	if v != VerdictNew || firstID != "" {
		t.Errorf("verdict %v firstID %q, want new and no first id (the full line was written)", v, firstID)
	}
	lines := dayLines(t, home, second)
	if len(lines) != 2 {
		t.Fatalf("file holds %d lines, want 2 (the full line was not written)", len(lines))
	}
	if strings.Contains(lines[1], `"dup"`) || !strings.Contains(lines[1], `"input"`) {
		t.Errorf("second line is not a full line: %s", lines[1])
	}
}

// TestLobeAppendCheckedLockHeldWritesFull: with the store lock held by another holder, the check is skipped
// and the full line is appended, although the same content is a repeat in the tail.
func TestLobeAppendCheckedLockHeldWritesFull(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	first, second := repeatPair()
	if _, _, err := AppendChecked(context.Background(), st, first); err != nil {
		t.Fatal(err)
	}

	unlock, err := st.Lock(context.Background())
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	v, firstID, err := AppendChecked(ctx, st, second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("AppendChecked while held = %v, want nil", err)
	}
	if v != VerdictNew || firstID != "" {
		t.Errorf("verdict %v firstID %q while held, want new and no first id", v, firstID)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("AppendChecked took %v while the lock was held, limit 300ms", elapsed)
	}
	lines := dayLines(t, home, second)
	if len(lines) != 2 {
		t.Fatalf("file holds %d lines, want 2 full lines", len(lines))
	}
	if strings.Contains(lines[1], `"dup"`) || !strings.Contains(lines[1], `"input"`) {
		t.Errorf("second line is not a full line: %s", lines[1])
	}
}
