// Package activity is the hub's append-only activity log ($TQ_HOME/hub/<ws>/activity.jsonl)
// and its aggregate reader for `tq insights`. Entries carry ids, verbs and
// aggregate data only; the store masks every string before it appends.
package activity

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	VerbCardCreate     = "card.create"
	VerbCardMove       = "card.move"
	VerbCardUpdate     = "card.update"
	VerbCommentAdd     = "comment.add"
	VerbRunStart       = "run.start"
	VerbRunWait        = "run.wait"
	VerbRunEnd         = "run.end"
	VerbDecisionAsk    = "decision.ask"
	VerbDecisionAnswer = "decision.answer"
	VerbReviewApprove  = "review.approve"
	VerbReviewFeedback = "review.feedback"
	VerbBudgetStop     = "budget.stop"
	VerbSettingsUpdate = "settings.update"
)

var Verbs = []string{VerbCardCreate, VerbCardMove, VerbCardUpdate, VerbCommentAdd, VerbRunStart, VerbRunWait, VerbRunEnd,
	VerbDecisionAsk, VerbDecisionAnswer, VerbReviewApprove, VerbReviewFeedback, VerbBudgetStop, VerbSettingsUpdate}

type Entry struct {
	TS        string         `json:"ts"`
	Workspace string         `json:"workspace"`
	Actor     string         `json:"actor"`
	Verb      string         `json:"verb"`
	CardID    string         `json:"card_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// Append writes one line. The caller (the store) has already masked e.
func Append(path string, e Entry) error {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// Read returns the entries at or after since; unparseable lines are skipped.
func Read(path string, since time.Time) ([]Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.TS)
		if err != nil || ts.Before(since) {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

type Report struct {
	Workspaces        []string           `json:"workspaces"`
	CardsByLane       map[string]int     `json:"cards_by_lane"`
	RunsStarted       int                `json:"runs_started"`
	RunsEnded         map[string]int     `json:"runs_ended"`
	CostByCard        map[string]float64 `json:"cost_by_card"`
	CostByKind        map[string]float64 `json:"cost_by_kind"`
	DecisionsAnswered int                `json:"decisions_answered"`
	MedianWaitMin     float64            `json:"median_wait_min"`
	Overrides         int                `json:"overrides"`
	RequestChanges    int                `json:"request_changes"`
	BudgetStops       int                `json:"budget_stops"`
	ReadError         string             `json:"read_error,omitempty"`
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// Summarize aggregates entries (any order; sorted by time here).
func Summarize(entries []Entry) Report {
	r := Report{Workspaces: []string{}, CardsByLane: map[string]int{}, RunsEnded: map[string]int{},
		CostByCard: map[string]float64{}, CostByKind: map[string]float64{}}
	es := append([]Entry(nil), entries...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].TS < es[j].TS })
	lane := map[string]string{}
	asked := map[string]time.Time{}
	var waits []float64
	for _, e := range es {
		switch e.Verb {
		case VerbCardCreate:
			lane[e.CardID] = str(e.Data["lane"])
		case VerbCardMove:
			lane[e.CardID] = str(e.Data["to"])
		case VerbRunStart:
			r.RunsStarted++
		case VerbRunEnd:
			r.RunsEnded[str(e.Data["status"])]++
			c := num(e.Data["cost_usd"])
			r.CostByCard[e.CardID] += c
			if k := str(e.Data["kind"]); k != "" {
				r.CostByKind[k] += c
			}
		case VerbDecisionAsk:
			if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
				asked[str(e.Data["decision"])] = t
			}
		case VerbDecisionAnswer:
			r.DecisionsAnswered++
			switch str(e.Data["choice"]) {
			case "override":
				r.Overrides++
			case "request-changes":
				r.RequestChanges++
			}
			if a, ok := asked[str(e.Data["decision"])]; ok {
				if t, err := time.Parse(time.RFC3339, e.TS); err == nil && !t.Before(a) {
					waits = append(waits, t.Sub(a).Minutes())
				}
			}
		case VerbBudgetStop:
			r.BudgetStops++
		}
	}
	for _, l := range lane {
		if l != "" {
			r.CardsByLane[l]++
		}
	}
	if len(waits) > 0 {
		sort.Float64s(waits)
		m := len(waits) / 2
		if len(waits)%2 == 1 {
			r.MedianWaitMin = waits[m]
		} else {
			r.MedianWaitMin = (waits[m-1] + waits[m]) / 2
		}
	}
	return r
}

// ReadAll reads every <hubDir>/<ws>/activity.jsonl that keep accepts (nil keeps all).
func ReadAll(hubDir string, since time.Time, keep func(ws string) bool) (Report, error) {
	ents, err := os.ReadDir(hubDir)
	if os.IsNotExist(err) {
		return Summarize(nil), nil
	}
	if err != nil {
		return Summarize(nil), err
	}
	var all []Entry
	var names []string
	var firstErr error
	for _, d := range ents {
		if !d.IsDir() || (keep != nil && !keep(d.Name())) {
			continue
		}
		es, err := Read(filepath.Join(hubDir, d.Name(), "activity.jsonl"), since)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if len(es) > 0 {
			names = append(names, d.Name())
			all = append(all, es...)
		}
	}
	r := Summarize(all)
	sort.Strings(names)
	if names != nil {
		r.Workspaces = names
	}
	return r, firstErr
}
