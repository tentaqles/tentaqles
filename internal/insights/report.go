// Package insights measures how Claude Code is used across tq identities:
// cost and context size, shell health, guard outcomes, Jev decisions and
// background review sessions. It reads transcripts and logs read-only,
// streaming them line by line, and reports aggregates only: counts, token
// totals, session ids and project directory names, never transcript content.
package insights

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// SchemaVersion identifies the --json layout. Bump it on breaking changes.
const SchemaVersion = "tq.insights/v1"

// DefaultIdentity labels transcripts under ~/.claude (no tq identity).
const DefaultIdentity = "default"

// Source is one identity's transcript root (the "projects" directory).
type Source struct {
	Name string
	Dir  string
}

// Options configure a run.
type Options struct {
	Since      time.Time
	Until      time.Time // report time; zero means now
	Workspaces []string  // identity names to include; empty means all
	Top        int
	Sources    []Source // nil means DiscoverSources()
	DecideDir  string   // "" means $TQ_HOME/decide
}

// SessionRow is one main session in the top lists.
type SessionRow struct {
	Identity    string    `json:"identity"`
	SessionID   string    `json:"session_id"`
	Project     string    `json:"project"`
	Class       string    `json:"class"`
	Model       string    `json:"model"`
	Start       time.Time `json:"start"`
	DurationMin float64   `json:"duration_min"`
	PeakContext int64     `json:"peak_context"`
	CostUSD     float64   `json:"cost_usd"` // estimate incl. subagents
	Subagents   int       `json:"subagents"`
}

// HookLatency is per-event hook duration.
type HookLatency struct {
	N     int     `json:"n"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
}

// ShellReport adds failure rates to ShellStats.
type ShellReport struct {
	ShellStats
	BashFailureRate       float64 `json:"bash_failure_rate"`
	PowerShellFailureRate float64 `json:"powershell_failure_rate"`
	FailureRate           float64 `json:"failure_rate"` // both shells combined
}

// ReviewReport counts headless security-review sessions.
type ReviewReport struct {
	Sessions int     `json:"sessions"`
	Opus     int     `json:"opus"`
	CostUSD  float64 `json:"cost_usd"`
}

// IdentityReport is one identity (or the total).
type IdentityReport struct {
	Name                string                 `json:"name"`
	Sessions            int                    `json:"sessions"`
	SessionsByClass     map[string]int         `json:"sessions_by_class"`
	SubagentTranscripts int                    `json:"subagent_transcripts"`
	CostUSD             float64                `json:"cost_usd"`
	RecordedCostUSD     float64                `json:"recorded_cost_usd"`
	CostByTier          map[string]float64     `json:"cost_by_tier"`
	Tokens              Tokens                 `json:"tokens"`
	PeakContextMax      int64                  `json:"peak_context_max"`
	SessionsOver400k    int                    `json:"sessions_over_400k"`
	Shell               ShellReport            `json:"shell"`
	Guard               GuardStats             `json:"guard"`
	HookLatency         map[string]HookLatency `json:"hook_latency"`
	Reviews             ReviewReport           `json:"reviews"`

	hooks   map[string][]float64
	subCost float64
}

func newIdentity(name string) *IdentityReport {
	return &IdentityReport{Name: name, SessionsByClass: map[string]int{}, CostByTier: map[string]float64{},
		Guard: newGuard(), HookLatency: map[string]HookLatency{}, hooks: map[string][]float64{}}
}

// WorktreeCount is filled in by the command (it owns the git scanning).
type WorktreeCount struct {
	Total    int `json:"total"`
	Prunable int `json:"prunable"`
}

// Target is one 30-day goal from the workflow plan next to its value now.
type Target struct {
	ID       string  `json:"id"`
	Goal     string  `json:"goal"`
	Current  float64 `json:"current"`
	Display  string  `json:"display"`
	Baseline string  `json:"baseline"`
	Status   string  `json:"status"` // met | not met | check
}

// Report is the whole result; its JSON form is the stable --json schema.
type Report struct {
	Schema       string           `json:"schema"`
	GeneratedAt  time.Time        `json:"generated_at"`
	Since        time.Time        `json:"since"`
	Until        time.Time        `json:"until"`
	WindowDays   float64          `json:"window_days"`
	Identities   []IdentityReport `json:"identities"`
	Total        IdentityReport   `json:"total"`
	TopByContext []SessionRow     `json:"top_by_context"`
	TopByCost    []SessionRow     `json:"top_by_cost"`
	Jev          JevReport        `json:"jev"`
	Worktrees    *WorktreeCount   `json:"worktrees"`
	Targets      []Target         `json:"targets"`
	Pricing      []PriceRow       `json:"-"`
}

// DiscoverSources lists the transcript roots: one per tq identity that has
// a Claude config dir, plus ~/.claude for the default (pre-tq) setup.
func DiscoverSources() []Source {
	var out []Source
	if ents, err := os.ReadDir(paths.IdentitiesRoot()); err == nil {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			d := filepath.Join(paths.IdentitiesRoot(), e.Name(), "claude", "projects")
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				out = append(out, Source{Name: e.Name(), Dir: d})
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		d := filepath.Join(h, ".claude", "projects")
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, Source{Name: DefaultIdentity, Dir: d})
		}
	}
	return out
}

type job struct {
	src  string
	root string
	path string
}

type result struct {
	src string
	fs  *FileStats
}

// Run builds the report. It only reads.
func Run(o Options) (*Report, error) {
	if o.Until.IsZero() {
		o.Until = time.Now().UTC()
	}
	if o.Top <= 0 {
		o.Top = 10
	}
	if o.Sources == nil {
		o.Sources = DiscoverSources()
	}
	if o.DecideDir == "" {
		o.DecideDir = filepath.Join(paths.Home(), "decide")
	}
	filter := map[string]bool{}
	for _, w := range o.Workspaces {
		for _, p := range strings.Split(w, ",") {
			if p = strings.TrimSpace(p); p != "" {
				filter[strings.ToLower(p)] = true
			}
		}
	}

	var jobs []job
	for _, s := range o.Sources {
		if len(filter) > 0 && !filter[strings.ToLower(s.Name)] {
			continue
		}
		_ = filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			if fi, err := d.Info(); err != nil || fi.ModTime().Before(o.Since) {
				return nil
			}
			jobs = append(jobs, job{src: s.Name, root: s.Dir, path: p})
			return nil
		})
	}

	results := make(chan result)
	work := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				if fs := scanFile(j, o.Since); fs != nil {
					results <- result{j.src, fs}
				}
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			work <- j
		}
		close(work)
		wg.Wait()
		close(results)
	}()

	idents := map[string]*IdentityReport{}
	sessions := map[string]*SessionRow{} // identity\x00session id
	var subs []result
	for r := range results {
		id := idents[r.src]
		if id == nil {
			id = newIdentity(r.src)
			idents[r.src] = id
		}
		if r.fs.Start.IsZero() {
			continue // nothing in the window
		}
		id.addFile(r.fs)
		if r.fs.Subagent {
			subs = append(subs, r)
			continue
		}
		row := &SessionRow{Identity: r.src, SessionID: r.fs.SessionID, Project: r.fs.Project, Class: r.fs.Class,
			Model: r.fs.Model, Start: r.fs.Start, DurationMin: r.fs.End.Sub(r.fs.Start).Minutes(),
			PeakContext: r.fs.PeakContext, CostUSD: r.fs.CostUSD}
		sessions[r.src+"\x00"+r.fs.SessionID] = row
	}
	for _, r := range subs {
		if row := sessions[r.src+"\x00"+r.fs.ParentID]; row != nil {
			row.CostUSD += r.fs.CostUSD
			row.Subagents++
		}
	}

	rep := &Report{Schema: SchemaVersion, GeneratedAt: time.Now().UTC(), Since: o.Since, Until: o.Until, Pricing: Prices}
	rep.WindowDays = o.Until.Sub(o.Since).Hours() / 24
	total := newIdentity("total")
	names := make([]string, 0, len(idents))
	for n := range idents {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		id := idents[n]
		id.finish()
		total.merge(id)
		rep.Identities = append(rep.Identities, *id)
	}
	total.finish()
	rep.Total = *total
	if rep.Identities == nil {
		rep.Identities = []IdentityReport{}
	}

	rows := make([]SessionRow, 0, len(sessions))
	for _, r := range sessions {
		rows = append(rows, *r)
	}
	rep.TopByContext = topN(rows, o.Top, func(a, b SessionRow) bool { return a.PeakContext > b.PeakContext })
	rep.TopByCost = topN(rows, o.Top, func(a, b SessionRow) bool { return a.CostUSD > b.CostUSD })

	avgSub := 0.0
	if total.SubagentTranscripts > 0 {
		avgSub = total.subCost / float64(total.SubagentTranscripts)
	}
	var err error
	rep.Jev, err = ReadJudgments(filepath.Join(o.DecideDir, "judgments.jsonl"), o.Since, filter, avgSub)
	if err != nil {
		return nil, err
	}
	rep.Jev.Log, err = ReadJevLog(filepath.Join(o.DecideDir, "log.jsonl"), o.Since, filter)
	if err != nil {
		return nil, err
	}
	rep.Targets = Targets(rep)
	return rep, nil
}

func scanFile(j job, since time.Time) *FileStats {
	f, err := os.Open(j.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := ScanTranscript(f, since)
	if err != nil {
		return nil
	}
	rel, _ := filepath.Rel(j.root, j.path)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	st.Project = parts[0]
	st.SessionID = strings.TrimSuffix(parts[len(parts)-1], ".jsonl")
	for i, p := range parts {
		if p == "subagents" && i > 0 {
			st.Subagent = true
			st.ParentID = parts[i-1]
		}
	}
	return st
}

func (id *IdentityReport) addFile(f *FileStats) {
	id.CostUSD += f.CostUSD
	for t, c := range f.CostByTier {
		id.CostByTier[t] += c
	}
	id.Tokens.add(f.Tokens)
	id.Shell.add(f.Shell)
	id.Guard.add(f.Guard)
	for ev, ds := range f.Hooks {
		id.hooks[ev] = append(id.hooks[ev], ds...)
	}
	if f.Subagent {
		id.SubagentTranscripts++
		id.subCost += f.CostUSD
		return
	}
	id.Sessions++
	id.SessionsByClass[f.Class]++
	id.RecordedCostUSD += f.Recorded
	if f.PeakContext > id.PeakContextMax {
		id.PeakContextMax = f.PeakContext
	}
	if f.PeakContext > ContextLimit {
		id.SessionsOver400k++
	}
	if f.Class == ClassSecurityReview {
		id.Reviews.Sessions++
		id.Reviews.CostUSD += f.CostUSD
		if TierOf(f.Model) == "opus" {
			id.Reviews.Opus++
		}
	}
}

func (id *IdentityReport) merge(o *IdentityReport) {
	id.Sessions += o.Sessions
	for k, v := range o.SessionsByClass {
		id.SessionsByClass[k] += v
	}
	id.SubagentTranscripts += o.SubagentTranscripts
	id.subCost += o.subCost
	id.CostUSD += o.CostUSD
	id.RecordedCostUSD += o.RecordedCostUSD
	for k, v := range o.CostByTier {
		id.CostByTier[k] += v
	}
	id.Tokens.add(o.Tokens)
	if o.PeakContextMax > id.PeakContextMax {
		id.PeakContextMax = o.PeakContextMax
	}
	id.SessionsOver400k += o.SessionsOver400k
	id.Shell.add(o.Shell.ShellStats)
	id.Guard.add(o.Guard)
	for ev, ds := range o.hooks {
		id.hooks[ev] = append(id.hooks[ev], ds...)
	}
	id.Reviews.Sessions += o.Reviews.Sessions
	id.Reviews.Opus += o.Reviews.Opus
	id.Reviews.CostUSD += o.Reviews.CostUSD
}

func rate(errs, calls int) float64 {
	if calls == 0 {
		return 0
	}
	return float64(errs) / float64(calls)
}

func (id *IdentityReport) finish() {
	s := &id.Shell
	s.BashFailureRate = rate(s.Bash.Errors, s.Bash.Calls)
	s.PowerShellFailureRate = rate(s.PowerShell.Errors, s.PowerShell.Calls)
	s.FailureRate = rate(s.Bash.Errors+s.PowerShell.Errors, s.Bash.Calls+s.PowerShell.Calls)
	for ev, ds := range id.hooks {
		id.HookLatency[ev] = HookLatency{N: len(ds), P50MS: Percentile(ds, 50), P95MS: Percentile(ds, 95)}
	}
}

func topN(rows []SessionRow, n int, less func(a, b SessionRow) bool) []SessionRow {
	s := append([]SessionRow(nil), rows...)
	sort.SliceStable(s, func(i, j int) bool {
		if less(s[i], s[j]) != less(s[j], s[i]) {
			return less(s[i], s[j])
		}
		return s[i].SessionID < s[j].SessionID
	})
	if len(s) > n {
		s = s[:n]
	}
	if s == nil {
		s = []SessionRow{}
	}
	return s
}

// Baselines from the 90-day session mining (2026-07-10 to 2026-10-08).
const (
	baselineDays        = 90.0
	baselineReviews     = 373.0
	baselineStopBlocks  = 71.0
	baselineExit127     = 34.0 // tq_run.sh exit-127 failures
	nearZeroPer30d      = 3.0  // "near zero": at most 3 per 30 days
	reviewReductionGoal = 0.80
)

// per30 normalises a count in the report window to 30 days.
func per30(n float64, days float64) float64 {
	if days < 1 {
		days = 1
	}
	return n * 30 / days
}

func status(ok bool) string {
	if ok {
		return "met"
	}
	return "not met"
}

// Targets compares the report with the plan's 30-day targets.
func Targets(r *Report) []Target {
	t := r.Total
	d := r.WindowDays
	var out []Target

	out = append(out, Target{ID: "sessions_over_400k", Goal: "no sessions over 400k context",
		Current: float64(t.SessionsOver400k), Display: strconv.Itoa(t.SessionsOver400k),
		Baseline: "25 in 90d", Status: status(t.SessionsOver400k == 0)})

	sb := per30(float64(t.Guard.StopBlocks), d)
	out = append(out, Target{ID: "stop_hook_blocks_per_30d", Goal: fmt.Sprintf("Stop-hook blocks near zero (<= %.0f per 30d)", nearZeroPer30d),
		Current: sb, Display: fmt.Sprintf("%.1f (%d in window)", sb, t.Guard.StopBlocks),
		Baseline: fmt.Sprintf("%.1f per 30d (%.0f in 90d)", per30(baselineStopBlocks, baselineDays), baselineStopBlocks),
		Status:   status(sb <= nearZeroPer30d)})

	x := per30(float64(t.Shell.Exit127), d)
	out = append(out, Target{ID: "exit_127_per_30d", Goal: fmt.Sprintf("exit-127 errors near zero (<= %.0f per 30d)", nearZeroPer30d),
		Current: x, Display: fmt.Sprintf("%.1f (%d in window)", x, t.Shell.Exit127),
		Baseline: fmt.Sprintf("%.1f per 30d (%.0f tq_run.sh in 90d)", per30(baselineExit127, baselineDays), baselineExit127),
		Status:   status(x <= nearZeroPer30d)})

	fr := t.Shell.FailureRate
	out = append(out, Target{ID: "shell_failure_rate", Goal: "shell failure rate under 5%",
		Current: fr, Display: fmt.Sprintf("%.2f%% (Bash %.1f%%, PowerShell %.1f%%)", fr*100, t.Shell.BashFailureRate*100, t.Shell.PowerShellFailureRate*100),
		Baseline: "5.0% (Bash 4.2%, PowerShell 15.2%)", Status: status(fr < 0.05)})

	base := per30(baselineReviews, baselineDays)
	goal := base * (1 - reviewReductionGoal)
	rv := per30(float64(t.Reviews.Sessions), d)
	out = append(out, Target{ID: "review_sessions_per_30d", Goal: fmt.Sprintf("Opus review sessions down >= 80%% (<= %.1f per 30d)", goal),
		Current: rv, Display: fmt.Sprintf("%.1f (%d in window, %d on Opus)", rv, t.Reviews.Sessions, t.Reviews.Opus),
		Baseline: fmt.Sprintf("%.1f per 30d (%.0f in 90d)", base, baselineReviews), Status: status(rv <= goal)})

	var enforced []string
	for _, rj := range r.Jev.Rules {
		if rj.Enforced {
			enforced = append(enforced, rj.Rule)
		}
	}
	jt := Target{ID: "jev_enforce_precision", Goal: "Jev rules enforced only at eval precision >= 0.9",
		Current: float64(len(enforced)), Baseline: "0 enforced (shadow only)"}
	if len(enforced) == 0 {
		jt.Display, jt.Status = "0 rules enforced", "met"
	} else {
		jt.Display = fmt.Sprintf("%d enforced: %s (confirm each with tq decide eval)", len(enforced), strings.Join(enforced, ", "))
		jt.Status = "check"
	}
	out = append(out, jt)
	return out
}
