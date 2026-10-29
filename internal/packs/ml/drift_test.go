package ml

import (
	"context"
	"encoding/json"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", filepath.FromSlash(name))
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != string(got) {
		t.Fatalf("%s differs:\n%s", name, got)
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func TestPSIAndJS(t *testing.T) {
	if v := psi([]float64{50, 50}, []float64{25, 75}); !approx(v, 0.27465) {
		t.Fatalf("psi %v", v)
	}
	if v := psi([]float64{10, 0}, []float64{10, 0}); v != 0 {
		t.Fatalf("identical psi %v", v)
	}
	if v := jsDistance([]float64{1, 0}, []float64{0, 1}); !approx(v, 1) {
		t.Fatalf("js %v", v)
	}
	if v := jsDistance([]float64{3, 1}, []float64{3, 1}); !approx(v, 0) {
		t.Fatalf("js same %v", v)
	}
}

const driftConfig = `
ml:
  enabled: true
  models:
    - { name: churn, card: models/churn/MODEL_CARD.md, metrics: reports/churn/metrics.json }
  drift:
    reference: data/profiles/churn-train.json
    current: .tq/drift/churn-current.json
    warn: 0.1
    fail: 0.25
    max_warn_share: 0.5
    features: { monthly_fee: { warn: 0.0005 } }
    metrics: { auc: { min: 0.78 } }
`

func driftRepo(t *testing.T, fixture string, files map[string]string) *packs.Input {
	t.Helper()
	root := testutil.TempDir(t)
	all := map[string]string{
		"data/profiles/churn-train.json": readFixture(t, fixture+"/reference.json"),
		".tq/drift/churn-current.json":   readFixture(t, fixture+"/current.json"),
		"reports/churn/metrics.json":     readFixture(t, "drift-basic/metrics.json"),
	}
	for k, v := range files {
		all[k] = v
	}
	writeFiles(t, root, all)
	in := &packs.Input{Root: root, Config: configFrom(t, driftConfig, "")}
	return in.ForPack("ml")
}

func TestDriftGolden(t *testing.T) {
	isolate(t)
	in := driftRepo(t, "drift-basic", nil)
	st := load(in.Settings())
	m, _ := st.Model("churn")
	res, err := runDrift(context.Background(), in, st, m, driftOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "fail" {
		t.Fatalf("status %s", res.Status)
	}
	byName := map[string]FeatureResult{}
	for _, f := range res.Features {
		byName[f.Name] = f
	}
	if byName["tenure_days"].Status != "warn" || byName["monthly_fee"].Status != "warn" || byName["plan"].Status != "fail" ||
		byName["region"].Status != "ok" || byName["__prediction__"].Status != "ok" {
		t.Fatalf("%+v", res.Features)
	}
	raw, err := os.ReadFile(filepath.Join(in.Root, ".tq", "drift", "churn-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "drift-basic/want-result.json", raw)
	for _, label := range []string{"basic", "enterprise", "3b9c358f"} {
		if strings.Contains(string(raw), label) {
			t.Fatalf("the result names a label %q", label)
		}
	}
}

func TestDriftPassesAndMetricMin(t *testing.T) {
	isolate(t)
	cfg := strings.Replace(driftConfig, "features: { monthly_fee: { warn: 0.0005 } }", "features: { plan: { warn: 0.5, fail: 1 } }", 1)
	in := driftRepo(t, "drift-basic", nil)
	in.Config = configFrom(t, cfg, "")
	in = in.ForPack("ml")
	st := load(in.Settings())
	m, _ := st.Model("churn")
	res, err := runDrift(context.Background(), in, st, m, driftOptions{})
	if err != nil || res.Status != "warn" {
		t.Fatalf("%v %+v", err, res)
	}
	// The metric bound fails when the metrics file is below it.
	writeFiles(t, in.Root, map[string]string{"reports/churn/metrics.json": `{"metrics": {"auc": 0.7}}`})
	res, _ = runDrift(context.Background(), in, st, m, driftOptions{})
	if res.Status != "fail" || len(res.Metrics) != 1 || res.Metrics[0].Status != "fail" {
		t.Fatalf("%+v", res)
	}
	// Missing metric fails too.
	writeFiles(t, in.Root, map[string]string{"reports/churn/metrics.json": `{"logloss": 0.4}`})
	res, _ = runDrift(context.Background(), in, st, m, driftOptions{})
	if res.Status != "fail" {
		t.Fatalf("%+v", res)
	}
}

func TestDriftMaxWarnShare(t *testing.T) {
	isolate(t)
	cfg := strings.Replace(driftConfig, "features: { monthly_fee: { warn: 0.0005 } }", "features: { plan: { warn: 0.001, fail: 1 }, monthly_fee: { warn: 0.0005 } }", 1)
	cfg = strings.Replace(cfg, "max_warn_share: 0.5", "max_warn_share: 0.5", 1)
	in := driftRepo(t, "drift-basic", nil)
	in.Config = configFrom(t, cfg, "")
	in = in.ForPack("ml")
	st := load(in.Settings())
	m, _ := st.Model("churn")
	res, _ := runDrift(context.Background(), in, st, m, driftOptions{})
	// tenure, monthly_fee, plan warn: 3 of 5 > 0.5.
	if res.Status != "fail" || !approx(res.WarnedShare, 0.6) {
		t.Fatalf("%+v", res)
	}
}

func TestDriftEdgesDiffer(t *testing.T) {
	isolate(t)
	in := driftRepo(t, "drift-edges", nil)
	st := load(in.Settings())
	m, _ := st.Model("churn")
	res, _ := runDrift(context.Background(), in, st, m, driftOptions{})
	if res.Status != "fail" || !strings.Contains(res.Features[0].Note, "reference edges") {
		t.Fatalf("%+v", res)
	}
}

func TestDriftCSV(t *testing.T) {
	isolate(t)
	in := driftRepo(t, "drift-basic", map[string]string{
		"data/ref.csv": readFixture(t, "drift-csv/reference.csv"),
		"data/cur.csv": readFixture(t, "drift-csv/current.csv"),
	})
	cfg := strings.Replace(driftConfig, "data/profiles/churn-train.json", "data/ref.csv", 1)
	cfg = strings.Replace(cfg, ".tq/drift/churn-current.json", "data/cur.csv", 1)
	in.Config = configFrom(t, cfg, "")
	in = in.ForPack("ml")
	st := load(in.Settings())
	m, _ := st.Model("churn")
	res, err := runDrift(context.Background(), in, st, m, driftOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(in.Root, ".tq", "drift", "churn-result.json"))
	golden(t, "drift-csv/want-result.json", raw)
	if strings.Contains(string(raw), "tester") {
		t.Fatal("labels leaked")
	}
	var tenure FeatureResult
	for _, f := range res.Features {
		if f.Name == "tenure_days" {
			tenure = f
		}
	}
	if tenure.Type != "numeric" || tenure.Status != "fail" {
		t.Fatalf("%+v", res.Features)
	}
}

func TestProfileHashedAgainstPlain(t *testing.T) {
	ref, err := LoadProfile("ref.json", []byte(`{"features":{"plan":{"type":"categorical","counts":{"basic":5,"pro":5}}}}`), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := LoadProfile("cur.json", []byte(`{"features":{"plan":{"type":"categorical","hashed":true,"counts":{"`+hashLabel("basic")+`":5,"sha256:`+hashLabel("pro")+`":5}}}}`), ref, false)
	if err != nil {
		t.Fatal(err)
	}
	res := EvaluateDrift(ref, cur, Drift{Metric: "psi"}, nil, nil)
	if res.Status != "ok" || res.Features[0].Value != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestProfileErrors(t *testing.T) {
	for _, bad := range []string{
		`{"features":{"x":{"type":"numeric","edges":[1,2],"counts":[1,2]}}}`,   // counts length
		`{"features":{"x":{"type":"numeric","edges":[2,1],"counts":[1,2,3]}}}`, // edges not increasing
		`{"features":{"x":{"type":"text","counts":[1]}}}`,
		`not json`,
	} {
		if _, err := LoadProfile("p.json", []byte(bad), nil, false); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestCurrentCommandRuns(t *testing.T) {
	isolate(t)
	in := driftRepo(t, "drift-basic", nil)
	cur := filepath.Join(in.Root, ".tq", "drift", "churn-current.json")
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(cur, old, old)
	writeFiles(t, in.Root, map[string]string{"fresh.json": readFixture(t, "drift-basic/reference.json")})
	cmd := `"` + fakeCLI(t) + `" copy fresh.json .tq/drift/churn-current.json`
	cfg := strings.Replace(driftConfig, "current: .tq/drift/churn-current.json", "current: .tq/drift/churn-current.json\n    current_command: '"+cmd+"'", 1)
	in.Config = configFrom(t, cfg, "")
	in.Tools = packs.NewToolRunner(in.Root, in.Config)
	in = in.ForPack("ml")
	st := load(in.Settings())
	m, _ := st.Model("churn")
	// The Input has no manifest, so the command is not the trusted layer's:
	// it runs only after a confirmation.
	asked := ""
	res, err := runDrift(context.Background(), in, st, m, driftOptions{Confirm: func(c string) error { asked = c; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if asked == "" || !strings.Contains(asked, "copy fresh.json") {
		t.Fatalf("the repo-layer command was not confirmed: %q", asked)
	}
	// The fresh current equals the reference: no drift.
	if res.Features[0].Value != 0 {
		t.Fatalf("%+v", res.Features)
	}
	// Refused without a confirmation.
	os.Chtimes(cur, old, old)
	if _, err := runDrift(context.Background(), in, st, m, driftOptions{}); err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Fatalf("an unconfirmed repo command must not run: %v", err)
	}
	// NoRun (the DoD check) reports a stale current instead of running.
	if _, err := runDrift(context.Background(), in, st, m, driftOptions{NoRun: true}); err == nil || !strings.Contains(err.Error(), "older than") {
		t.Fatalf("stale current: %v", err)
	}
}

func TestDriftCheckInDoD(t *testing.T) {
	isolate(t)
	in := driftRepo(t, "drift-basic", nil)
	if driftApplies(in) {
		t.Fatal("in_dod is false by default")
	}
	in.Config = configFrom(t, driftConfig+"    in_dod: true\n", "")
	in = in.ForPack("ml")
	if !driftApplies(in) {
		t.Fatal("in_dod: true applies")
	}
	r := runDriftCheck(context.Background(), in)
	if r.Status != packs.OK || len(rules(r.Findings, packs.LevelError)) != 1 {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.Findings[0].Message, "plan") {
		t.Fatalf("the finding names the feature: %+v", r.Findings)
	}
}

func TestDriftResultJSONStable(t *testing.T) {
	ref, _ := LoadProfile("r.json", []byte(readFixture(t, "drift-basic/reference.json")), nil, false)
	a := EvaluateDrift(ref, ref, Drift{Metric: "js"}, nil, nil)
	b := EvaluateDrift(ref, ref, Drift{Metric: "js"}, nil, nil)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) != string(y) || a.Status != "ok" {
		t.Fatalf("%s", x)
	}
}
