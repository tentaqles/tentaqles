package insights

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/decide"
)

// JevLog summarises $TQ_HOME/decide/log.jsonl.
type JevLog struct {
	Calls        int            `json:"calls"`
	Cached       int            `json:"cached"`
	CacheHitRate float64        `json:"cache_hit_rate"`
	Errors       int            `json:"errors"`
	CostUSD      float64        `json:"cost_usd"`
	P50MS        int64          `json:"p50_ms"`
	P95MS        int64          `json:"p95_ms"`
	ByPurpose    map[string]int `json:"by_purpose"`
}

// RuleJudgments counts one rule's judgments.
type RuleJudgments struct {
	Kind        string `json:"kind"`
	Rule        string `json:"rule"`
	Evaluated   int    `json:"evaluated"`
	WouldAsk    int    `json:"would_ask"`
	WouldDeny   int    `json:"would_deny"`
	AppliedAsk  int    `json:"applied_ask"`
	AppliedDeny int    `json:"applied_deny"`
	Enforced    bool   `json:"enforced"` // seen in enforce mode
}

// KindJudgments counts one judgment kind (guard, route, triage, ...).
type KindJudgments struct {
	Lines  int            `json:"lines"`
	Errors int            `json:"errors"`
	Modes  map[string]int `json:"modes"`
}

// RouteSummary is model routing in shadow or enforce mode.
type RouteSummary struct {
	Decisions int            `json:"decisions"`
	Errors    int            `json:"errors"`
	Picks     map[string]int `json:"picks"` // "parent->pick"
	Cheaper   int            `json:"cheaper"`
	Applied   int            `json:"applied"`
	// EstSavingsUSD assumes each cheaper pick would have saved the average
	// subagent transcript cost in the window times (1 - output price ratio
	// pick/parent). A rough upper-bound-ish figure, not a measurement.
	EstSavingsUSD      float64 `json:"est_savings_usd"`
	AvgSubagentCostUSD float64 `json:"avg_subagent_cost_usd"`
}

// TriageSummary is diff risk triage.
type TriageSummary struct {
	Low   int            `json:"low"`
	High  int            `json:"high"`
	Flags map[string]int `json:"flags"`
}

// JevReport is the Jev section of the report.
type JevReport struct {
	Log    JevLog                   `json:"log"`
	Kinds  map[string]KindJudgments `json:"kinds"`
	Rules  []RuleJudgments          `json:"rules"`
	Route  RouteSummary             `json:"route"`
	Triage TriageSummary            `json:"triage"`
}

// eachLine streams a JSONL file, calling fn for every line. A missing file
// is not an error.
func eachLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64<<10)
	var buf []byte
	for {
		line, skip, err := readLine(br, buf)
		if len(line) > 0 && !skip {
			buf = line
			fn(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func wsMatch(ws string, filter map[string]bool) bool {
	return len(filter) == 0 || filter[strings.ToLower(ws)]
}

// ReadJevLog summarises the Jev call log.
func ReadJevLog(path string, since time.Time, filter map[string]bool) (JevLog, error) {
	out := JevLog{ByPurpose: map[string]int{}}
	var ms []float64
	err := eachLine(path, func(line []byte) {
		var e decide.Entry
		if json.Unmarshal(line, &e) != nil || e.Time.Before(since) || !wsMatch(e.Workspace, filter) {
			return
		}
		out.Calls++
		p := e.Purpose
		if p == "" {
			p = "unknown"
		}
		out.ByPurpose[p]++
		if e.Cached {
			out.Cached++
		}
		if e.Error != "" {
			out.Errors++
		}
		out.CostUSD += e.CostUSD
		ms = append(ms, float64(e.MS))
	})
	if out.Calls > 0 {
		out.CacheHitRate = float64(out.Cached) / float64(out.Calls)
	}
	out.P50MS = int64(Percentile(ms, 50))
	out.P95MS = int64(Percentile(ms, 95))
	return out, err
}

// ReadJudgments summarises the judgment log. avgSubCost is the average
// subagent transcript cost, used for the routing savings estimate.
func ReadJudgments(path string, since time.Time, filter map[string]bool, avgSubCost float64) (JevReport, error) {
	r := JevReport{
		Kinds:  map[string]KindJudgments{},
		Route:  RouteSummary{Picks: map[string]int{}, AvgSubagentCostUSD: avgSubCost},
		Triage: TriageSummary{Flags: map[string]int{}},
	}
	rules := map[string]*RuleJudgments{}
	rule := func(kind, id string) *RuleJudgments {
		k := kind + "\x00" + id
		if rules[k] == nil {
			rules[k] = &RuleJudgments{Kind: kind, Rule: id}
		}
		return rules[k]
	}
	err := eachLine(path, func(line []byte) {
		var j decide.Judgment
		if json.Unmarshal(line, &j) != nil || j.Time.Before(since) || !wsMatch(j.Workspace, filter) {
			return
		}
		kind := j.Kind
		if kind == "" {
			kind = "unknown"
		}
		k := r.Kinds[kind]
		if k.Modes == nil {
			k.Modes = map[string]int{}
		}
		k.Lines++
		k.Modes[j.Mode]++
		if j.Error != "" {
			k.Errors++
		}
		r.Kinds[kind] = k

		switch kind {
		case "route":
			r.Route.Decisions++
			if j.Error != "" {
				r.Route.Errors++
				return
			}
			parent, pick := j.Extra["parent"], j.Extra["pick"]
			if pick == "" {
				return
			}
			r.Route.Picks[parent+"->"+pick]++
			if ratio := TierRatio(pick, parent); ratio < 1 {
				r.Route.Cheaper++
				r.Route.EstSavingsUSD += avgSubCost * (1 - ratio)
			}
			if j.Extra["applied"] == "true" {
				r.Route.Applied++
			}
			return
		case "triage":
			if j.Error != "" {
				return
			}
			if j.Extra["risk"] == "low" {
				r.Triage.Low++
			} else {
				r.Triage.High++
			}
			for _, f := range strings.Split(j.Extra["flags"], ",") {
				if f = strings.TrimSpace(f); f != "" {
					r.Triage.Flags[f]++
				}
			}
			return
		}
		// guard and any future rule-keyed kind (stop, skill, ...).
		ids := map[string]bool{}
		for id := range j.P {
			ids[id] = true
		}
		for id := range j.Would {
			ids[id] = true
		}
		for id := range j.Applied {
			ids[id] = true
		}
		for id := range ids {
			rj := rule(kind, id)
			rj.Evaluated++
			switch j.Would[id] {
			case "ask":
				rj.WouldAsk++
			case "deny":
				rj.WouldDeny++
			}
			switch j.Applied[id] {
			case "ask":
				rj.AppliedAsk++
			case "deny":
				rj.AppliedDeny++
			}
			if j.Mode == "enforce" {
				rj.Enforced = true
			}
		}
	})
	for _, rj := range rules {
		r.Rules = append(r.Rules, *rj)
	}
	sort.Slice(r.Rules, func(a, b int) bool {
		if r.Rules[a].Kind != r.Rules[b].Kind {
			return r.Rules[a].Kind < r.Rules[b].Kind
		}
		return r.Rules[a].Rule < r.Rules[b].Rule
	})
	if r.Rules == nil {
		r.Rules = []RuleJudgments{}
	}
	return r, err
}

// Percentile returns the p-th percentile (nearest rank) of xs, or 0.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(float64(len(s))*p/100+0.999999) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}
