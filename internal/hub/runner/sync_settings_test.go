package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestSettingsCarrySyncDefaults(t *testing.T) {
	h := newHarness(t)
	got := h.svc.Settings().Sync
	want := store.SyncSettings{Enabled: false, Interval: store.DefaultSyncInterval, Policy: store.DefaultSyncPolicy()}
	if got != want {
		t.Fatalf("no tracker block: %+v", got)
	}
	h.svc.cfg.HasTracker = true
	if !h.svc.Settings().Sync.Enabled {
		t.Fatal("sync.enabled defaults to true with a tracker block")
	}
}

func TestUpdateSyncSettings(t *testing.T) {
	h := newHarness(t)
	got, err := h.svc.UpdateSettings(context.Background(), SettingsUpdate{Sync: &SyncUpdate{
		Enabled: boolp(true), Interval: intp(90), Policy: map[string]string{store.OutboxCreateCard: store.PolicyAuto}}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Sync.Enabled || got.Sync.Interval != 90 || got.Sync.Policy.CreateCard != store.PolicyAuto || got.Sync.Policy.EditText != store.PolicyAsk {
		t.Fatalf("%+v", got.Sync)
	}
	e := lastEntry(t, h, "settings.update")
	fields := anyStrings(e.Data["fields"])
	if len(fields) != 3 || fields[0] != "sync.enabled" || fields[1] != "sync.interval" || fields[2] != "sync.policy.create_card" {
		t.Fatalf("fields %v", fields)
	}
	for _, u := range []SettingsUpdate{
		{Sync: &SyncUpdate{Interval: intp(30)}},
		{Sync: &SyncUpdate{Policy: map[string]string{store.OutboxCloseItem: "always"}}},
		{Sync: &SyncUpdate{Policy: map[string]string{"move_on_lunch": store.PolicyOff}}},
		{Sync: &SyncUpdate{}},
	} {
		if _, err := h.svc.UpdateSettings(context.Background(), u); err == nil {
			t.Fatalf("%+v accepted", u.Sync)
		}
	}
	if _, err := h.svc.UpdateSettings(WithAgent(context.Background()), SettingsUpdate{Sync: &SyncUpdate{Interval: intp(600)}}); !errors.Is(err, ErrHumanOnly) {
		t.Fatalf("agent: %v", err)
	}
	if h.st.SyncInterval() != 90 {
		t.Fatal("a refused update changed the interval")
	}
}
