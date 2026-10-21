package insights

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessReadEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")
	lines := []string{
		`{"ts":"2026-10-20T10:00:00+00:00","kind":"verify","slug":"a","project":"x","workflow":"feature","checkpoint":1,"ok":true}`,
		`{"ts":"2026-10-20T10:01:00+00:00","kind":"verify","slug":"a","project":"x","workflow":"feature","checkpoint":1,"ok":false}`,
		`{"ts":"2026-10-20T10:02:00+00:00","kind":"review","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"build-critic","result":"FAIL","blockers":1,"majors":0,"minors":2,"unjudged":0}`,
		`{"ts":"2026-10-20T10:03:00+00:00","kind":"review","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"spec-critic","result":"BLOCKED","unjudged":1}`,
		`{"ts":"2026-10-20T10:04:00+00:00","kind":"judge","slug":"a","project":"x","workflow":"feature","checkpoint":1,"reviewer":"spec-critic","result":"PASS"}`,
		`{"ts":"2026-10-20T10:05:00+00:00","kind":"dod","slug":"a","project":"x","workflow":"feature","ok":true}`,
		`{"ts":"2026-10-20T10:06:00+00:00","kind":"dod-override","slug":"b","project":"x","workflow":"fix","ok":false}`,
		`{"ts":"2026-10-20T10:06:30+00:00","kind":"dod-override-kept","slug":"b","project":"x","workflow":"fix","ok":true}`,
		`{"ts":"2026-10-20T10:06:40+00:00","kind":"ask","slug":"a","workflow":"feature","checkpoint":1,"decision":"judge"}`,
		`{"ts":"2026-10-20T10:06:50+00:00","kind":"answer","slug":"a","workflow":"feature","checkpoint":1,"decision":"judge","actor":"terminal"}`,
		`{"ts":"2026-10-20T10:06:55+00:00","kind":"mystery","slug":"a","workflow":"feature"}`,
		`{"ts":"2026-10-20T10:07:00+00:00","kind":"finish","slug":"a","project":"x","workflow":"feature"}`,
		`{"ts":"2026-01-01T00:00:00+00:00","kind":"verify","slug":"old","project":"x","workflow":"build","ok":true}`,
		`{"ts":"2026-10-20T10:08:00+00:00","kind":"review","slug":"a"}`,
		`not json`,
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	r, err := ReadProcessEvents(p, since)
	if err != nil {
		t.Fatal(err)
	}
	if r.VerifyPass != 1 || r.VerifyFail != 1 {
		t.Fatalf("verify %+v", r)
	}
	bc := r.Reviewers["build-critic"]
	if bc.Fail != 1 || bc.Blockers != 1 || bc.Minors != 2 {
		t.Fatalf("critic %+v", bc)
	}
	sc := r.Reviewers["spec-critic"]
	if sc.Blocked != 1 || sc.Unjudged != 1 || sc.HumanJudgements != 1 || sc.Pass != 0 {
		t.Fatalf("spec-critic %+v", sc)
	}
	if r.DoDPass != 1 || r.DoDFail != 0 || r.DoDOverrides != 1 || r.DoDOverridesKept != 1 || r.Finished != 1 {
		t.Fatalf("dod %+v", r)
	}
	if r.Asked != 1 || r.Answered != 1 || r.Other["mystery"] != 1 {
		t.Fatalf("ask/answer/other %+v", r)
	}
	if r.ByWorkflow["feature"] != 1 || r.ByWorkflow["fix"] != 1 || r.ByWorkflow["build"] != 0 {
		t.Fatalf("by workflow %v", r.ByWorkflow)
	}
	if got := r.CannotJudgeRate; got < 0.49 || got > 0.51 {
		t.Fatalf("cannot-judge rate %v want 0.5", got)
	}
	if _, ok := r.Reviewers[""]; ok {
		t.Fatalf("reviewer-less review must be skipped: %v", r.Reviewers)
	}
}

func TestProcessMissingFileIsEmpty(t *testing.T) {
	r, err := ReadProcessEvents(filepath.Join(t.TempDir(), "none.jsonl"), time.Time{})
	if err != nil || r.VerifyPass != 0 || r.Reviewers == nil || r.Other == nil {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestProcessLineCap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.jsonl")
	var b strings.Builder
	old := 250000 - maxProcessLines
	for i := 0; i < 250000; i++ {
		if i < old {
			b.WriteString(`{"ts":"2026-10-20T10:00:00+00:00","kind":"verify","slug":"o","workflow":"oldwf","ok":false}` + "\n")
		} else {
			b.WriteString(`{"ts":"2026-10-20T10:00:00+00:00","kind":"verify","slug":"n","workflow":"newwf","ok":true}` + "\n")
		}
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := ReadProcessEvents(p, time.Time{})
	if err != nil || r.VerifyPass != maxProcessLines || r.VerifyFail != 0 || r.ByWorkflow["oldwf"] != 0 || r.ByWorkflow["newwf"] != 1 {
		t.Fatalf("pass %d fail %d wf %v err %v: the cap must keep the newest events", r.VerifyPass, r.VerifyFail, r.ByWorkflow, err)
	}
}

func TestProcessRenderSanitisesAndShowsReadError(t *testing.T) {
	long := strings.Repeat("x", 100)
	r := &Report{Process: ProcessReport{Reviewers: map[string]ReviewerStats{"bad\x1b[31mname" + long: {Pass: 1}},
		Other: map[string]int{"k\x07ind": 1}, ReadError: "events.jsonl could not be read"}}
	var sb strings.Builder
	WriteText(&sb, r)
	out := sb.String()
	if strings.ContainsAny(out, "\x1b\x07") || strings.Contains(out, long) {
		t.Fatalf("control chars or long name leaked:\n%q", out)
	}
	if !strings.Contains(out, "events.jsonl could not be read") {
		t.Fatalf("read error missing:\n%s", out)
	}
}

func TestProcessRenderAndJSON(t *testing.T) {
	r := &Report{Process: ProcessReport{VerifyPass: 2, Reviewers: map[string]ReviewerStats{"build-critic": {Pass: 1, Blocked: 1}},
		ByWorkflow: map[string]int{"feature": 1}, CannotJudgeRate: 0.5}}
	var sb strings.Builder
	WriteText(&sb, r)
	out := sb.String()
	if !strings.Contains(out, "== Process (build gate)") || !strings.Contains(out, "build-critic") || !strings.Contains(out, "50.0%") {
		t.Fatalf("render:\n%s", out)
	}
}
