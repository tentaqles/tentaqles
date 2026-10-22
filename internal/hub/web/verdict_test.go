package web

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
)

func verdictData(statuses []string, dodOK *bool, reviews ...gate.Review) ReviewData {
	st := &gate.State{}
	for i, s := range statuses {
		st.Checkpoints = append(st.Checkpoints, gate.Checkpoint{ID: gate.CheckpointID(string(rune('1' + i))), Status: s})
	}
	rec := &gate.Records{Reviews: reviews}
	if dodOK != nil {
		rec.DoD = &gate.DoDRecord{OK: *dodOK}
	}
	st.Records = rec
	return ReviewData{State: st, Records: rec}
}

func TestVerdictRules(t *testing.T) {
	yes, no := true, false
	pass := gate.Review{Checkpoint: "1", Reviewer: "build-critic", Result: "PASS"}
	fail := gate.Review{Checkpoint: "2", Reviewer: "build-critic", Result: "FAIL"}
	cases := []struct {
		name  string
		d     ReviewData
		class string
		note  string
	}{
		{"all passed with reviews", verdictData([]string{"passed", "passed"}, &yes, pass), "ok", "2 of 2 checkpoints passed, 1 reviews PASS, DoD ok"},
		{"all passed, zero reviews", verdictData([]string{"passed"}, &yes), "wait", "no review recorded"},
		{"one FAIL review", verdictData([]string{"passed", "passed"}, &yes, pass, fail), "wait", "review build-critic on checkpoint 2: FAIL"},
		{"partially passed", verdictData([]string{"passed", "pending"}, &yes, pass), "wait", "1 of 2 checkpoints passed"},
		{"zero checkpoints", verdictData(nil, &yes, pass), "wait", "no checkpoint recorded"},
		{"no DoD record", verdictData([]string{"passed"}, nil, pass), "wait", "no definition-of-done record"},
		{"DoD not met", verdictData([]string{"passed"}, &no, pass), "bad", "DoD not met"},
		{"no records at all", ReviewData{RecordsNote: "this run has no build slug"}, "wait", "this run has no build slug"},
	}
	for _, c := range cases {
		v := c.d.Verdict()
		if v.Class != c.class || !strings.Contains(v.Note, c.note) {
			t.Errorf("%s: got %s %q / %q, want %s with %q", c.name, v.Class, v.Title, v.Note, c.class, c.note)
		}
		if c.class == "ok" && v.Title != "Everything the gate recorded says this is done" {
			t.Errorf("%s: title %q", c.name, v.Title)
		}
		if c.class == "wait" && strings.Contains(v.Title, "done") {
			t.Errorf("%s: a wait band must not say done: %q", c.name, v.Title)
		}
	}
}
