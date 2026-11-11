package eventlog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// dropsAt is a time whose UTC date is 2026-11-09, used by the tests that do not check the date.
func dropsAt(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-11-09T14:03:11Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// TestLobeDropsAppendOneLine: one AppendDrop gives a drops file of the UTC day with exactly one line, and
// that line is JSON with exactly the keys at and why.
func TestLobeDropsAppendOneLine(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	at := dropsAt(t)
	if err := AppendDrop(context.Background(), st, DropNoExit, at); err != nil {
		t.Fatalf("AppendDrop = %v, want nil", err)
	}
	path := filepath.Join(storeDir(home), "events", "drops-20261109.jsonl")
	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("drops file has %d lines, want 1: %q", len(lines), lines)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("line is not JSON: %v: %q", err, lines[0])
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "at,why" {
		t.Fatalf("keys = %v, want exactly at and why", keys)
	}
	if m["why"] != "no-exit" {
		t.Fatalf("why = %v, want no-exit", m["why"])
	}
	if m["at"] != "2026-11-09T14:03:11Z" {
		t.Fatalf("at = %v, want 2026-11-09T14:03:11Z", m["at"])
	}
}

// TestLobeDropsStopsAt64KiB: with the drops file already 64 KiB, AppendDrop writes nothing and returns nil.
func TestLobeDropsStopsAt64KiB(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	at := dropsAt(t)
	dir := filepath.Join(storeDir(home), "events")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "drops-20261109.jsonl")
	full := strings.Repeat("x", dropsMax-1) + "\n"
	if err := os.WriteFile(path, []byte(full), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AppendDrop(context.Background(), st, DropNoExit, at); err != nil {
		t.Fatalf("AppendDrop = %v, want nil", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != dropsMax {
		t.Fatalf("size = %d after AppendDrop, want %d unchanged", fi.Size(), dropsMax)
	}
}

// TestLobeDropsUnknownReasonRefused: a reason that is not known is refused with an error, and no file is made.
func TestLobeDropsUnknownReasonRefused(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	at := dropsAt(t)
	if err := AppendDrop(context.Background(), st, "boom", at); err == nil {
		t.Fatal("AppendDrop with reason boom = nil, want an error")
	}
	path := filepath.Join(storeDir(home), "events", "drops-20261109.jsonl")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("drops file exists after a refused reason (stat err = %v)", err)
	}
}

// TestLobeDropsUTCDate: the file is named by the UTC date, so 23:30 at minus five hours is the next day.
func TestLobeDropsUTCDate(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	at, err := time.Parse(time.RFC3339, "2026-11-10T23:30:00-05:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := DropsFileFor(at); got != "events/drops-20261111.jsonl" {
		t.Fatalf("DropsFileFor = %q, want events/drops-20261111.jsonl", got)
	}
	if err := AppendDrop(context.Background(), st, DropNoExit, at); err != nil {
		t.Fatalf("AppendDrop = %v, want nil", err)
	}
	path := filepath.Join(storeDir(home), "events", "drops-20261111.jsonl")
	if lines := readLines(t, path); len(lines) != 1 {
		t.Fatalf("drops-20261111.jsonl has %d lines, want 1", len(lines))
	}
}
