package data

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite the want.json golden files")

// defaultGoldenConfig is the pack config of a golden case without config.yaml.
const defaultGoldenConfig = `
data:
  enabled: true
  dbt:
    contract_paths: ["models/marts/**"]
`

// goldenWant is the hand-written expectation of each case: the status, then
// rule@file:line for each finding (sorted). It is the spec; want.json holds
// the full output (detail, messages) for review.
var goldenWant = map[string]string{
	"dbt-new-model-tests/ok-keys":                    "ok",
	"dbt-new-model-tests/no-yaml":                    "fail missing-yaml@models/staging/stg_orders.sql:1",
	"dbt-new-model-tests/missing-not-null":           "fail missing-description@models/staging/_staging.yml:3 missing-key-tests@models/staging/_staging.yml:5",
	"dbt-new-model-tests/pk-constraint-enforced":     "ok",
	"dbt-new-model-tests/pk-constraint-not-enforced": "fail missing-key-tests@models/marts/_marts.yml:6",
	"dbt-new-model-tests/composite-utils":            "ok",
	"dbt-new-model-tests/no-primary-key":             "fail missing-primary-key@models/staging/_staging.yml:3",
	"dbt-new-model-tests/model-id-column":            "ok",
	"dbt-new-model-tests/modified-not-new":           "ok",
	"dbt-new-model-tests/suppressed":                 "ok",
	"dbt-new-model-tests/contract-pk":                "ok",
	"dbt-new-model-tests/custom-required-tests":      "ok",

	"dbt-contract-enforced/enforced-yaml":          "ok",
	"dbt-contract-enforced/not-enforced":           "fail contract-not-enforced@models/marts/_marts.yml:3",
	"dbt-contract-enforced/folder-config":          "ok",
	"dbt-contract-enforced/sql-config":             "ok",
	"dbt-contract-enforced/missing-data-type":      "fail column-no-data-type@models/marts/_marts.yml:9",
	"dbt-contract-enforced/no-columns":             "fail enforced-no-columns@models/marts/fct_orders.sql:1",
	"dbt-contract-enforced/sql-overrides-folder":   "fail contract-not-enforced@models/marts/_marts.yml:3",
	"dbt-contract-enforced/outside-contract-paths": "ok",
	"dbt-contract-enforced/project-subdir":         "fail contract-not-enforced@analytics/models/marts/fct_orders.sql:1",
	"dbt-contract-enforced/project-file-changed":   "fail contract-not-enforced@models/marts/_marts.yml:3",

	"dbt-incremental/no-unique-key":      "fail incremental-no-unique-key@models/marts/fct_events.sql:1",
	"dbt-incremental/append-strategy":    "ok",
	"dbt-incremental/no-lookback":        "ok incremental-no-lookback@models/marts/fct_events.sql:4",
	"dbt-incremental/lookback-macro":     "ok",
	"dbt-incremental/yaml-config":        "ok",
	"dbt-incremental/folder-incremental": "fail incremental-no-unique-key@models/events/fct_clicks.sql:1",
	"dbt-incremental/table-model":        "ok",

	"sql-select-star/mart-star":        "fail select-star@models/marts/fct_orders.sql:1",
	"sql-select-star/staging-source":   "ok",
	"sql-select-star/staging-ref-star": "ok select-star@models/staging/stg_orders.sql:1",
	"sql-select-star/other-star":       "ok select-star@models/intermediate/int_orders.sql:4",
	"sql-select-star/count-star":       "ok",
	"sql-select-star/suppressed":       "ok",
	"sql-select-star/not-a-model":      "ok",

	"sql-hardcoded-relation/schema-table": "fail hardcoded-relation@models/marts/fct_orders.sql:2",
	"sql-hardcoded-relation/three-part":   "fail hardcoded-relation@models/marts/fct_orders.sql:2",
	"sql-hardcoded-relation/ref-and-ctes": "ok",
	"sql-hardcoded-relation/suppressed":   "ok",
}

type goldenResult struct {
	Status   packs.Status    `json:"status"`
	Detail   string          `json:"detail"`
	Findings []packs.Finding `json:"findings"`
}

func packsOf(t testing.TB, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func checkByID(t testing.TB, id string) packs.Check {
	t.Helper()
	for _, c := range packs.Checks("data") {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %s is not registered", id)
	return packs.Check{}
}

// readChanged reads changed.txt: one "<status> <path>" per line.
func readChanged(t testing.TB, file string) []packs.ChangedFile {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out []packs.ChangedFile
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		st, p, _ := strings.Cut(l, " ")
		out = append(out, packs.ChangedFile{Path: p, Status: st[0]})
	}
	return out
}

// runCase runs one built-in check the way the framework does (Applies, Run,
// then "a check fails only on an error finding").
func runCase(t *testing.T, c packs.Check, in *packs.Input) goldenResult {
	t.Helper()
	in = in.ForPack("data")
	if c.Applies != nil && !c.Applies(in) {
		return goldenResult{Status: "n/a", Findings: []packs.Finding{}}
	}
	r := c.Run(context.Background(), in)
	if r.Findings == nil {
		r.Findings = []packs.Finding{}
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Rule < b.Rule
	})
	status := r.Status
	if len(r.Findings) > 0 && status != packs.Skip {
		status = packs.OK
		for _, f := range r.Findings {
			if f.Level == packs.LevelError {
				status = packs.Fail
			}
		}
	}
	return goldenResult{Status: status, Detail: r.Detail, Findings: r.Findings}
}

func summary(g goldenResult) string {
	parts := []string{string(g.Status)}
	for _, f := range g.Findings {
		parts = append(parts, fmt.Sprintf("%s@%s:%d", f.Rule, f.File, f.Line))
	}
	return strings.Join(parts, " ")
}

func TestGolden(t *testing.T) {
	isolate(t)
	dirs, err := filepath.Glob(filepath.Join("testdata", "*", "*", "changed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range dirs {
		caseDir := filepath.Dir(f)
		check := filepath.Base(filepath.Dir(caseDir))
		name := check + "/" + filepath.Base(caseDir)
		if !strings.HasPrefix(check, "dbt-") && !strings.HasPrefix(check, "sql-") {
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			cfgBody := defaultGoldenConfig
			if b, err := os.ReadFile(filepath.Join(caseDir, "config.yaml")); err == nil {
				cfgBody = string(b)
			}
			root, _ := filepath.Abs(filepath.Join(caseDir, "repo"))
			in := &packs.Input{Root: root, Base: "HEAD", Changed: readChanged(t, f), Config: packs.Merge(packsOf(t, cfgBody), nil)}
			got := runCase(t, checkByID(t, "data/"+check), in)
			want, ok := goldenWant[name]
			if !ok {
				t.Fatalf("no expectation for %s; got %q", name, summary(got))
			}
			if s := summary(got); s != want {
				t.Errorf("got  %q\nwant %q\nfindings: %+v", s, want, got.Findings)
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			_ = enc.Encode(got)
			wantFile := filepath.Join(caseDir, "want.json")
			if *update {
				if err := os.WriteFile(wantFile, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := os.ReadFile(wantFile)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/packs/data -run TestGolden -update)", err)
			}
			if strings.ReplaceAll(string(raw), "\r\n", "\n") != buf.String() {
				t.Errorf("want.json differs:\n%s\nwant:\n%s", buf.String(), raw)
			}
			for _, fd := range got.Findings {
				if strings.Contains(fd.Message, "tester@") {
					t.Errorf("a finding carries a value: %+v", fd)
				}
			}
		})
	}
	for name := range goldenWant {
		if !seen[name] {
			t.Errorf("expectation %s has no fixture", name)
		}
	}
}

func isolate(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}
