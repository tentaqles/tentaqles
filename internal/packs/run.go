package packs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// Schema is the --json schema of tq pack check.
const Schema = "tq.packs/v1"

const (
	// DefaultBudget is tq pack check's --budget default.
	DefaultBudget = 600 * time.Second
	// MaxBudget caps --budget.
	MaxBudget = 1800 * time.Second
	// BuiltinTimeout cancels a built-in check that runs longer.
	BuiltinTimeout = 10 * time.Second
)

// RunOptions narrow a run.
type RunOptions struct {
	Packs       []string // only these packs ("" = every enabled pack)
	BuiltinOnly bool
	// Classes redact external tool output tails (the workspace's exit classes).
	Classes []string
}

// Summary counts results.
type Summary struct {
	OK           int `json:"ok"`
	Fail         int `json:"fail"`
	Skip         int `json:"skip"`
	RequiredFail int `json:"required_fail"`
}

// Report is the tq.packs/v1 document.
type Report struct {
	Schema     string   `json:"schema"`
	Workspace  string   `json:"workspace"`
	Base       string   `json:"base"`
	ConfigHash string   `json:"config_hash"`
	Results    []Result `json:"results"`
	Summary    Summary  `json:"summary"`
}

// Failed reports whether a required check failed.
func (r Report) Failed() bool { return r.Summary.RequiredFail > 0 }

// MissingDetail is the detail of a declared tool that is not installed.
func MissingDetail(pack string) string {
	return fmt.Sprintf("declared in packs.%s.tools but not installed; install it or remove it from tools", pack)
}

// RunChecks runs the applicable checks of the enabled packs, in parallel
// (min(4, NumCPU) workers), within in.Budget, and appends one aggregate
// event line per result. Results are sorted by id.
func RunChecks(ctx context.Context, in *Input, o RunOptions) Report {
	rep := Report{Schema: Schema, Workspace: in.Workspace(), Base: in.Base, ConfigHash: in.Config.Hash(), Results: []Result{}}
	in.classes = o.Classes
	if !in.Budget.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, in.Budget)
		defer cancel()
	}
	type job struct {
		c  Check
		in *Input
	}
	var jobs []job
	for _, p := range in.Config.Enabled() {
		if len(o.Packs) > 0 && !contains(o.Packs, p) {
			continue
		}
		pin := in.ForPack(p)
		for _, c := range Checks(p) {
			if o.BuiltinOnly && c.Kind == External {
				continue
			}
			if !applies(c, pin) {
				continue
			}
			jobs = append(jobs, job{c, pin})
		}
		if r, ok := baselineGrew(pin); ok {
			rep.Results = append(rep.Results, r)
		}
	}
	workers := runtime.NumCPU()
	if workers > 4 {
		workers = 4
	}
	if workers < 1 {
		workers = 1
	}
	results := make([]Result, len(jobs))
	durs := make([]time.Duration, len(jobs))
	missing := make([]bool, len(jobs))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				start := time.Now()
				results[i], missing[i] = runOne(ctx, jobs[i].c, jobs[i].in, o)
				durs[i] = time.Since(start)
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	for i, r := range results {
		appendEvent(event{Workspace: in.Workspace(), Check: r.ID, Pack: r.Pack, Status: string(r.Status), Required: r.Required,
			DurationMS: durs[i].Milliseconds(), Missing: missing[i], Counts: levelCounts(r)})
	}
	rep.Results = append(rep.Results, results...)
	sort.SliceStable(rep.Results, func(i, j int) bool { return rep.Results[i].ID < rep.Results[j].ID })
	for _, r := range rep.Results {
		switch r.Status {
		case OK:
			rep.Summary.OK++
		case Fail:
			rep.Summary.Fail++
			if r.Required {
				rep.Summary.RequiredFail++
			}
		default:
			rep.Summary.Skip++
		}
	}
	return rep
}

func applies(c Check, in *Input) (ok bool) {
	if c.Applies == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			ok = true // a broken Applies runs the check rather than hiding it
		}
	}()
	return c.Applies(in)
}

// runOne runs one check; the bool reports a declared-missing tool.
func runOne(ctx context.Context, c Check, in *Input, o RunOptions) (Result, bool) {
	base := Result{ID: c.ID, Pack: c.Pack, Findings: []Finding{}}
	if ctx.Err() != nil {
		// The run's budget is spent: no probe, no tool, a fail.
		base.Status, base.Detail, base.Required = Fail, "timed out", c.Kind == Builtin || in.Settings().Declared(c.Tool)
		return base, false
	}
	if c.Kind == External {
		declared := in.Settings().Declared(c.Tool)
		base.Required = declared
		if in.Tools == nil {
			base.Status, base.Detail = Skip, "no tool runner"
			if declared {
				base.Status, base.Detail = Fail, MissingDetail(c.Pack)
			}
			return base, declared
		}
		p := in.Tools.Probe(c.Pack, c.Tool)
		if !p.Found {
			if declared {
				base.Status, base.Detail = Fail, MissingDetail(c.Pack)
				base.Tool = &ToolRun{Name: c.Tool}
				return base, true
			}
			base.Status, base.Detail = Skip, c.Tool+" not installed (not declared in tools)"
			return base, false
		}
		if c.MinVersion != "" && toolrun.CompareVersions(p.Version, c.MinVersion) < 0 {
			base.Status = Fail
			base.Detail = fmt.Sprintf("%s %s is below the minimum %s", c.Tool, orUnknown(p.Version), c.MinVersion)
			base.Tool = &ToolRun{Name: c.Tool, Version: p.Version, Path: p.Path}
			return base, false
		}
		cin := *in
		cin.Tools = checkTools{ToolRunner: in.Tools, timeout: c.Timeout}
		r := guarded(ctx, c, &cin, 0)
		r.ID, r.Pack, r.Required = c.ID, c.Pack, declared
		if r.Tool == nil {
			r.Tool = &ToolRun{Name: c.Tool, Version: p.Version, Path: p.Path}
		}
		downgraded := 0
		if in.Baseline != nil {
			for i, f := range r.Findings {
				if f.Level == LevelError && in.Baseline.Match(c.ID, f) {
					r.Findings[i].Level = LevelWarn
					downgraded++
				}
			}
		}
		return normalise(r, downgraded), false
	}
	r := guarded(ctx, c, in, BuiltinTimeout)
	r.ID, r.Pack, r.Required = c.ID, c.Pack, true
	return normalise(r, 0), false
}

// checkTools gives a check's tool runs the check's default timeout.
type checkTools struct {
	ToolRunner
	timeout time.Duration
}

func (t checkTools) Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error) {
	if s.Timeout <= 0 {
		s.Timeout = t.timeout
	}
	return t.ToolRunner.Run(ctx, s)
}

func orUnknown(v string) string {
	if v == "" {
		return "(unknown version)"
	}
	return v
}

// guarded runs c.Run with a timeout (0: the run's deadline only) and turns a
// panic or a blown deadline into a fail.
func guarded(ctx context.Context, c Check, in *Input, timeout time.Duration) Result {
	cctx := ctx
	cancel := func() {}
	if timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				msg := fmt.Sprint(p)
				if len(msg) > 200 {
					msg = msg[:200]
				}
				done <- Result{Status: Fail, Detail: "check panicked: " + msg}
			}
		}()
		done <- c.Run(cctx, in)
	}()
	select {
	case r := <-done:
		if errors.Is(cctx.Err(), context.DeadlineExceeded) && r.Status != Fail {
			return Result{Status: Fail, Detail: "timed out"}
		}
		return r
	case <-cctx.Done():
		return Result{Status: Fail, Detail: "timed out"}
	}
}

// normalise: an error finding fails the check; an explicit Fail stays Fail,
// unless every error finding it had was downgraded by the baseline
// (downgraded > 0 and none is left), which makes it OK.
func normalise(r Result, downgraded int) Result {
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	for i := range r.Findings {
		if r.Findings[i].Level != LevelError {
			r.Findings[i].Level = LevelWarn
		}
	}
	switch r.Status {
	case OK, Fail, Skip:
	default:
		r.Status = Fail
		if r.Detail == "" {
			r.Detail = "check returned no status"
		}
	}
	errs, _ := r.Counts()
	switch {
	case errs > 0 && r.Status != Skip:
		r.Status = Fail
	case r.Status == Fail && downgraded > 0 && errs == 0 && r.Detail != "timed out" && (r.Tool == nil || !r.Tool.TimedOut):
		r.Status = OK
	}
	return r
}

func levelCounts(r Result) map[string]int {
	e, w := r.Counts()
	return map[string]int{LevelError: e, LevelWarn: w}
}

// baselineGrew is the framework's <pack>/baseline result: a warn finding
// baseline-grew when the diff adds entries (by check and fingerprint) that
// the old side does not have.
func baselineGrew(in *Input) (Result, bool) {
	b := normRepoPath(in.Settings().Baseline)
	if b == "" || in.All || in.Base == "" {
		return Result{}, false
	}
	changed := false
	for _, c := range in.Changed {
		if normRepoPath(c.Path) == b && c.Status != 'D' {
			changed = true
		}
	}
	if !changed {
		return Result{}, false
	}
	now := baselineKeys(in, b, false)
	old := baselineKeys(in, b, true)
	added := 0
	for k := range now {
		if !old[k] {
			added++
		}
	}
	r := Result{ID: in.Pack() + "/baseline", Pack: in.Pack(), Status: OK, Required: false, Findings: []Finding{}}
	if added > 0 {
		r.Detail = fmt.Sprintf("baseline gained %d entries", added)
		r.Findings = append(r.Findings, Finding{File: b, Level: LevelWarn, Rule: "baseline-grew",
			Message: fmt.Sprintf("the baseline gained %d entries (%d before, %d now); a reviewer must accept them", added, len(old), len(now))})
	} else {
		r.Detail = fmt.Sprintf("baseline has %d entries (was %d), none new", len(now), len(old))
	}
	return r, true
}

// BaselineGrew reports whether the change grows the running pack's baseline
// file (for a pack flag's Raise, and for triage's baseline_change).
func BaselineGrew(in *Input) bool {
	r, ok := baselineGrew(in)
	return ok && len(r.Findings) > 0
}

// baselineKeys reads the entries' check+fingerprint keys of one side. An
// unreadable new side counts every entry as unknown (growth).
func baselineKeys(in *Input, p string, old bool) map[string]bool {
	var raw []byte
	var err error
	if old {
		raw, err = in.ReadOld(p)
	} else {
		raw, err = in.ReadNew(p, 16<<20)
	}
	out := map[string]bool{}
	if err != nil {
		return out
	}
	var f struct {
		Entries []struct {
			Check       string `json:"check"`
			Fingerprint string `json:"fingerprint"`
			Rule        string `json:"rule"`
			Location    string `json:"location"`
		} `json:"entries"`
	}
	if json.Unmarshal(raw, &f) != nil {
		if !old {
			out["\x00unparsable"] = true
		}
		return out
	}
	for i, e := range f.Entries {
		k := e.Check + "\x00" + e.Fingerprint
		if e.Fingerprint == "" {
			k = fmt.Sprintf("%s\x00%s\x00%s\x00#%d", e.Check, e.Rule, e.Location, i)
		}
		out[k] = true
	}
	return out
}

// EncodeJSON writes the report as tq.packs/v1 (two-space indent), the exact
// bytes `tq pack check --json` prints.
func EncodeJSON(w io.Writer, r Report) error {
	if r.Results == nil {
		r.Results = []Result{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// Text renders a report as the text output: one line per result plus up to
// 20 findings per check.
func (r Report) Text() string {
	var b strings.Builder
	if len(r.Results) == 0 {
		b.WriteString("no checks ran\n")
	}
	for _, res := range r.Results {
		st := string(res.Status)
		if res.Status == Fail && !res.Required {
			st = "fail (advisory)"
		}
		fmt.Fprintf(&b, "%-4s %s: %s\n", st, res.ID, res.Detail)
		for i, f := range res.Findings {
			if i == 20 {
				fmt.Fprintf(&b, "     … and %d more\n", len(res.Findings)-20)
				break
			}
			fmt.Fprintf(&b, "     %s\n", f.String())
		}
	}
	fmt.Fprintf(&b, "%d ok, %d fail (%d required), %d skip\n", r.Summary.OK, r.Summary.Fail, r.Summary.RequiredFail, r.Summary.Skip)
	return b.String()
}
