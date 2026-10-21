package store

import (
	"strings"
	"testing"
)

func TestSettingsDefaults(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if s.Concurrency() != 2 {
		t.Fatalf("concurrency %d", s.Concurrency())
	}
	if s.DefaultBudget() != (Budget{Tokens: 2_000_000, USD: 10, Minutes: 120, Rounds: 12}) {
		t.Fatalf("budget %+v", s.DefaultBudget())
	}
	if !s.Notify() {
		t.Fatal("notify defaults to true")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	if err := s.SetSetting(SettingConcurrency, "3"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(SettingDefaultBudget, `{"tokens":100,"usd":1,"minutes":5,"rounds":2}`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(SettingNotify, "false"); err != nil {
		t.Fatal(err)
	}
	if s.Concurrency() != 3 || s.DefaultBudget().Rounds != 2 || s.Notify() {
		t.Fatalf("settings not applied: %d %+v %v", s.Concurrency(), s.DefaultBudget(), s.Notify())
	}
	if err := s.SetSetting(SettingConcurrency, "0"); err != nil {
		t.Fatal(err)
	}
	if s.Concurrency() != DefaultConcurrency {
		t.Fatal("a concurrency below 1 falls back to the default")
	}
}

func TestParseBudget(t *testing.T) {
	b, err := ParseBudget("tokens=500_000, usd=2.5,minutes=30,rounds=4", DefaultBudget)
	if err != nil {
		t.Fatal(err)
	}
	if b != (Budget{Tokens: 500000, USD: 2.5, Minutes: 30, Rounds: 4}) {
		t.Fatalf("%+v", b)
	}
	partial, err := ParseBudget("usd=3", DefaultBudget)
	if err != nil || partial.USD != 3 || partial.Tokens != DefaultBudget.Tokens {
		t.Fatalf("partial %+v %v", partial, err)
	}
	for _, bad := range []string{"tokens", "tokens=-1", "usd=abc", "dollars=3", "rounds=0", "usd=nan", "usd=inf", "minutes=inf", "minutes=NaN"} {
		if _, err := ParseBudget(bad, DefaultBudget); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got := (Budget{Tokens: 10, USD: 1.5, Minutes: 2, Rounds: 3}).String(); got != "tokens=10,usd=1.5,minutes=2,rounds=3" {
		t.Fatalf("String() = %q", got)
	}
	if !strings.Contains(DefaultBudget.String(), "tokens=2000000") {
		t.Fatal(DefaultBudget.String())
	}
}

func TestDefaultBudgetRejectsInvalidStored(t *testing.T) {
	isolate(t)
	s := mustOpen(t, "acme")
	for _, bad := range []string{`{"tokens":0}`, `{"tokens":-5,"usd":1,"minutes":1,"rounds":1}`, `{"usd":-1}`, `{"rounds":0}`, `not json`} {
		if err := s.SetSetting(SettingDefaultBudget, bad); err != nil {
			t.Fatal(err)
		}
		if s.DefaultBudget() != DefaultBudget {
			t.Fatalf("%s accepted: %+v", bad, s.DefaultBudget())
		}
	}
}
