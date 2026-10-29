package ml

// Drift (spec C §4.6): tq never queries data. It compares two local files,
// the reference and the current profile, each a CSV (tq builds the profile
// with encoding/csv) or a profile JSON tq.drift-profile/v1 holding only
// aggregates. The result names features and numbers, never labels.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// Schemas of the drift files.
const (
	ProfileSchema = "tq.drift-profile/v1"
	ResultSchema  = "tq.drift-result/v1"
	// MaxCategories: a categorical feature with more labels is compared on
	// hashed labels.
	MaxCategories  = 50
	driftMaxBytes  = 64 << 20
	emptyBinFloor  = 1e-4
	resultDir      = ".tq/drift"
	predictionName = "__prediction__"
)

// FeatureProfile is one feature of a profile.
type FeatureProfile struct {
	Type   string // numeric | categorical
	Edges  []float64
	Bins   []float64          // numeric counts
	Cats   map[string]float64 // categorical counts by label (or hash)
	Hashed bool
}

// Profile is a tq.drift-profile/v1 document (or one tq built from a CSV).
type Profile struct {
	Rows     int
	Features map[string]*FeatureProfile
}

type profileJSON struct {
	Schema    string                     `json:"schema"`
	Rows      int                        `json:"rows"`
	Source    string                     `json:"source"`
	CreatedAt string                     `json:"created_at"`
	Features  map[string]json.RawMessage `json:"features"`
}

type featureJSON struct {
	Type   string          `json:"type"`
	Edges  []float64       `json:"edges"`
	Counts json.RawMessage `json:"counts"`
	Hashed bool            `json:"hashed"`
}

// hashLabel is the label hash of the profile format: lowercase hex sha256 of
// the UTF-8 label. A "sha256:" prefix in a profile is accepted.
func hashLabel(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// LoadProfile reads a profile from a CSV (by extension) or a profile JSON.
// For a CSV, numeric bins use ref's edges when ref has the feature, else the
// deciles of the column; hashAll hashes every categorical label.
func LoadProfile(name string, raw []byte, ref *Profile, hashAll bool) (*Profile, error) {
	if strings.EqualFold(filepath.Ext(name), ".csv") {
		return csvProfile(raw, ref, hashAll)
	}
	var pj profileJSON
	if err := json.Unmarshal(raw, &pj); err != nil {
		return nil, fmt.Errorf("%s: not a profile JSON (%s): %v", name, ProfileSchema, err)
	}
	if pj.Schema != "" && pj.Schema != ProfileSchema {
		return nil, fmt.Errorf("%s: schema %q, want %s", name, pj.Schema, ProfileSchema)
	}
	if len(pj.Features) == 0 {
		return nil, fmt.Errorf("%s: the profile has no features", name)
	}
	p := &Profile{Rows: pj.Rows, Features: map[string]*FeatureProfile{}}
	for fname, fr := range pj.Features {
		var fj featureJSON
		if err := json.Unmarshal(fr, &fj); err != nil {
			return nil, fmt.Errorf("%s: feature %s: %v", name, fname, err)
		}
		f := &FeatureProfile{Type: fj.Type, Edges: fj.Edges, Hashed: fj.Hashed}
		switch fj.Type {
		case "numeric":
			if err := json.Unmarshal(fj.Counts, &f.Bins); err != nil {
				return nil, fmt.Errorf("%s: feature %s: numeric counts must be a list", name, fname)
			}
			if n, e := len(f.Bins), len(f.Edges); e == 0 || (n != e+1 && n != e-1) {
				return nil, fmt.Errorf("%s: feature %s: %d counts for %d edges (want edges+1 open bins or edges-1 closed bins)", name, fname, n, e)
			}
			for i := 1; i < len(f.Edges); i++ {
				if !(f.Edges[i] > f.Edges[i-1]) {
					return nil, fmt.Errorf("%s: feature %s: edges must increase", name, fname)
				}
			}
		case "categorical":
			if err := json.Unmarshal(fj.Counts, &f.Cats); err != nil {
				return nil, fmt.Errorf("%s: feature %s: categorical counts must be an object", name, fname)
			}
			if f.Hashed {
				norm := map[string]float64{}
				for k, v := range f.Cats {
					norm[strings.ToLower(strings.TrimPrefix(k, "sha256:"))] += v
				}
				f.Cats = norm
			}
		default:
			return nil, fmt.Errorf("%s: feature %s: type must be numeric or categorical", name, fname)
		}
		for _, v := range append(append([]float64{}, f.Bins...), values(f.Cats)...) {
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("%s: feature %s: counts must be non-negative numbers", name, fname)
			}
		}
		p.Features[fname] = f
	}
	return p, nil
}

func values(m map[string]float64) []float64 {
	out := make([]float64, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// csvProfile builds a profile from a CSV with a header row. A column whose
// non-empty values all parse as numbers is numeric; any other is
// categorical. Empty cells are skipped.
func csvProfile(raw []byte, ref *Profile, hashAll bool) (*Profile, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.ReuseRecord = true
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("csv: no header row: %v", err)
	}
	cols := append([]string(nil), head...)
	nums := make([][]float64, len(cols))
	cats := make([]map[string]float64, len(cols))
	numeric := make([]bool, len(cols))
	for i := range cols {
		cats[i] = map[string]float64{}
		numeric[i] = true
	}
	rows := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: line %d: %v", rows+2, err)
		}
		rows++
		for i := range cols {
			if i >= len(rec) {
				continue
			}
			v := strings.TrimSpace(rec[i])
			if v == "" {
				continue
			}
			cats[i][v]++
			if numeric[i] {
				if f, err := strconv.ParseFloat(v, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
					nums[i] = append(nums[i], f)
				} else {
					numeric[i], nums[i] = false, nil
				}
			}
		}
	}
	p := &Profile{Rows: rows, Features: map[string]*FeatureProfile{}}
	for i, name := range cols {
		if name == "" {
			continue
		}
		if numeric[i] && len(nums[i]) > 0 {
			f := &FeatureProfile{Type: "numeric"}
			if ref != nil && ref.Features[name] != nil && ref.Features[name].Type == "numeric" {
				rf := ref.Features[name]
				f.Edges = append([]float64(nil), rf.Edges...)
				f.Bins = binValues(nums[i], f.Edges, len(rf.Bins) == len(rf.Edges)-1)
			} else {
				f.Edges = deciles(nums[i])
				f.Bins = binValues(nums[i], f.Edges, false)
			}
			p.Features[name] = f
			continue
		}
		f := &FeatureProfile{Type: "categorical", Cats: cats[i]}
		if hashAll || len(f.Cats) > MaxCategories {
			f.Cats, f.Hashed = hashCats(f.Cats), true
		}
		p.Features[name] = f
	}
	return p, nil
}

// deciles are the distinct 10th..90th percentile cut points (open bins).
func deciles(vs []float64) []float64 {
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	var out []float64
	for k := 1; k <= 9; k++ {
		v := s[(k*(len(s)-1))/10]
		if len(out) == 0 || v > out[len(out)-1] {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		out = []float64{s[0]}
	}
	return out
}

// binValues counts values into open bins (-inf, e0], (e0, e1], ... (en, inf),
// or into closed bins [e0, e1], (e1, e2] ... with the outer values clamped.
func binValues(vs, edges []float64, closed bool) []float64 {
	n := len(edges) + 1
	if closed {
		n = len(edges) - 1
	}
	out := make([]float64, n)
	for _, v := range vs {
		i := sort.SearchFloat64s(edges, v)
		if closed {
			i--
			if i < 0 {
				i = 0
			}
			if i >= n {
				i = n - 1
			}
		}
		out[i]++
	}
	return out
}

func hashCats(m map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[hashLabel(k)] += v
	}
	return out
}

func proportions(cs []float64) []float64 {
	sum := 0.0
	for _, c := range cs {
		sum += c
	}
	out := make([]float64, len(cs))
	for i, c := range cs {
		if sum > 0 {
			out[i] = c / sum
		}
	}
	return out
}

// psi is Σ (aᵢ − eᵢ)·ln(aᵢ/eᵢ) over proportions, empty bins floored at 1e-4.
func psi(ref, cur []float64) float64 {
	e, a := proportions(ref), proportions(cur)
	v := 0.0
	for i := range e {
		ei, ai := math.Max(e[i], emptyBinFloor), math.Max(a[i], emptyBinFloor)
		v += (ai - ei) * math.Log(ai/ei)
	}
	return round(v)
}

// jsDistance is the Jensen-Shannon distance with log base 2 (0..1).
func jsDistance(ref, cur []float64) float64 {
	p, q := proportions(ref), proportions(cur)
	d := 0.0
	for i := range p {
		m := (p[i] + q[i]) / 2
		if p[i] > 0 {
			d += 0.5 * p[i] * math.Log2(p[i]/m)
		}
		if q[i] > 0 {
			d += 0.5 * q[i] * math.Log2(q[i]/m)
		}
	}
	return round(math.Sqrt(math.Max(d, 0)))
}

func round(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// FeatureResult is one feature's verdict.
type FeatureResult struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Value  float64 `json:"value"`
	Warn   float64 `json:"warn"`
	Fail   float64 `json:"fail"`
	Status string  `json:"status"` // ok | warn | fail
	Note   string  `json:"note,omitempty"`
}

// MetricResult is one drift.metrics.<m>.min bound.
type MetricResult struct {
	Name   string   `json:"name"`
	Value  *float64 `json:"value"`
	Min    float64  `json:"min"`
	Status string   `json:"status"`
}

// DriftResult is the tq.drift-result/v1 document. It is deterministic for
// the same inputs (no timestamps), so its hash is stable evidence.
type DriftResult struct {
	Schema          string          `json:"schema"`
	Model           string          `json:"model"`
	Metric          string          `json:"metric"`
	Status          string          `json:"status"`
	Reference       string          `json:"reference"`
	Current         string          `json:"current"`
	ReferenceSHA256 string          `json:"reference_sha256"`
	CurrentSHA256   string          `json:"current_sha256"`
	Features        []FeatureResult `json:"features"`
	WarnedShare     float64         `json:"warned_share"`
	MaxWarnShare    float64         `json:"max_warn_share"`
	Metrics         []MetricResult  `json:"metrics"`
	Reasons         []string        `json:"reasons"`
}

// EvaluateDrift compares the profiles under d's thresholds and checks the
// metric bounds against metrics (metricsErr when the file could not be read).
func EvaluateDrift(ref, cur *Profile, d Drift, metrics map[string]float64, metricsErr error) DriftResult {
	res := DriftResult{Schema: ResultSchema, Metric: d.Metric, Features: []FeatureResult{}, Metrics: []MetricResult{}, Reasons: []string{}}
	if res.Metric == "" {
		res.Metric = "psi"
	}
	res.MaxWarnShare = fl(d.MaxWarnShare, DefaultMaxWarnShare)
	names := make([]string, 0, len(ref.Features))
	for n := range ref.Features {
		names = append(names, n)
	}
	sort.Strings(names)
	warned, failed := 0, 0
	for _, n := range names {
		rf := ref.Features[n]
		fr := FeatureResult{Name: n, Type: rf.Type, Warn: fl(d.Warn, DefaultWarn), Fail: fl(d.Fail, DefaultFail)}
		if o, ok := d.Features[n]; ok {
			fr.Warn, fr.Fail = fl(o.Warn, fr.Warn), fl(o.Fail, fr.Fail)
		}
		cf := cur.Features[n]
		var rc, cc []float64
		switch {
		case cf == nil:
			fr.Status, fr.Note = "fail", "missing in the current profile"
		case cf.Type != rf.Type:
			fr.Status, fr.Note = "fail", "the type differs from the reference"
		case rf.Type == "numeric" && (!equalFloats(rf.Edges, cf.Edges) || len(rf.Bins) != len(cf.Bins)):
			fr.Status, fr.Note = "fail", "edges differ from the reference: rebuild the current profile with the reference edges"
		case rf.Type == "numeric":
			rc, cc = rf.Bins, cf.Bins
		default:
			rcats, ccats := rf.Cats, cf.Cats
			if rf.Hashed != cf.Hashed {
				if !rf.Hashed {
					rcats = hashCats(rcats)
				}
				if !cf.Hashed {
					ccats = hashCats(ccats)
				}
			}
			labels := map[string]bool{}
			for k := range rcats {
				labels[k] = true
			}
			for k := range ccats {
				labels[k] = true
			}
			keys := make([]string, 0, len(labels))
			for k := range labels {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				rc, cc = append(rc, rcats[k]), append(cc, ccats[k])
			}
		}
		if fr.Status == "" {
			if sum(rc) == 0 || sum(cc) == 0 {
				fr.Status, fr.Note = "fail", "no rows on one side"
			} else {
				if res.Metric == "js" {
					fr.Value = jsDistance(rc, cc)
				} else {
					fr.Value = psi(rc, cc)
				}
				switch {
				case fr.Value >= fr.Fail:
					fr.Status = "fail"
				case fr.Value >= fr.Warn:
					fr.Status = "warn"
				default:
					fr.Status = "ok"
				}
			}
		}
		switch fr.Status {
		case "fail":
			failed++
			res.Reasons = append(res.Reasons, fmt.Sprintf("feature %s: %s", n, failReason(fr)))
		case "warn":
			warned++
		}
		res.Features = append(res.Features, fr)
	}
	if len(names) > 0 {
		res.WarnedShare = round(float64(warned) / float64(len(names)))
	}
	if res.WarnedShare > res.MaxWarnShare {
		res.Reasons = append(res.Reasons, fmt.Sprintf("%d of %d features warn (share %.2f > max_warn_share %.2f)", warned, len(names), res.WarnedShare, res.MaxWarnShare))
	}
	mnames := make([]string, 0, len(d.Metrics))
	for n := range d.Metrics {
		mnames = append(mnames, n)
	}
	sort.Strings(mnames)
	for _, n := range mnames {
		mr := MetricResult{Name: n, Min: fl(d.Metrics[n].Min, 0), Status: "ok"}
		if v, ok := metrics[n]; ok && metricsErr == nil {
			vv := v
			mr.Value = &vv
			if v < mr.Min {
				mr.Status = "fail"
				res.Reasons = append(res.Reasons, fmt.Sprintf("metric %s is %g, below the minimum %g", n, v, mr.Min))
			}
		} else {
			mr.Status = "fail"
			why := "is not in the metrics file"
			if metricsErr != nil {
				why = "cannot be read: " + metricsErr.Error()
			}
			res.Reasons = append(res.Reasons, fmt.Sprintf("metric %s %s", n, why))
		}
		res.Metrics = append(res.Metrics, mr)
	}
	switch {
	case len(res.Reasons) > 0:
		res.Status = "fail"
	case warned > 0:
		res.Status = "warn"
	default:
		res.Status = "ok"
	}
	return res
}

func failReason(f FeatureResult) string {
	if f.Note != "" {
		return f.Note
	}
	return fmt.Sprintf("%g is at or above fail %g", f.Value, f.Fail)
}

func sum(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9*math.Max(1, math.Abs(a[i])) {
			return false
		}
	}
	return true
}

// readMetrics reads a metrics JSON: {"auc": 0.8}, {"metrics": {...}} or
// {"auc": {"value": 0.8}}. Non-numeric entries are ignored.
func readMetrics(raw []byte) (map[string]float64, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("not a JSON object: %v", err)
	}
	if inner, ok := top["metrics"].(map[string]any); ok {
		top = inner
	}
	out := map[string]float64{}
	for k, v := range top {
		switch x := v.(type) {
		case float64:
			out[k] = x
		case map[string]any:
			if f, ok := x["value"].(float64); ok {
				out[k] = f
			}
		}
	}
	return out, nil
}

// driftOptions control how runDrift may produce the current profile.
type driftOptions struct {
	// NoRun never runs current_command (the DoD check): a missing or stale
	// current is an error that says how to refresh it.
	NoRun bool
	// Confirm asks the human before a current_command that is not in the
	// trusted manifest runs. Nil refuses such a command.
	Confirm func(command string) error
	// Now is the clock (tests).
	Now time.Time
}

// ErrNoDrift means the model has no drift reference configured.
var ErrNoDrift = errors.New("no drift reference configured")

// trustedValue is the manifest's own value at a dotted path of packs.ml,
// or "" (the repo layer added it, or there is no manifest).
func trustedValue(in *packs.Input, path ...string) string {
	return scan.Scalar(scan.Get(trustedRoot(in.Manifest), path...))
}

func trustedCurrentCommand(in *packs.Input, st Settings, m Model, cmd string) bool {
	if cmd == "" {
		return false
	}
	if m.Drift != nil && m.Drift.CurrentCommand != "" {
		// a per-model command is trusted when the manifest lists it for the model
		if in.Manifest == nil {
			return false
		}
		pc := in.Manifest.Packs["ml"]
		for _, it := range scan.Items(scan.Get(&pc.Raw, "models")) {
			if scan.Scalar(scan.Get(it, "name")) == m.Name && scan.Scalar(scan.Get(it, "drift", "current_command")) == cmd {
				return true
			}
		}
		return false
	}
	return trustedValue(in, "drift", "current_command") == cmd
}

func classified(in *packs.Input, p string) bool {
	if in.Manifest == nil {
		return false
	}
	for _, c := range in.Manifest.Data.Classified(p) {
		if c != "public" && c != "internal" {
			return true
		}
	}
	return false
}

// ensureCurrent runs current_command when the current profile is missing or
// older than 24 h.
func ensureCurrent(ctx context.Context, in *packs.Input, st Settings, m Model, d Drift, o driftOptions) error {
	cur := filepath.Join(in.Root, filepath.FromSlash(d.Current))
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	fi, err := os.Stat(cur)
	fresh := err == nil && now.Sub(fi.ModTime()) <= CurrentMaxAge
	if fresh {
		return nil
	}
	state := "missing"
	if err == nil {
		state = "older than 24h"
	}
	if d.CurrentCommand == "" {
		if err != nil {
			return fmt.Errorf("the current profile %s is missing: produce it (drift.current_command is not set)", d.Current)
		}
		return nil // stale but no command: compare what is there
	}
	if o.NoRun {
		return fmt.Errorf("the current profile %s is %s: run tq pack ml drift %s to refresh it", d.Current, state, m.Name)
	}
	argv, err := FillCommand(d.CurrentCommand, map[string]string{"model": m.Name})
	if err != nil {
		return fmt.Errorf("drift.current_command: %v", err)
	}
	if !trustedCurrentCommand(in, st, m, d.CurrentCommand) {
		if o.Confirm == nil {
			return fmt.Errorf("drift.current_command comes from tq-packs.yaml, not the manifest: run it yourself (%s), or have the human confirm it with tq pack ml drift %s in a terminal", QuoteArgv(argv), m.Name)
		}
		if err := o.Confirm(QuoteArgv(argv)); err != nil {
			return fmt.Errorf("drift.current_command was not confirmed: %v", err)
		}
	}
	if in.Tools == nil {
		return errors.New("no tool runner")
	}
	cctx, cancel := context.WithTimeout(ctx, CurrentTimeout)
	defer cancel()
	out, err := in.Tools.Run(cctx, toolrun.Spec{Pack: "ml", Tool: argv[0], Args: argv[1:], Timeout: CurrentTimeout})
	switch {
	case err != nil:
		return fmt.Errorf("drift.current_command: %v", err)
	case out.TimedOut:
		return fmt.Errorf("drift.current_command timed out after %s", CurrentTimeout)
	case out.ExitCode != 0:
		return fmt.Errorf("drift.current_command exited %d:\n%s", out.ExitCode, out.Tail(20, classesFor(in)))
	}
	if _, err := os.Stat(cur); err != nil {
		return fmt.Errorf("drift.current_command finished but %s is missing", d.Current)
	}
	return nil
}

// runDrift evaluates one model and writes .tq/drift/<model>-result.json.
func runDrift(ctx context.Context, in *packs.Input, st Settings, m Model, o driftOptions) (DriftResult, error) {
	d := st.DriftFor(m)
	if d.Reference == "" || d.Current == "" {
		return DriftResult{}, fmt.Errorf("model %s: %w (set packs.ml.drift.reference and current)", m.Name, ErrNoDrift)
	}
	if err := ensureCurrent(ctx, in, st, m, d, o); err != nil {
		return DriftResult{}, err
	}
	rraw, err := readWork(in, d.Reference, driftMaxBytes)
	if err != nil {
		return DriftResult{}, fmt.Errorf("drift.reference %s: %v", d.Reference, err)
	}
	ref, err := LoadProfile(d.Reference, rraw, nil, classified(in, d.Reference))
	if err != nil {
		return DriftResult{}, err
	}
	craw, err := readWork(in, d.Current, driftMaxBytes)
	if err != nil {
		return DriftResult{}, fmt.Errorf("drift.current %s: %v", d.Current, err)
	}
	cur, err := LoadProfile(d.Current, craw, ref, classified(in, d.Current) || classified(in, d.Reference))
	if err != nil {
		return DriftResult{}, err
	}
	var metrics map[string]float64
	var merr error
	if len(d.Metrics) > 0 {
		if m.Metrics == "" {
			merr = errors.New("the model has no metrics file")
		} else if raw, err := readWork(in, m.Metrics, cardMaxBytes); err != nil {
			merr = err
		} else {
			metrics, merr = readMetrics(raw)
		}
		if merr != nil {
			merr = errors.New(strings.TrimSpace(strings.ReplaceAll(merr.Error(), in.Root, "")))
		}
	}
	res := EvaluateDrift(ref, cur, d, metrics, merr)
	res.Model, res.Reference, res.Current = m.Name, d.Reference, d.Current
	res.ReferenceSHA256, res.CurrentSHA256 = packs.HashBytes(rraw), packs.HashBytes(craw)
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return res, err
	}
	b = append(b, '\n')
	p := filepath.Join(in.Root, filepath.FromSlash(resultPath(m.Name)))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return res, err
	}
	packs.Count("ml", "drift:"+res.Status, 1)
	return res, nil
}

// resultPath is the repo path of a model's drift result.
func resultPath(model string) string { return resultDir + "/" + model + "-result.json" }

// --- the ml/drift check (DoD only with drift.in_dod: true) ---

func driftApplies(in *packs.Input) bool {
	st := loadIn(in)
	return st.Drift.InDoD && len(st.Models) > 0
}

func runDriftCheck(ctx context.Context, in *packs.Input) packs.Result {
	st := loadIn(in)
	var fs []packs.Finding
	n := 0
	for _, m := range st.Models {
		d := st.DriftFor(m)
		if d.Reference == "" {
			continue
		}
		n++
		res, err := runDrift(ctx, in, st, m, driftOptions{NoRun: true})
		if err != nil {
			fs = append(fs, packs.Finding{File: d.Current, Level: packs.LevelError, Rule: "drift-input", Message: err.Error()})
			continue
		}
		for _, r := range res.Reasons {
			fs = append(fs, packs.Finding{File: resultPath(m.Name), Level: packs.LevelError, Rule: "drift-fail", Message: "model " + m.Name + ": " + r})
		}
		if res.Status == "warn" {
			for _, f := range res.Features {
				if f.Status == "warn" {
					fs = append(fs, packs.Finding{File: resultPath(m.Name), Level: packs.LevelWarn, Rule: "drift-warn",
						Message: fmt.Sprintf("model %s: feature %s %s %g (warn %g)", m.Name, f.Name, res.Metric, f.Value, f.Warn)})
				}
			}
		}
	}
	return packs.Result{Status: packs.OK, Detail: plural(n, "model") + " checked for drift", Findings: fs}
}

// readWork reads a working-tree file (drift inputs and results are not
// staged: .tq/drift is gitignored), capped at max bytes.
func readWork(in *packs.Input, p string, max int64) ([]byte, error) {
	f, err := os.Open(filepath.Join(in.Root, filepath.FromSlash(p)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, unwrapPath(err))
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, max)
	}
	return b, nil
}

// unwrapPath drops the absolute path from an os error (messages name repo paths only).
func unwrapPath(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
