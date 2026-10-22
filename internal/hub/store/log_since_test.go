package store

import (
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestActivitySinceFiltersAndOrders(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for i, d := range []time.Duration{-72 * time.Hour, -2 * time.Hour, -time.Hour} {
		if err := s.Log(activity.Entry{TS: base.Add(d).Format(time.RFC3339), Actor: "tester", Verb: activity.VerbCardCreate, CardID: "c-aaaaaaaaaaa" + string(rune('a'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	es, err := s.ActivitySince(base.Add(-3*time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].CardID != "c-aaaaaaaaaaac" || es[1].CardID != "c-aaaaaaaaaaab" {
		t.Fatalf("newest first, the old entry left out: %+v", es)
	}
	if es, _ := s.ActivitySince(base.Add(-100*time.Hour), 1); len(es) != 1 {
		t.Fatalf("limit: %+v", es)
	}
}
