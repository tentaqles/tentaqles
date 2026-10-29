package insights

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// PackCheckStats aggregates one pack check id.
type PackCheckStats struct {
	Runs            int     `json:"runs"`
	OK              int     `json:"ok"`
	Fail            int     `json:"fail"`
	Skip            int     `json:"skip"`
	AdvisoryFail    int     `json:"advisory_fail"`
	DeclaredMissing int     `json:"declared_missing"`
	P95MS           float64 `json:"p95_ms"`
	durations       []float64
}

// PackApprovals aggregates the pack ledgers.
type PackApprovals struct {
	Asked             int     `json:"asked"`
	Approved          int     `json:"approved"`
	Rejected          int     `json:"rejected"`
	Expired           int     `json:"expired"`
	Used              int     `json:"used"`
	MedianAnswerMin   float64 `json:"median_answer_min"`
	PromotionsApplied int     `json:"promotions_applied"`
}

// PacksReport is the Packs section (spec C §7): aggregates only.
type PacksReport struct {
	Checks          map[string]*PackCheckStats `json:"checks"`
	DeclaredMissing int                        `json:"declared_missing"`
	// Counters are the packs' own counters (packs.Count), by pack then name.
	Counters  map[string]map[string]int `json:"counters"`
	Approvals PackApprovals             `json:"approvals"`
	ReadError string                    `json:"read_error,omitempty"`
}

type packEvent struct {
	TS         string `json:"ts"`
	Workspace  string `json:"workspace"`
	Check      string `json:"check"`
	Pack       string `json:"pack"`
	Status     string `json:"status"`
	Required   bool   `json:"required"`
	DurationMS int64  `json:"duration_ms"`
	Missing    bool   `json:"declared_missing"`
	Counter    string `json:"counter"`
	N          int    `json:"n"`
}

type ledgerLine struct {
	Event     string `json:"event"`
	Decision  string `json:"decision"`
	Action    string `json:"action"`
	Grant     string `json:"grant"`
	Choice    string `json:"choice"`
	ActorKind string `json:"actor_kind"`
	ExpiresAt string `json:"expires_at"`
	TS        string `json:"ts"`
}

// ReadPacks reads $TQ_HOME/packs/events.jsonl and every pack ledger
// ($TQ_HOME/packs/<ws>/<pack>/ledger.jsonl) since since; keepWS filters
// workspaces (nil: all).
func ReadPacks(dir string, since, now time.Time, keepWS func(string) bool) (PacksReport, error) {
	rep := PacksReport{Checks: map[string]*PackCheckStats{}, Counters: map[string]map[string]int{}}
	var firstErr error
	if err := eachLine(filepath.Join(dir, "events.jsonl"), func(b []byte) {
		var e packEvent
		if json.Unmarshal(b, &e) != nil {
			return
		}
		if ts, ok := parseEventTime(e.TS); !ok || ts.Before(since) {
			return
		}
		if keepWS != nil && !keepWS(e.Workspace) {
			return
		}
		if e.Counter != "" {
			m := rep.Counters[e.Pack]
			if m == nil {
				m = map[string]int{}
				rep.Counters[e.Pack] = m
			}
			m[e.Counter] += e.N
			return
		}
		if e.Check == "" {
			return
		}
		s := rep.Checks[e.Check]
		if s == nil {
			s = &PackCheckStats{}
			rep.Checks[e.Check] = s
		}
		s.Runs++
		switch e.Status {
		case "ok":
			s.OK++
		case "fail":
			s.Fail++
			if !e.Required {
				s.AdvisoryFail++
			}
		default:
			s.Skip++
		}
		if e.Missing {
			s.DeclaredMissing++
			rep.DeclaredMissing++
		}
		s.durations = append(s.durations, float64(e.DurationMS))
	}); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	for _, s := range rep.Checks {
		s.P95MS = percentile(s.durations, 0.95)
	}
	// Ledgers.
	wsDirs, _ := os.ReadDir(dir)
	var waits []float64
	for _, w := range wsDirs {
		if !w.IsDir() || (keepWS != nil && !keepWS(w.Name())) {
			continue
		}
		packDirs, _ := os.ReadDir(filepath.Join(dir, w.Name()))
		for _, p := range packDirs {
			if !p.IsDir() {
				continue
			}
			type st struct {
				asked, answered time.Time
				grant, choice   string
				agent, used     bool
				action          string
				expires         time.Time
			}
			byID := map[string]*st{}
			var order []string
			err := eachLine(filepath.Join(dir, w.Name(), p.Name(), "ledger.jsonl"), func(b []byte) {
				var l ledgerLine
				if json.Unmarshal(b, &l) != nil || l.Decision == "" {
					return
				}
				s := byID[l.Decision]
				if s == nil {
					s = &st{}
					byID[l.Decision] = s
					order = append(order, l.Decision)
				}
				ts, _ := time.Parse(time.RFC3339, l.TS)
				switch l.Event {
				case "asked":
					s.asked, s.grant, s.action = ts, l.Grant, l.Action
					s.expires, _ = time.Parse(time.RFC3339, l.ExpiresAt)
				case "answered":
					if s.answered.IsZero() {
						s.answered, s.choice, s.agent = ts, l.Choice, l.ActorKind == "agent"
					}
				case "consumed":
					s.used = true
				case "expired":
					s.expires = ts
				}
			})
			if err != nil && !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			for _, id := range order {
				s := byID[id]
				if s.asked.IsZero() || s.asked.Before(since) {
					continue
				}
				a := &rep.Approvals
				a.Asked++
				if !s.answered.IsZero() {
					waits = append(waits, s.answered.Sub(s.asked).Minutes())
					if s.choice == s.grant && !s.agent {
						a.Approved++
					} else {
						a.Rejected++
					}
				}
				switch {
				case s.used:
					a.Used++
					if s.action == "promote" {
						a.PromotionsApplied++
					}
				case !s.expires.IsZero() && !now.Before(s.expires):
					a.Expired++
				}
			}
		}
	}
	rep.Approvals.MedianAnswerMin = percentile(waits, 0.5)
	if firstErr != nil {
		rep.ReadError = "pack events or ledgers could not be read; pack counts are incomplete"
	}
	return rep, firstErr
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(float64(len(s)-1)*p + 0.5)
	return s[i]
}

// packIDs returns the check ids sorted.
func (r PacksReport) packIDs() []string {
	out := make([]string, 0, len(r.Checks))
	for id := range r.Checks {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func packCounterLine(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " " + itoa(m[k])
	}
	return strings.Join(parts, ", ")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// writePacks renders the Packs section of the text report.
func writePacks(w io.Writer, p PacksReport) {
	fmt.Fprintln(w, "\n== Packs (tq pack)")
	if len(p.Checks) == 0 && len(p.Counters) == 0 && p.Approvals.Asked == 0 {
		fmt.Fprintln(w, "(no pack runs)")
	} else {
		if len(p.Checks) > 0 {
			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "check\truns\tok\tfail\tskip\tadvisory fails\tdeclared missing\tp95")
			for _, id := range p.packIDs() {
				s := p.Checks[id]
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%.0f ms\n", id, s.Runs, s.OK, s.Fail, s.Skip, s.AdvisoryFail, s.DeclaredMissing, s.P95MS)
			}
			tw.Flush()
		}
		packsWithCounters := make([]string, 0, len(p.Counters))
		for k := range p.Counters {
			packsWithCounters = append(packsWithCounters, k)
		}
		sort.Strings(packsWithCounters)
		for _, k := range packsWithCounters {
			fmt.Fprintf(w, "%s counters: %s\n", k, packCounterLine(p.Counters[k]))
		}
		a := p.Approvals
		fmt.Fprintf(w, "approvals asked %d, approved %d, rejected %d, expired %d, used %d, median time to answer %s; promotions applied %d\n",
			a.Asked, a.Approved, a.Rejected, a.Expired, a.Used, dur(a.MedianAnswerMin), a.PromotionsApplied)
	}
	if p.ReadError != "" {
		fmt.Fprintf(w, "note: %s\n", p.ReadError)
	}
}
