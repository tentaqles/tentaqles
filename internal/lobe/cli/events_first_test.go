package cli

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

// TestEventsFirstLineSearchesTwoDays: a reference's first line is looked for in its own day file and the day
// before only. The first line here sits eight day files back: it is not printed, and only the two day files
// next to the reference are read.
func TestEventsFirstLineSearchesTwoDays(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	cwd := f.client("acme")
	writeDay(t, "20261102", sessionLine(900, "s3", "evt_old", ""))
	for d := 3; d <= 8; d++ {
		writeDay(t, fmt.Sprintf("202611%02d", d), sessionLine(800+d, "s9", fmt.Sprintf("evt_f%d", d), ""))
	}
	writeDay(t, "20261109", sessionLine(700, "s9", "evt_y", ""))
	var refs []string
	for n := 1; n <= 25; n++ {
		refs = append(refs, sessionLine(n, "s3", fmt.Sprintf("evt_r%02d", n), "evt_old"))
	}
	writeDay(t, "20261110", refs...)

	// Count the day files read: a counter around the reader seam.
	var read []string
	old := readTail
	readTail = func(path string, max int) ([][]byte, int, error) {
		read = append(read, filepath.Base(path))
		return old(path, max)
	}
	t.Cleanup(func() { readTail = old })

	var buf bytes.Buffer
	if err := Events(deps(false, nil, cwd), "", &buf); err != nil {
		t.Fatal(err)
	}
	if got := numbers(t, buf.String()); fmt.Sprint(got) != "[6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25]" {
		t.Fatalf("printed %v: the first line eight days back must not be printed", got)
	}
	allowed := map[string]bool{"events-20261110.jsonl": true, "events-20261109.jsonl": true}
	seenPrev := false
	for _, name := range read {
		if !allowed[name] {
			t.Errorf("read %s, want only the reference's day and the day before", name)
		}
		if name == "events-20261109.jsonl" {
			seenPrev = true
		}
	}
	if !seenPrev {
		t.Errorf("the day before was not searched; reads %v", read)
	}
}

// TestEventsFirstLineOlderCopyWins: a pair with a copy in the day before and a copy in the reference's day
// prints the older copy, as eventlog.Scan does, and the newer copy in the selection is dropped.
func TestEventsFirstLineOlderCopyWins(t *testing.T) {
	f := newHome(t)
	openAcme(t)
	cwd := f.client("acme")
	// The day before: the older copy of evt_a first, then 20 lines that push it out of the selection.
	prev := []string{sessionLine(1, "s3", "evt_a", "")}
	for n := 100; n < 120; n++ {
		prev = append(prev, sessionLine(n, "s9", fmt.Sprintf("evt_p%d", n), ""))
	}
	writeDay(t, "20261109", prev...)
	// The reference's day: a newer copy of evt_a, then three references to it.
	writeDay(t, "20261110", sessionLine(2, "s3", "evt_a", ""),
		sessionLine(3, "s3", "evt_r3", "evt_a"), sessionLine(4, "s3", "evt_r4", "evt_a"),
		sessionLine(5, "s3", "evt_r5", "evt_a"))

	var buf bytes.Buffer
	if err := Events(deps(false, nil, cwd), "", &buf); err != nil {
		t.Fatal(err)
	}
	// The older copy (n=1) leads; the newer copy (n=2) is dropped; the selection's fillers 104..119 follow.
	want := []int{1}
	for n := 104; n < 120; n++ {
		want = append(want, n)
	}
	want = append(want, 3, 4, 5)
	if got := numbers(t, buf.String()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("printed %v, want %v", got, want)
	}
}
