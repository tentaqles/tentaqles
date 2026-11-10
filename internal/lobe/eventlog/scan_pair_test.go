package eventlog

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestLobeScanPairAcrossDayFiles: a copy of a pair in the day file after the first copy's day is hidden (a
// replay across UTC midnight reads as one event); a copy whose pair the previous day file does not hold is
// passed, so the memory is the two day files only.
func TestLobeScanPairAcrossDayFiles(t *testing.T) {
	home := setupHome(t)
	openAcme(t)
	events := filepath.Join(storeDir(home), "events")
	// 08: the first copy of s1/evt_x.
	writeFile(t, filepath.Join(events, "events-20261108.jsonl"), `{"session":"s1","id":"evt_x","n":1}`+"\n")
	// 09: the next day's copy of the same pair is one event with the first; s2/evt_y is new.
	writeFile(t, filepath.Join(events, "events-20261109.jsonl"),
		`{"session":"s1","id":"evt_x","n":2}`+"\n"+`{"session":"s2","id":"evt_y","n":3}`+"\n")
	// 10: a copy seen in 09 (skipped there) is hidden here too, through the day before's record.
	writeFile(t, filepath.Join(events, "events-20261110.jsonl"), `{"session":"s1","id":"evt_x","n":4}`+"\n")
	// 11: s2/evt_y was seen in 09 but not in 10, so the previous day file does not hold it: passed.
	writeFile(t, filepath.Join(events, "events-20261111.jsonl"), `{"session":"s2","id":"evt_y","n":5}`+"\n")

	st := openAcme(t)
	var got []string
	if _, _, err := Scan(st, func(line []byte) bool {
		got = append(got, string(line))
		return true
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{
		`{"session":"s1","id":"evt_x","n":1}`,
		`{"session":"s2","id":"evt_y","n":3}`,
		`{"session":"s2","id":"evt_y","n":5}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan lines = %q, want %q", got, want)
	}
}
