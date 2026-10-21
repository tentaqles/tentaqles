package insights

import (
	"encoding/json"
	"time"
)

// Process events are written by the plugin's build gate
// ($TQ_HOME/build-gate/events.jsonl): one aggregate-only line per verify run,
// reviewer verdict, human judgement, definition-of-done run, override, ask,
// answer and finish. They carry slugs, reviewer names, results and counts;
// never file names or text. Unknown kinds are counted under Other and missing
// fields are skipped, so a newer gate never breaks the report.

// ReviewerStats aggregates one reviewer's verdicts.
type ReviewerStats struct {
	Pass            int `json:"pass"`
	Fail            int `json:"fail"`
	Blocked         int `json:"blocked"`
	Blockers        int `json:"blockers"`
	Majors          int `json:"majors"`
	Minors          int `json:"minors"`
	Unjudged        int `json:"unjudged"`
	HumanJudgements int `json:"human_judgements"`
}

// ProcessReport is the "Process" section of the report (spec §12).
type ProcessReport struct {
	VerifyPass       int                      `json:"verify_pass"`
	VerifyFail       int                      `json:"verify_fail"`
	Reviewers        map[string]ReviewerStats `json:"reviewers"`
	DoDPass          int                      `json:"dod_pass"`
	DoDFail          int                      `json:"dod_fail"`
	DoDOverrides     int                      `json:"dod_overrides"`
	DoDOverridesKept int                      `json:"dod_overrides_kept"`
	Asked            int                      `json:"asked"`
	Answered         int                      `json:"answered"`
	Finished         int                      `json:"finished"`
	ByWorkflow       map[string]int           `json:"by_workflow"`
	Other            map[string]int           `json:"other"`
	CannotJudgeRate  float64                  `json:"cannot_judge_rate"`
	ReadError        string                   `json:"read_error,omitempty"`
}

type processEvent struct {
	TS       string `json:"ts"`
	Kind     string `json:"kind"`
	Slug     string `json:"slug"`
	Workflow string `json:"workflow"`
	OK       *bool  `json:"ok"`
	Reviewer string `json:"reviewer"`
	Result   string `json:"result"`
	Blockers int    `json:"blockers"`
	Majors   int    `json:"majors"`
	Minors   int    `json:"minors"`
	Unjudged int    `json:"unjudged"`
}

func parseEventTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000000+00:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// maxProcessLines bounds how much of a large events file is aggregated: the
// newest events win, so a ring buffer of that many parsed events is kept.
const maxProcessLines = 200000

// ReadProcessEvents aggregates the events at or after since. A missing file
// yields an empty report and no error; malformed lines are skipped and only
// the newest maxProcessLines events are aggregated.
func ReadProcessEvents(path string, since time.Time) (ProcessReport, error) {
	r := ProcessReport{Reviewers: map[string]ReviewerStats{}, ByWorkflow: map[string]int{}, Other: map[string]int{}}
	ring := make([]processEvent, 0, 1024)
	total := 0
	err := eachLine(path, func(line []byte) {
		var e processEvent
		if json.Unmarshal(line, &e) != nil || e.Kind == "" {
			return
		}
		t, ok := parseEventTime(e.TS)
		if ok {
			if t.Before(since) {
				return
			}
		} else if !since.IsZero() {
			return
		}
		if len(ring) < maxProcessLines {
			ring = append(ring, e)
		} else {
			ring[total%maxProcessLines] = e
		}
		total++
	})
	start := 0
	if total > maxProcessLines {
		start = total % maxProcessLines
	}
	runs := map[string]string{} // slug -> workflow
	verdicts, blocked := 0, 0
	for i := 0; i < len(ring); i++ {
		e := ring[(start+i)%len(ring)]
		r.add(e, runs, &verdicts, &blocked)
	}
	for _, wf := range runs {
		r.ByWorkflow[wf]++
	}
	if verdicts > 0 {
		r.CannotJudgeRate = float64(blocked) / float64(verdicts)
	}
	return r, err
}

func (r *ProcessReport) add(e processEvent, runs map[string]string, verdicts, blocked *int) {
	if e.Slug != "" {
		if e.Workflow != "" {
			runs[e.Slug] = e.Workflow
		} else if _, seen := runs[e.Slug]; !seen {
			runs[e.Slug] = "build"
		}
	}
	switch e.Kind {
	case "verify":
		if e.OK != nil && *e.OK {
			r.VerifyPass++
		} else {
			r.VerifyFail++
		}
	case "review":
		if e.Reviewer == "" {
			return
		}
		s := r.Reviewers[e.Reviewer]
		switch e.Result {
		case "PASS":
			s.Pass++
			*verdicts++
		case "FAIL":
			s.Fail++
			*verdicts++
		case "BLOCKED":
			s.Blocked++
			*blocked++
			*verdicts++
		}
		s.Blockers += e.Blockers
		s.Majors += e.Majors
		s.Minors += e.Minors
		s.Unjudged += e.Unjudged
		r.Reviewers[e.Reviewer] = s
	case "judge":
		if e.Reviewer == "" {
			return
		}
		s := r.Reviewers[e.Reviewer]
		s.HumanJudgements++
		r.Reviewers[e.Reviewer] = s
	case "dod":
		if e.OK != nil && *e.OK {
			r.DoDPass++
		} else {
			r.DoDFail++
		}
	case "dod-override":
		r.DoDOverrides++
	case "dod-override-kept":
		r.DoDOverridesKept++
	case "ask":
		r.Asked++
	case "answer":
		r.Answered++
	case "finish":
		r.Finished++
	default:
		r.Other[e.Kind]++
	}
}
