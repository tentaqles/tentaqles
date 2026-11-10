package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFile writes content to path, creating its folder.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// objLine is a JSON object line holding n.
func objLine(n int) string {
	return fmt.Sprintf(`{"n":%d}`, n)
}

// brokenFixture is a day file with a blank line, a line that is not JSON and a last line with no line
// break: the whole lines are n=1 and n=2, and two lines are broken.
const brokenFixture = `{"n":1}` + "\n" + "\n" + "not json\n" + `{"n":2}` + "\n" + `{"n":3}`

func TestLobeLogReaderSkipsBrokenLines(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	path := filepath.Join(storeDir(home), filepath.FromSlash(testDayFile))
	writeFile(t, path, brokenFixture)
	want := []string{objLine(1), objLine(2)}

	var got []string
	files, skipped, err := Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan lines = %q, want %q", got, want)
	}
	if skipped != 2 {
		t.Errorf("Scan skipped = %d, want 2", skipped)
	}
	if files != 1 {
		t.Errorf("Scan files = %d, want 1", files)
	}

	tailLines, tailSkipped, err := Tail(path, 10)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got := bytesToStrings(tailLines); !reflect.DeepEqual(got, want) {
		t.Errorf("Tail lines = %q, want %q", got, want)
	}
	if tailSkipped != 2 {
		t.Errorf("Tail skipped = %d, want 2", tailSkipped)
	}
}

func TestLobeLogScanOrdersDayFilesOldestFirst(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	events := filepath.Join(storeDir(home), "events")
	// Written out of order on purpose; the names are what decides the order.
	writeFile(t, filepath.Join(events, "events-20261110.jsonl"), objLine(31)+"\n")
	writeFile(t, filepath.Join(events, "events-20261108.jsonl"), objLine(11)+"\n"+objLine(12)+"\n")
	writeFile(t, filepath.Join(events, "events-20261109.jsonl"), objLine(21)+"\n")
	// Files that are not day files are not read.
	writeFile(t, filepath.Join(events, "drops-20261109.jsonl"), objLine(99)+"\n")
	writeFile(t, filepath.Join(events, "events-2026110.jsonl"), objLine(98)+"\n")
	writeFile(t, filepath.Join(events, "events-20261109.jsonl.bak"), objLine(97)+"\n")

	var got []string
	files, skipped, err := Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{objLine(11), objLine(12), objLine(21), objLine(31)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan lines = %q, want %q", got, want)
	}
	if files != 3 || skipped != 0 {
		t.Errorf("Scan = %d files, %d skipped; want 3, 0", files, skipped)
	}

	// fn returning false stops the walk: the later day files are not read.
	var first []string
	if _, _, err := Scan(st, func(line []byte) bool {
		first = append(first, string(line))
		return len(first) < 2
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{objLine(11), objLine(12)}; !reflect.DeepEqual(first, want) {
		t.Errorf("Scan stopped after %q, want %q", first, want)
	}
}

func TestLobeLogTailReturnsNewestOldestFirst(t *testing.T) {
	home := setupHome(t)
	path := filepath.Join(storeDir(home), filepath.FromSlash(testDayFile))
	var b strings.Builder
	for n := 0; n < 10; n++ {
		b.WriteString(objLine(n) + "\n")
	}
	writeFile(t, path, b.String())

	got, skipped, err := Tail(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{objLine(7), objLine(8), objLine(9)}; !reflect.DeepEqual(bytesToStrings(got), want) {
		t.Errorf("Tail(3) = %q, want %q", bytesToStrings(got), want)
	}
	if skipped != 0 {
		t.Errorf("Tail(3) skipped = %d, want 0", skipped)
	}

	all, _, err := Tail(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10 || string(all[0]) != objLine(0) {
		t.Errorf("Tail(100) gave %d lines, first %q; want 10 lines from n=0", len(all), bytesToStrings(all))
	}
}

func TestLobeLogTailReadsAWindowThatGrows(t *testing.T) {
	home := setupHome(t)
	path := filepath.Join(storeDir(home), filepath.FromSlash(testDayFile))
	// Ten lines of about 300 KB make a file of about 3 MB: the first 1 MiB window holds three lines,
	// so asking for eight takes a larger window.
	pad := strings.Repeat("x", 300000)
	var b strings.Builder
	for n := 0; n < 10; n++ {
		fmt.Fprintf(&b, `{"n":%d,"pad":"%s"}`+"\n", n, pad)
	}
	writeFile(t, path, b.String())

	got, _, err := Tail(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("Tail(8) gave %d lines, want 8", len(got))
	}
	if !strings.HasPrefix(string(got[0]), `{"n":2,`) || !strings.HasPrefix(string(got[7]), `{"n":9,`) {
		t.Errorf("Tail(8) first %.12q, last %.12q; want n=2 first and n=9 last", got[0], got[7])
	}

	// Two lines fit in the first window, so the newest two come back without a larger read.
	two, _, err := Tail(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(two) != 2 || !strings.HasPrefix(string(two[1]), `{"n":9,`) {
		t.Errorf("Tail(2) gave %d lines; want the newest two", len(two))
	}
}

func TestLobeLogReadersMissingFolderIsNoError(t *testing.T) {
	home := setupHome(t)
	st := openAcme(t)
	called := false
	files, skipped, err := Scan(st, func([]byte) bool {
		called = true
		return true
	})
	if err != nil || files != 0 || skipped != 0 || called {
		t.Errorf("Scan with no events folder = %d, %d, %v, called %v; want 0, 0, nil, false", files, skipped, err, called)
	}
	lines, skipped, err := Tail(filepath.Join(storeDir(home), "events", "events-20261109.jsonl"), 5)
	if err != nil || len(lines) != 0 || skipped != 0 {
		t.Errorf("Tail of a missing file = %d lines, %d skipped, %v; want none", len(lines), skipped, err)
	}
	if lines, _, err := Tail(filepath.Join(home, "x"), 0); err != nil || lines != nil {
		t.Errorf("Tail with max 0 = %v, %v; want none", lines, err)
	}
}

// bytesToStrings converts lines to strings for comparison.
func bytesToStrings(lines [][]byte) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, string(l))
	}
	return out
}
