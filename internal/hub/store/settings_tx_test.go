package store

import (
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

func TestSetSettingsAtomicAndPublishesOnce(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	ch, cancel := s.Subscribe()
	defer cancel()
	n, off := 7, false
	b := Budget{Tokens: 5, USD: 1.5, Minutes: 3, Rounds: 2}
	if err := s.SetSettings(SettingsWrite{Concurrency: &n, DefaultBudget: &b, Notify: &off}, activity.Entry{Actor: "hub:tester", Verb: activity.VerbSettingsUpdate}); err != nil {
		t.Fatal(err)
	}
	if s.Concurrency() != 7 || s.DefaultBudget() != b || s.Notify() {
		t.Fatal("not all fields stored")
	}
	kinds := map[string]int{}
	for done := false; !done; {
		select {
		case c := <-ch:
			kinds[c.Kind]++
		case <-time.After(200 * time.Millisecond):
			done = true
		}
	}
	if kinds["settings"] != 1 {
		t.Fatalf("settings published %d times: %v", kinds["settings"], kinds)
	}
}

func TestSetSettingsRollsBackWhenTheAuditWriteFails(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if _, err := s.DB.Exec(`CREATE TRIGGER no_activity BEFORE INSERT ON activity BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	ch, cancel := s.Subscribe()
	defer cancel()
	n, off := 7, false
	b := Budget{Tokens: 5, USD: 1.5, Minutes: 3, Rounds: 2}
	if err := s.SetSettings(SettingsWrite{Concurrency: &n, DefaultBudget: &b, Notify: &off}, activity.Entry{Actor: "hub:tester", Verb: activity.VerbSettingsUpdate}); err == nil {
		t.Fatal("the injected failure was swallowed")
	}
	if s.Concurrency() != DefaultConcurrency || s.DefaultBudget() != DefaultBudget || !s.Notify() {
		t.Fatal("a failed update left some fields changed")
	}
	select {
	case c := <-ch:
		t.Fatalf("published %+v after a rollback", c)
	default:
	}
}
