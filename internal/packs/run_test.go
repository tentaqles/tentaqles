package packs

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

var update = flag.Bool("update", false, "rewrite golden files")

// goldenInput is the fixture behind testdata/check-golden.json: the data pack
// with sqlfluff declared, the ml pack with ruff installed but too old and dvc
// absent and undeclared.
func goldenInput(t *testing.T) *Input {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "models/orders.sql", "select *\nfrom {{ ref('stg_orders') }}\n")
	cfg := Merge(packsOf(t, `
data: { tools: [sqlfluff], example_paths: [models/] }
ml: { enabled: true }
`), nil)
	in := &Input{Root: root, Base: "HEAD", Config: cfg,
		Changed: []ChangedFile{{Path: "models/orders.sql", Status: 'M'}},
		Tools: fakeTools{
			probes: map[string]toolrun.Probe{
				"sqlfluff": {Tool: "sqlfluff", Found: true, Path: "/tools/sqlfluff", Source: "path", Version: "3.2.0"},
				"ruff":     {Tool: "ruff", Found: true, Path: "/tools/ruff", Source: "path", Version: "0.4.1"},
			},
			outs: map[string]toolrun.Output{"sqlfluff": {Stdout: []byte("trailing whitespace\n"), ExitCode: 1, Version: "3.2.0"}},
		},
		Budget: time.Now().Add(time.Minute)}
	in.SetWorkspace("acme", root)
	return in
}

func TestCheckGoldenJSON(t *testing.T) {
	isolate(t)
	rep := RunChecks(context.Background(), goldenInput(t), RunOptions{})
	var buf bytes.Buffer
	if err := EncodeJSON(&buf, rep); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "check-golden.json")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test -run TestCheckGoldenJSON -update)", err)
	}
	if got := strings.ReplaceAll(buf.String(), "\r\n", "\n"); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("tq.packs/v1 output changed (the plugin gate reads it):\n%s\nwant:\n%s", got, want)
	}
	if !rep.Failed() || rep.Summary.RequiredFail != 3 {
		t.Fatalf("summary %+v", rep.Summary)
	}
}

func TestRunStatusesAndRequired(t *testing.T) {
	isolate(t)
	rep := RunChecks(context.Background(), goldenInput(t), RunOptions{})
	got := map[string]Result{}
	for _, r := range rep.Results {
		got[r.ID] = r
	}
	if r := got["data/example-select-star"]; r.Status != Fail || !r.Required || len(r.Findings) != 1 || r.Findings[0].Line != 1 {
		t.Fatalf("built-in error finding fails, required: %+v", r)
	}
	if r := got["data/example-warn"]; r.Status != Fail || r.Findings[0].Level != LevelWarn {
		t.Fatalf("an explicit Fail stays Fail, even with only warn findings (F13): %+v", r)
	}
	if r := got["data/example-sqlfluff"]; r.Status != Fail || !r.Required || r.Tool == nil || r.Tool.Version != "3.2.0" {
		t.Fatalf("declared tool: %+v", r)
	}
	if r := got["ml/example-ruff"]; r.Status != Fail || r.Required || !strings.Contains(r.Detail, "below the minimum 0.5.0") {
		t.Fatalf("undeclared below the minimum is an advisory fail: %+v", r)
	}
	if r := got["ml/example-dvc"]; r.Status != Skip || r.Required {
		t.Fatalf("undeclared and missing is a skip: %+v", r)
	}
	if _, ok := got["data/example-panic"]; ok {
		t.Fatal("a check that does not apply must not run")
	}
}

func TestDeclaredMissingFails(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	in.Tools = fakeTools{}
	rep := RunChecks(context.Background(), in, RunOptions{Packs: []string{"data"}})
	for _, r := range rep.Results {
		if r.ID == "data/example-sqlfluff" {
			if r.Status != Fail || !r.Required || r.Detail != MissingDetail("data") {
				t.Fatalf("%+v", r)
			}
			return
		}
	}
	t.Fatal("no sqlfluff result")
}

func TestBuiltinOnlyAndPackFilter(t *testing.T) {
	isolate(t)
	rep := RunChecks(context.Background(), goldenInput(t), RunOptions{BuiltinOnly: true, Packs: []string{"data"}})
	for _, r := range rep.Results {
		if r.Pack != "data" || r.Tool != nil {
			t.Fatalf("unexpected %+v", r)
		}
	}
	if len(rep.Results) != 2 {
		t.Fatalf("%d results", len(rep.Results))
	}
}

func TestPanicAndTimeoutFailClosed(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	in.Changed = []ChangedFile{{Path: "panic.marker", Status: 'A'}, {Path: "slow.marker", Status: 'A'}}
	in.Budget = time.Now().Add(300 * time.Millisecond)
	start := time.Now()
	rep := RunChecks(context.Background(), in, RunOptions{Packs: []string{"data"}})
	if time.Since(start) > 5*time.Second {
		t.Fatal("the run deadline did not cancel the slow check")
	}
	got := map[string]Result{}
	for _, r := range rep.Results {
		got[r.ID] = r
	}
	if r := got["data/example-panic"]; r.Status != Fail || !r.Required || !strings.Contains(r.Detail, "panicked") {
		t.Fatalf("panic: %+v", r)
	}
	if r := got["data/example-slow"]; r.Status != Fail || !r.Required || r.Detail != "timed out" {
		t.Fatalf("timeout: %+v", r)
	}
}

func TestBaselineDowngradesExternalOnly(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	in.Baseline = matchAll{}
	rep := RunChecks(context.Background(), in, RunOptions{Packs: []string{"data"}})
	for _, r := range rep.Results {
		switch r.ID {
		case "data/example-sqlfluff":
			if r.Status != OK || r.Findings[0].Level != LevelWarn {
				t.Fatalf("baselined external finding: %+v", r)
			}
		case "data/example-select-star":
			if r.Status != Fail {
				t.Fatalf("built-ins ignore the baseline: %+v", r)
			}
		}
	}
}

type matchAll struct{}

func (matchAll) Match(string, Finding) bool { return true }

func TestNoPacksEnabled(t *testing.T) {
	isolate(t)
	in := &Input{Root: t.TempDir(), Config: Merge(nil, nil)}
	start := time.Now()
	rep := RunChecks(context.Background(), in, RunOptions{})
	if len(rep.Results) != 0 || rep.Failed() || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("%+v in %v", rep, time.Since(start))
	}
}

func TestEventsCarryNoFileNames(t *testing.T) {
	isolate(t)
	SetEventWorkspace("acme")
	RunChecks(context.Background(), goldenInput(t), RunOptions{})
	Count("data", "drift:fail", 2)
	Count("data", "bad counter/with path", 1)
	raw, err := os.ReadFile(EventsFile())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, bad := range []string{"orders.sql", "models/", "SELECT", "trailing whitespace", "with path"} {
		if strings.Contains(s, bad) {
			t.Fatalf("events carry %q:\n%s", bad, s)
		}
	}
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var last event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil || last.Counter != "drift:fail" || last.N != 2 || last.Workspace != "acme" {
		t.Fatalf("counter line: %v %+v", err, last)
	}
	var first event
	_ = json.Unmarshal([]byte(lines[0]), &first)
	if first.Check == "" || first.Counts == nil {
		t.Fatalf("result line: %+v", first)
	}
}

func TestBaselineGrewWarnsAndRaisesFlag(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, ".tq/base.json", `{"entries":[{"check":"a"},{"check":"b"}]}`)
	cfg := Merge(packsOf(t, "data: { baseline: .tq/base.json }\n"), nil)
	in := &Input{Root: root, Base: "HEAD", Config: cfg, Changed: []ChangedFile{{Path: ".tq/base.json", Status: 'M'}},
		Git: oldSide{".tq/base.json": `{"entries":[{"check":"a"}]}`}}
	rep := RunChecks(context.Background(), in, RunOptions{})
	var r *Result
	for i := range rep.Results {
		if rep.Results[i].ID == "data/baseline" {
			r = &rep.Results[i]
		}
	}
	if r == nil || r.Status != OK || len(r.Findings) != 1 || r.Findings[0].Rule != "baseline-grew" || r.Findings[0].Level != LevelWarn {
		t.Fatalf("%+v", r)
	}
	if flags, detail := RaiseFlagsDetail(in); !contains(flags, FlagBaselineChange) || detail[FlagBaselineChange] != "data" {
		t.Fatalf("flags %v", flags)
	}
}

type oldSide map[string]string

func (o oldSide) Show(ref, p string) ([]byte, error) {
	if s, ok := o[p]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}
func (o oldSide) ListFiles() ([]string, error) { return nil, nil }

func TestTextOutput(t *testing.T) {
	isolate(t)
	rep := RunChecks(context.Background(), goldenInput(t), RunOptions{})
	txt := rep.Text()
	for _, want := range []string{"fail data/example-select-star: 1 SQL file", "models/orders.sql:1 error SQL001 SELECT * in a model", "fail (advisory) ml/example-ruff", "skip ml/example-dvc"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("missing %q in:\n%s", want, txt)
		}
	}
}
