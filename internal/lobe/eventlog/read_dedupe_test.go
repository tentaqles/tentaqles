package eventlog

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLobeReaderKeepsTheFirst(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	events := filepath.Join(storeDir(home), "events")
	first := `{"session":"s1","id":"evt_x","n":1}`
	writeFile(t, filepath.Join(events, "events-20261108.jsonl"),
		first+"\n"+`{"session":"s1","id":"evt_x","n":2}`+"\n"+`{"session":"s2","id":"evt_x","n":3}`+"\n")
	// The same pair in a later day file is still a second line of the pair: it is not passed.
	writeFile(t, filepath.Join(events, "events-20261109.jsonl"),
		`{"session":"s1","id":"evt_x","n":4}`+"\n"+`{"n":5}`+"\n"+`{"n":6}`+"\n")

	var got []string
	if _, _, err := Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{first, `{"session":"s2","id":"evt_x","n":3}`, `{"n":5}`, `{"n":6}`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan lines = %q, want %q", got, want)
	}
}

// tailLine is one event line of a fixed length: every line of the test file is the same size, so a
// window can be placed exactly on a line start or inside a line.
func tailLine(n int) string {
	return fmt.Sprintf(`{"v":1,"id":"evt_%06d","at":"2026-11-10T10:00:00Z","session":"s1","host":"claude","tool":"Bash","kind":"fail","cwd":".","input":"{\"n\":%06d}"}`, n, n)
}

func TestLobeTailEventsDropsPartialLine(t *testing.T) {
	home := setupHome(t)
	path := filepath.Join(storeDir(home), "events", "events-20261110.jsonl")
	const total = 1000
	var b strings.Builder
	for n := 1; n <= total; n++ {
		b.WriteString(tailLine(n) + "\n")
	}
	lineLen := len(tailLine(1)) + 1
	// An unterminated last line is not whole and is dropped.
	b.WriteString(`{"v":1,"id":"evt_partial"`)
	writeFile(t, path, b.String())
	size := b.Len()

	// The last 64 KiB: the partial first line is dropped, the ids run on to the last whole line.
	got, err := TailEvents(path, EventTailBytes)
	if err != nil {
		t.Fatalf("TailEvents: %v", err)
	}
	if len(got) == 0 || len(got) >= total {
		t.Fatalf("TailEvents returned %d events, want some but not all of %d", len(got), total)
	}
	if last := got[len(got)-1].ID; last != fmt.Sprintf("evt_%06d", total) {
		t.Errorf("last event = %s, want evt_%06d", last, total)
	}
	first := total - len(got) + 1
	for i, e := range got {
		if e.ID != fmt.Sprintf("evt_%06d", first+i) {
			t.Fatalf("event %d is %s: ids are not contiguous to the end", i, e.ID)
		}
	}
	if got[0].Input != fmt.Sprintf(`{"n":%06d}`, first) {
		t.Errorf("first event input = %q", got[0].Input)
	}
	if n := len(got) * lineLen; n > EventTailBytes {
		t.Errorf("%d lines hold %d bytes, more than the window", len(got), n)
	}

	// Line n starts at byte (n-1)*lineLen. The window starts exactly at the start of line k: lines k to
	// total are returned, and the unterminated last line is not.
	const k = 500
	exact, err := TailEvents(path, size-(k-1)*lineLen)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != total-k+1 || exact[0].ID != fmt.Sprintf("evt_%06d", k) {
		t.Fatalf("window on a line start gave %d events, want %d from evt_%06d", len(exact), total-k+1, k)
	}

	// The window starts 5 bytes into line k: line k is partial and dropped, so k+1 is first.
	mid, err := TailEvents(path, size-((k-1)*lineLen+5))
	if err != nil {
		t.Fatal(err)
	}
	if len(mid) != total-k || mid[0].ID != fmt.Sprintf("evt_%06d", k+1) {
		t.Fatalf("window inside a line gave %d events, want %d from evt_%06d", len(mid), total-k, k+1)
	}
}

func TestLobeTailEventsMissingFileAndZeroWindow(t *testing.T) {
	home := setupHome(t)
	got, err := TailEvents(filepath.Join(storeDir(home), "events", "events-20991231.jsonl"), EventTailBytes)
	if err != nil || len(got) != 0 {
		t.Errorf("missing file: %d events, err %v; want none and no error", len(got), err)
	}
	if got, err := TailEvents("unused", 0); err != nil || got != nil {
		t.Errorf("zero window: %v, %v; want nil, nil", got, err)
	}
}
