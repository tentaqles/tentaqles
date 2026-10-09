package decide

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
)

// Case is one labelled eval example: a state, one yes/no question, and the
// answer a careful human would give. Family groups cases by rule (sql,
// secrets, n8n, ...) so every rule gets its own threshold.
type Case struct {
	ID       string   `json:"id"`
	Family   string   `json:"family"`
	Lang     string   `json:"lang,omitempty"`
	Tags     []string `json:"tags,omitempty"` // e.g. injection, paraphrase
	State    any      `json:"state"`
	Question string   `json:"question"`
	Expect   bool     `json:"expect"`
}

// ReadCases parses JSONL; blank lines and lines starting with # are skipped.
func ReadCases(r io.Reader) ([]Case, error) {
	var out []Case
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var c Case
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if c.Question == "" || c.State == nil {
			return nil, fmt.Errorf("line %d: state and question are required", n)
		}
		if c.ID == "" {
			c.ID = fmt.Sprintf("case%d", n)
		}
		if c.Family == "" {
			c.Family = "default"
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

// Result is Jev's answer to one case.
type Result struct {
	Case Case
	P    float64
	Err  error
}

// Run asks Jev every case, `workers` at a time.
func Run(ctx context.Context, c *Client, cases []Case, workers int) []Result {
	if workers < 1 {
		workers = 1
	}
	out := make([]Result, len(cases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)
	for i, cs := range cases {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, cs Case) {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := c.Ask(ctx, cs.State, map[string]Question{"q": Noul(cs.Question)})
			res := Result{Case: cs, Err: err}
			if err == nil {
				res.P, res.Err = r.NoulOf("q")
			}
			out[i] = res
		}(i, cs)
	}
	wg.Wait()
	return out
}

// Point is the confusion matrix at one threshold.
type Point struct {
	Threshold         float64
	TP, FP, FN, TN    int
	Precision, Recall float64
	F1                float64
}

// Report summarises one family (or "all").
type Report struct {
	Family   string
	N        int
	Pos      int
	Errors   int
	Baseline float64 // accuracy of always answering "no"
	Curve    []Point
	// Pick is the threshold with the best recall among those whose precision
	// is >= MinPrecision (the highest such threshold on a tie), or nil when
	// none qualifies — that family must stay in shadow mode.
	Pick *Point
	// Misses are the wrong cases at Pick (or at 0.5 when there is no pick).
	Misses []Result
}

// MinPrecision is the bar for a rule to block: at most 1 false alarm in 10.
const MinPrecision = 0.9

// Score builds one report per family plus an "all" report. Errored cases
// count against nobody's confusion matrix but are reported.
func Score(results []Result) []Report {
	fams := map[string][]Result{}
	for _, r := range results {
		fams[r.Case.Family] = append(fams[r.Case.Family], r)
	}
	names := make([]string, 0, len(fams))
	for f := range fams {
		names = append(names, f)
	}
	sort.Strings(names)
	var out []Report
	for _, f := range names {
		out = append(out, score(f, fams[f]))
	}
	if len(names) > 1 {
		out = append(out, score("all", results))
	}
	return out
}

func score(family string, rs []Result) Report {
	rep := Report{Family: family, N: len(rs)}
	var ok []Result
	for _, r := range rs {
		if r.Err != nil {
			rep.Errors++
			continue
		}
		if r.Case.Expect {
			rep.Pos++
		}
		ok = append(ok, r)
	}
	if len(ok) > 0 {
		rep.Baseline = float64(len(ok)-rep.Pos) / float64(len(ok))
	}
	for t := 0.10; t < 0.951; t += 0.05 {
		t = math.Round(t*100) / 100
		p := Point{Threshold: t}
		for _, r := range ok {
			yes := r.P >= t
			switch {
			case yes && r.Case.Expect:
				p.TP++
			case yes && !r.Case.Expect:
				p.FP++
			case !yes && r.Case.Expect:
				p.FN++
			default:
				p.TN++
			}
		}
		if p.TP+p.FP > 0 {
			p.Precision = float64(p.TP) / float64(p.TP+p.FP)
		}
		if p.TP+p.FN > 0 {
			p.Recall = float64(p.TP) / float64(p.TP+p.FN)
		}
		if p.Precision+p.Recall > 0 {
			p.F1 = 2 * p.Precision * p.Recall / (p.Precision + p.Recall)
		}
		rep.Curve = append(rep.Curve, p)
	}
	for i := range rep.Curve {
		p := rep.Curve[i]
		if p.TP == 0 || p.Precision < MinPrecision {
			continue
		}
		// >= over an ascending sweep: among thresholds with the best recall,
		// take the highest, which leaves the most margin against false alarms
		// on cases the eval set does not cover.
		if rep.Pick == nil || p.Recall >= rep.Pick.Recall {
			pp := p
			rep.Pick = &pp
		}
	}
	at := 0.5
	if rep.Pick != nil {
		at = rep.Pick.Threshold
	}
	for _, r := range ok {
		if (r.P >= at) != r.Case.Expect {
			rep.Misses = append(rep.Misses, r)
		}
	}
	return rep
}

// Format renders reports as plain text.
func Format(w io.Writer, reps []Report) {
	for _, r := range reps {
		fmt.Fprintf(w, "== %s: %d cases (%d positive, %d errors); always-no baseline %.0f%%\n", r.Family, r.N, r.Pos, r.Errors, r.Baseline*100)
		fmt.Fprintf(w, "  thr   prec  recall  f1    tp fp fn tn\n")
		for _, p := range r.Curve {
			mark := " "
			if r.Pick != nil && p.Threshold == r.Pick.Threshold {
				mark = "*"
			}
			fmt.Fprintf(w, " %s%.2f  %.2f  %.2f    %.2f  %2d %2d %2d %2d\n", mark, p.Threshold, p.Precision, p.Recall, p.F1, p.TP, p.FP, p.FN, p.TN)
		}
		if r.Pick != nil {
			fmt.Fprintf(w, "  pick: block_threshold %.2f (precision %.2f, recall %.2f)\n", r.Pick.Threshold, r.Pick.Precision, r.Pick.Recall)
		} else {
			fmt.Fprintf(w, "  pick: none reaches precision %.2f — keep this family in shadow mode\n", MinPrecision)
		}
		for _, m := range r.Misses {
			kind := "false alarm"
			if m.Case.Expect {
				kind = "missed"
			}
			fmt.Fprintf(w, "  %-11s %s (p=%.2f, %s %s)\n", kind, m.Case.ID, m.P, m.Case.Lang, strings.Join(m.Case.Tags, ","))
		}
	}
}
