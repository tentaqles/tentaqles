package eventlog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// limitEvent is an event of the day file of testAt, with the given input, for the limit tests.
func limitEvent(id, kind string, in scrub.Clean) Event {
	return Event{
		V:       1,
		ID:      id,
		At:      testAt,
		Session: "s1",
		Host:    "claude",
		Tool:    "Bash",
		Kind:    kind,
		Cwd:     ".",
		Input:   in,
	}
}

// smallInput is a short input of the call, as the scrubber makes it.
func smallInput(t *testing.T, cmd string) scrub.Clean {
	t.Helper()
	return scrub.Input(json.RawMessage(`{"command":"` + cmd + `"}`))
}

// longText is a text of n copies of unit as one JSON string, made with scrub.Line. scrub.Input and
// scrub.Text cap their text at about 4000 characters, so the scrubbed constructors cannot make a line
// over 32 KiB from input or error; scrub.Line has no such cap, and the limit tests need one.
func longText(t *testing.T, unit string, n int) scrub.Clean {
	t.Helper()
	c, err := scrub.Line(strings.Repeat(unit, n))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// sizeTo makes the file at path exactly size bytes long, filled with zeros, as a sparse file: a stand-in
// for a day file that has grown to a fill level without writing it line by line.
func sizeTo(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

// fileHas reports how many times marker occurs in the file at path.
func fileHas(t *testing.T, path, marker string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(raw, []byte(marker))
}

// dropReasons returns the why of each line of the drops file of testAt, in order, and checks that each line
// has exactly the keys at and why.
func dropReasons(t *testing.T, home string) []string {
	t.Helper()
	path := filepath.Join(storeDir(home), "events", "drops-20261109.jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	var out []string
	for _, line := range readLines(t, path) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("drops line is not JSON: %v: %q", err, line)
		}
		if len(m) != 2 || m["at"] == nil || m["why"] == nil {
			t.Fatalf("drops line has keys other than at and why: %q", line)
		}
		out = append(out, m["why"].(string))
	}
	return out
}

// TestLobeLogCaps: at 8 MiB an ok event is dropped and a fail event is written; at 16 MiB both are dropped,
// one drops line each. The check is skipped when the lock is held elsewhere.
func TestLobeLogCaps(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	ctx := context.Background()
	log := filepath.Join(storeDir(home), testDayFile)

	sizeTo(t, log, 8<<20)
	ok := limitEvent("evt_00000001", KindOK, smallInput(t, "ls"))
	if err := Append(ctx, st, ok); err != nil {
		t.Fatalf("Append ok at 8 MiB = %v, want nil", err)
	}
	fail := limitEvent("evt_00000002", KindFail, smallInput(t, "x --two"))
	if err := Append(ctx, st, fail); err != nil {
		t.Fatalf("Append fail at 8 MiB = %v, want nil", err)
	}
	if n := fileHas(t, log, "evt_00000001"); n != 0 {
		t.Errorf("ok event written at 8 MiB (%d times)", n)
	}
	if n := fileHas(t, log, "evt_00000002"); n != 1 {
		t.Errorf("fail event written %d times at 8 MiB, want 1", n)
	}
	if got := dropReasons(t, home); strings.Join(got, ",") != "full" {
		t.Errorf("drops at 8 MiB = %v, want [full]", got)
	}

	sizeTo(t, log, 16<<20)
	if err := Append(ctx, st, limitEvent("evt_00000003", KindFail, smallInput(t, "x --three"))); err != nil {
		t.Fatalf("Append fail at 16 MiB = %v, want nil", err)
	}
	if err := Append(ctx, st, limitEvent("evt_00000004", KindOK, smallInput(t, "ls"))); err != nil {
		t.Fatalf("Append ok at 16 MiB = %v, want nil", err)
	}
	if n := fileHas(t, log, "evt_00000003") + fileHas(t, log, "evt_00000004"); n != 0 {
		t.Errorf("events written at 16 MiB (%d occurrences), want none", n)
	}
	if got := dropReasons(t, home); strings.Join(got, ",") != "full,full,full" {
		t.Errorf("drops after 16 MiB = %v, want [full full full]", got)
	}

	// The same file with the lock held elsewhere: the check is skipped and the fail is appended as before.
	other := openAcme(t)
	unlock, err := other.Lock(ctx)
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	defer unlock()
	if _, _, err := AppendChecked(ctx, st, limitEvent("evt_00000005", KindFail, smallInput(t, "x --five"))); err != nil {
		t.Fatalf("AppendChecked with the lock held = %v, want nil", err)
	}
	if n := fileHas(t, log, "evt_00000005"); n != 1 {
		t.Errorf("fail with the lock held written %d times, want 1 (the check is skipped)", n)
	}
}

// TestLobeLogBigLine: a line over 32 KiB has its input and error halved until it fits, at most four times.
// One that does not fit after four halvings is dropped with the reason big.
func TestLobeLogBigLine(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	ctx := context.Background()
	log := filepath.Join(storeDir(home), testDayFile)

	// 40000 ASCII runes of input: one halving makes the line fit.
	one := limitEvent("evt_0000000a", KindFail, longText(t, "a", 40000))
	// 40000 two-byte runes of input and of error: about 160 KB, three halvings (20 KB) make it fit. The cut
	// is by runes, so the stored texts stay valid UTF-8.
	three := limitEvent("evt_0000000b", KindFail, longText(t, "é", 40000))
	three.Error = longText(t, "é", 40000)
	for _, e := range []Event{one, three} {
		if err := Append(ctx, st, e); err != nil {
			t.Fatalf("Append %s = %v, want nil", e.ID, err)
		}
	}
	lines := readLines(t, log)
	if len(lines) != 2 {
		t.Fatalf("day file holds %d lines, want 2", len(lines))
	}
	// The stored texts are the Clean texts halved as many times as the line needed: the rune count of each
	// is the original's shifted right by that count.
	for _, c := range []struct {
		line   string
		e      Event
		halves int
	}{{lines[0], one, 1}, {lines[1], three, 3}} {
		if len(c.line)+1 > lineLimit {
			t.Errorf("line of %d bytes is over the limit", len(c.line)+1)
		}
		var m struct {
			Input string `json:"input"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(c.line), &m); err != nil {
			t.Fatalf("line does not parse: %v", err)
		}
		if got, want := utf8.RuneCountInString(m.Input), utf8.RuneCountInString(c.e.Input.String())>>c.halves; got != want {
			t.Errorf("input has %d runes, want %d (halved %d times)", got, want, c.halves)
		}
		if c.e.Error.String() != "" {
			if got, want := utf8.RuneCountInString(m.Error), utf8.RuneCountInString(c.e.Error.String())>>c.halves; got != want {
				t.Errorf("error has %d runes, want %d (halved %d times)", got, want, c.halves)
			}
		}
		if !utf8.ValidString(m.Input) || !utf8.ValidString(m.Error) {
			t.Errorf("a cut text is not valid UTF-8")
		}
	}
	if got := dropReasons(t, home); len(got) != 0 {
		t.Errorf("drops = %v, want none for lines that fit", got)
	}

	// 600 KB of input: four halvings leave about 37 KB, still over the limit. The line is dropped with big,
	// and nothing is an error.
	huge := limitEvent("evt_0000000c", KindFail, longText(t, "b", 600000))
	if err := Append(ctx, st, huge); err != nil {
		t.Fatalf("Append huge = %v, want nil (a big drop is not an error)", err)
	}
	if n := fileHas(t, log, "evt_0000000c"); n != 0 {
		t.Errorf("huge line written (%d times), want none", n)
	}
	if got := dropReasons(t, home); strings.Join(got, ",") != "big" {
		t.Errorf("drops = %v, want [big]", got)
	}
}

// TestLobeLogTornLineRepair: after five characters with no line break (a crash mid-line), the next event
// is a whole line of its own. Scan returns it and counts the torn line as one skip.
func TestLobeLogTornLineRepair(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	log := filepath.Join(storeDir(home), testDayFile)
	if err := os.MkdirAll(filepath.Dir(log), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte(`{"v":`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := Append(context.Background(), st, limitEvent("evt_0000000d", KindFail, smallInput(t, "x --torn"))); err != nil {
		t.Fatalf("Append after a torn line = %v, want nil", err)
	}
	lines := readLines(t, log)
	if len(lines) != 2 || lines[0] != `{"v":` {
		t.Fatalf("file lines = %q, want the torn text on line 1 and the event on line 2", lines)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &m); err != nil || m["id"] != "evt_0000000d" {
		t.Fatalf("line 2 is not the whole event: %v %q", err, lines[1])
	}

	var got []string
	_, skipped, err := Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	})
	if err != nil {
		t.Fatalf("Scan = %v", err)
	}
	if len(got) != 1 || skipped != 1 {
		t.Fatalf("Scan returned %d lines and skipped %d, want 1 and 1", len(got), skipped)
	}
}
