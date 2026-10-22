package store

import (
	"math"
	"testing"
	"time"
)

func TestSetConcurrencyBounds(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	for _, n := range []int{0, -1, MaxConcurrency + 1} {
		if err := s.SetConcurrency(n); err == nil {
			t.Fatalf("%d accepted", n)
		}
	}
	if err := s.SetConcurrency(MaxConcurrency); err != nil || s.Concurrency() != MaxConcurrency {
		t.Fatalf("%v %d", err, s.Concurrency())
	}
}

func TestSetDefaultBudgetValidates(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	for _, b := range []Budget{{}, {Tokens: 1, USD: math.NaN(), Minutes: 1, Rounds: 1}, {Tokens: 1, USD: 1, Minutes: math.Inf(1), Rounds: 1}, {Tokens: 1, USD: 1, Minutes: 1, Rounds: 0}} {
		if err := s.SetDefaultBudget(b); err == nil {
			t.Fatalf("%+v accepted", b)
		}
	}
	want := Budget{Tokens: 5, USD: 1.5, Minutes: 3, Rounds: 2}
	if err := s.SetDefaultBudget(want); err != nil || s.DefaultBudget() != want {
		t.Fatalf("%v %+v", err, s.DefaultBudget())
	}
}

func TestSettersPublishASettingsChange(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	ch, cancel := s.Subscribe()
	defer cancel()
	if err := s.SetNotify(false); err != nil || s.Notify() {
		t.Fatalf("%v %v", err, s.Notify())
	}
	select {
	case c := <-ch:
		if c.Kind != "settings" || c.CardID != "" {
			t.Fatalf("%+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change published")
	}
}
