package policy

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// keepRecorded restores the plan-store seam after a test.
func keepRecorded(t *testing.T) {
	old := RecordedPlans
	t.Cleanup(func() { RecordedPlans = old })
	RecordedPlans = nil
}

// TestSetRecordedPlans: the apply gate's register file wires its plan store
// once; a second store, or a nil one, is a programming error.
func TestSetRecordedPlans(t *testing.T) {
	keepRecorded(t)
	if ps, err := Plans(&packs.Input{}); ps != nil || err != nil {
		t.Fatalf("no store: %v %v", ps, err)
	}
	abs := filepath.Join(t.TempDir(), "p1.redacted.json")
	SetRecordedPlans(func(*packs.Input) ([]Plan, error) {
		return []Plan{{ID: "p1", Kind: model.KindTFPlan, Path: abs}}, nil
	})
	ps, err := Plans(&packs.Input{})
	if err != nil || len(ps) != 1 || ps[0].ID != "p1" {
		t.Fatalf("Plans = %v %v", ps, err)
	}
	mustPanic(t, "second store", func() {
		SetRecordedPlans(func(*packs.Input) ([]Plan, error) { return nil, nil })
	})
	RecordedPlans = nil
	mustPanic(t, "nil store", func() { SetRecordedPlans(nil) })
}

// TestPlansFailClosed: a store that panics, errs or returns a plan the
// checks cannot read safely fails the checks that read plans.
func TestPlansFailClosed(t *testing.T) {
	keepRecorded(t)
	abs := filepath.Join(t.TempDir(), "p.json")
	cases := map[string]func(*packs.Input) ([]Plan, error){
		"panic": func(*packs.Input) ([]Plan, error) { panic("boom") },
		"error": func(*packs.Input) ([]Plan, error) { return nil, errors.New("plans.jsonl is corrupt") },
		"relative": func(*packs.Input) ([]Plan, error) {
			return []Plan{{ID: "p", Kind: model.KindTFPlan, Path: "p.json"}}, nil
		},
		"no path": func(*packs.Input) ([]Plan, error) { return []Plan{{ID: "p", Kind: model.KindTFPlan}}, nil },
		"no kind": func(*packs.Input) ([]Plan, error) { return []Plan{{ID: "p", Path: abs}}, nil },
		"no id":   func(*packs.Input) ([]Plan, error) { return []Plan{{Kind: model.KindTFPlan, Path: abs}}, nil },
	}
	for name, f := range cases {
		RecordedPlans = f
		ps, err := Plans(&packs.Input{})
		if err == nil || ps != nil {
			t.Errorf("%s: Plans = %v, %v; want an error", name, ps, err)
		}
		if err != nil && strings.Contains(err.Error(), abs) {
			t.Errorf("%s: the error names the path: %v", name, err)
		}
		if !HasPlans(&packs.Input{}) {
			t.Errorf("%s: HasPlans false; an unreadable store must run the checks", name)
		}
	}
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}
