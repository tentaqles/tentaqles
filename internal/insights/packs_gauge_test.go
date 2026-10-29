package insights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Final review, correctness m7: drill_age_days:<name> is a gauge (spec D
// §19): the Packs section keeps its latest value, never a sum over runs.
// Ordinary counters still add up.
func TestReadPacksGaugeKeepsLatest(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	at := func(h int) string { return now.Add(-time.Duration(h) * time.Hour).Format(time.RFC3339) }
	ev := strings.Join([]string{
		`{"ts":"` + at(3) + `","workspace":"acme","pack":"db","counter":"drill_age_days:main","n":5}`,
		`{"ts":"` + at(2) + `","workspace":"acme","pack":"db","counter":"drill_age_days:main","n":6}`,
		`{"ts":"` + at(1) + `","workspace":"acme","pack":"db","counter":"drill_age_days:main","n":7}`,
		`{"ts":"` + at(2) + `","workspace":"acme","pack":"db","counter":"restore_denied:env","n":1}`,
		`{"ts":"` + at(1) + `","workspace":"acme","pack":"db","counter":"restore_denied:env","n":1}`,
	}, "\n") + "\n"
	os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(ev), 0o600)
	rep, err := ReadPacks(dir, now.Add(-24*time.Hour), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.Counters["db"]["drill_age_days:main"]; got != 7 {
		t.Fatalf("gauge: %d, want the latest value 7", got)
	}
	if got := rep.Counters["db"]["restore_denied:env"]; got != 2 {
		t.Fatalf("counter: %d, want 2", got)
	}
}
