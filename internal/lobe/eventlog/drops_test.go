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

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
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

// TestLobeLogDropsFile: each drop line has exactly the keys at and why; a payload full of marker strings
// leaves no marker in the drops file; the file stops at 64 KiB; when the drops file cannot be written, a
// limit refusal still returns no error (the hook would exit 0 and print nothing).
func TestLobeLogDropsFile(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	ctx := context.Background()
	const marker = "ZQMARK"
	log := filepath.Join(storeDir(home), testDayFile)
	drops := filepath.Join(storeDir(home), "events", "drops-20261109.jsonl")

	// A big line whose input is full of the marker: dropped with big, and the marker stays out of the drops file.
	big := limitEvent("evt_00000010", KindFail, longText(t, marker+" ", 100000))
	if v, _, err := AppendChecked(ctx, st, big); err != nil || v != VerdictDropped {
		t.Fatalf("AppendChecked of a big line = %v, %v; want dropped and no error", v, err)
	}
	// A full refusal at 16 MiB, with the marker in its error: dropped with full, the marker stays out too.
	sizeTo(t, log, 16<<20)
	full := limitEvent("evt_00000011", KindFail, smallInput(t, "ls"))
	full.Error = scrub.Input(json.RawMessage(`"` + marker + ` in the error"`))
	if v, _, err := AppendChecked(ctx, st, full); err != nil || v != VerdictDropped {
		t.Fatalf("AppendChecked at 16 MiB = %v, %v; want dropped and no error", v, err)
	}

	lines := readLines(t, drops)
	if len(lines) != 2 {
		t.Fatalf("drops file has %d lines, want 2", len(lines))
	}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("drop line is not JSON: %v", err)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if strings.Join(keys, ",") != "at,why" {
			t.Fatalf("drop keys = %v, want exactly at and why", keys)
		}
	}
	if n := fileHas(t, drops, marker); n != 0 {
		t.Fatalf("drops file holds the payload marker %d times, want 0", n)
	}

	// The file stops at 64 KiB: with it at the limit, a further drop writes nothing.
	if err := os.WriteFile(drops, []byte(strings.Repeat("x", dropsMax-1)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AppendChecked(ctx, st, full); err != nil {
		t.Fatalf("AppendChecked at the drops limit = %v, want nil", err)
	}
	if fi, err := os.Stat(drops); err != nil || fi.Size() != dropsMax {
		t.Fatalf("drops file = %v, %v; want unchanged at %d bytes", fi, err, dropsMax)
	}

	// The drops file cannot be written (its path is a folder): a full refusal still returns no error.
	if err := os.Remove(drops); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(drops, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Append(ctx, st, full); err != nil {
		t.Fatalf("Append with an unwritable drops file = %v, want nil", err)
	}
	if _, _, err := AppendChecked(ctx, st, full); err != nil {
		t.Fatalf("AppendChecked with an unwritable drops file = %v, want nil", err)
	}
}

// TestLobeLogDiskDropCounted: a write that fails while the drops file is writable is counted with the reason
// disk. The write error is returned as well, so the caller can classify it.
func TestLobeLogDiskDropCounted(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	// The day file is a folder, so the event's write fails; the drops file is an ordinary file.
	if err := os.MkdirAll(filepath.Join(storeDir(home), testDayFile), 0700); err != nil {
		t.Fatal(err)
	}
	v, _, err := AppendChecked(context.Background(), st, limitEvent("evt_00000012", KindFail, smallInput(t, "ls")))
	if err == nil {
		t.Fatal("AppendChecked into a folder = nil error, want the write error")
	}
	if v != VerdictDropped {
		t.Fatalf("verdict = %v, want dropped", v)
	}
	drops := filepath.Join(storeDir(home), "events", "drops-20261109.jsonl")
	if got := readLines(t, drops); len(got) != 1 || !strings.Contains(got[0], `"why":"disk"`) {
		t.Fatalf("drops file = %q, want one line with why disk", got)
	}
}
