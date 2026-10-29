package data

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite the want.json golden files")

// isolatePipeline points TQ_HOME, HOME and USERPROFILE at a temp dir.
func isolatePipeline(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// fixtureGit is a GitReader over the fixture tree: every file is tracked and
// there is no old side.
type fixtureGit struct{ files []string }

func (g fixtureGit) Show(ref, p string) ([]byte, error) { return nil, os.ErrNotExist }
func (g fixtureGit) ListFiles() ([]string, error)       { return g.files, nil }

// caseConfig is a case's config.yaml: the packs block and the manifest data
// block.
type caseConfig struct {
	Packs map[string]manifest.PackConfig `yaml:"packs"`
	Data  manifest.Data                  `yaml:"data"`
}

// golden is what a want.json holds: one check's result.
type golden struct {
	Status   packs.Status    `json:"status"`
	Detail   string          `json:"detail"`
	Findings []packs.Finding `json:"findings"`
}

// runCase runs every enabled data check over testdata/<dir>/<case>/new as
// the changed set and returns the result of check id.
func runCase(t *testing.T, caseDir, id string) packs.Result {
	t.Helper()
	root := filepath.Join(caseDir, "new")
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	cc := caseConfig{}
	if b, err := os.ReadFile(filepath.Join(caseDir, "config.yaml")); err == nil {
		if err := yaml.Unmarshal(b, &cc); err != nil {
			t.Fatal(err)
		}
	}
	if cc.Packs == nil {
		cc.Packs = map[string]manifest.PackConfig{}
	}
	if _, ok := cc.Packs["data"]; !ok {
		var pc manifest.PackConfig
		if err := yaml.Unmarshal([]byte("enabled: true\n"), &pc); err != nil {
			t.Fatal(err)
		}
		cc.Packs["data"] = pc
	}
	in := &packs.Input{Root: root, Config: packs.Merge(cc.Packs, nil), Manifest: &manifest.Manifest{Data: cc.Data},
		Git: fixtureGit{files: files}, Budget: time.Now().Add(time.Minute)}
	for _, f := range files {
		in.Changed = append(in.Changed, packs.ChangedFile{Path: f, Status: 'M'})
	}
	in.SetWorkspace("acme", root)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}, BuiltinOnly: true})
	for _, r := range rep.Results {
		if r.ID == id {
			return r
		}
	}
	return packs.Result{ID: id, Status: "not-run", Findings: []packs.Finding{}}
}

// TestPipelineGoldens runs each case under testdata/pipeline-*/ and compares
// the check's status, detail and findings with want.json (-update rewrites).
func TestPipelineGoldens(t *testing.T) {
	isolatePipeline(t)
	dirs, err := filepath.Glob(filepath.Join("testdata", "pipeline-*"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, dir := range dirs {
		id := checkForDir(filepath.Base(dir))
		if id == "" {
			continue
		}
		cases, _ := os.ReadDir(dir)
		for _, c := range cases {
			if !c.IsDir() {
				continue
			}
			caseDir := filepath.Join(dir, c.Name())
			if _, err := os.Stat(filepath.Join(caseDir, "new")); err != nil {
				continue
			}
			n++
			t.Run(filepath.Base(dir)+"/"+c.Name(), func(t *testing.T) {
				r := runCase(t, caseDir, id)
				got := golden{Status: r.Status, Detail: r.Detail, Findings: r.Findings}
				b, _ := json.MarshalIndent(got, "", "  ")
				b = append(b, '\n')
				want := filepath.Join(caseDir, "want.json")
				if *update {
					if err := os.WriteFile(want, b, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				w, err := os.ReadFile(want)
				if err != nil {
					t.Fatalf("%v (run with -update)", err)
				}
				if !bytes.Equal(bytes.ReplaceAll(w, []byte("\r\n"), []byte("\n")), b) {
					t.Fatalf("%s:\n%s\nwant:\n%s", id, b, w)
				}
				for _, f := range r.Findings {
					if f.File == "" || f.Rule == "" || f.Message == "" || (f.Level != packs.LevelError && f.Level != packs.LevelWarn) {
						t.Errorf("malformed finding %+v", f)
					}
				}
			})
		}
	}
	if n < 20 {
		t.Fatalf("only %d golden cases found", n)
	}
}

// checkForDir maps testdata/pipeline-<x> to its check id.
func checkForDir(dir string) string {
	for _, c := range packs.Checks("data") {
		if c.ID == "data/"+dir || c.ID == "data/"+strings.TrimPrefix(dir, "pipeline-") {
			return c.ID
		}
	}
	return ""
}

// TestPipelineFindingsCarryNoValues: findings never quote file contents
// (the PII exit rule): no synthetic value from the fixtures appears.
func TestPipelineFindingsCarryNoValues(t *testing.T) {
	isolatePipeline(t)
	for _, c := range []struct{ dir, id string }{
		{"testdata/pipeline-notebook-outputs/bad", "data/notebook-outputs"},
		{"testdata/pipeline-synapse-artifacts/bad", "data/synapse-artifacts"},
		{"testdata/pipeline-pii-output/bad", "data/pipeline-pii-output"},
	} {
		r := runCase(t, c.dir, c.id)
		if len(r.Findings) == 0 {
			t.Fatalf("%s: no findings", c.id)
		}
		b, _ := json.Marshal(r)
		for _, v := range []string{"tester@acme.test", "notreal", "c2VjcmV0a2V5", "acme-sql.database"} {
			if strings.Contains(string(b), v) {
				t.Fatalf("%s: a finding carries a fixture value %q: %s", c.id, v, b)
			}
		}
	}
}

func TestPipelineNoFilesIsQuiet(t *testing.T) {
	isolatePipeline(t)
	root := t.TempDir()
	in := &packs.Input{Root: root, Config: packs.Merge(map[string]manifest.PackConfig{"data": mustPack(t, "enabled: true\n")}, nil),
		Git: fixtureGit{}, Budget: time.Now().Add(time.Minute),
		Changed: []packs.ChangedFile{{Path: "README.md", Status: 'M'}}}
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}})
	for _, r := range rep.Results {
		if strings.HasPrefix(r.ID, "data/") {
			t.Fatalf("a pipeline check ran on a README change: %+v", r)
		}
	}
}

func mustPack(t testing.TB, body string) manifest.PackConfig {
	t.Helper()
	var pc manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &pc); err != nil {
		t.Fatal(err)
	}
	return pc
}

func writeFixture(t testing.TB, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPipelineChecks2000Files: the built-in pipeline checks finish well
// within the spec's 5 s on a 2,000-file change (spec C §1.4).
func TestPipelineChecks2000Files(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	isolatePipeline(t)
	root := t.TempDir()
	body := "from airflow import DAG\nimport pendulum\n\nwith DAG(dag_id=\"d\", start_date=pendulum.datetime(2026, 1, 1), catchup=False, default_args={\"retries\": 1}) as dag:\n    pass\n\n" +
		strings.Repeat("df = spark.table(\"raw.orders\").filter(F.col(\"d\") == F.lit(ds))\n", 40)
	in := &packs.Input{Root: root, Config: packs.Merge(map[string]manifest.PackConfig{"data": mustPack(t, "enabled: true\n")}, nil),
		Manifest: &manifest.Manifest{}, Budget: time.Now().Add(time.Minute)}
	var files []string
	for i := 0; i < 2000; i++ {
		p := "dags/d" + strings.Repeat("x", i%7) + "_" + itoa(i) + ".py"
		writeFixture(t, root, p, body)
		files = append(files, p)
		in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: 'M'})
	}
	in.Git = fixtureGit{files: files}
	start := time.Now()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}, BuiltinOnly: true})
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("pipeline checks took %v on 2,000 files", d)
	}
	for _, r := range rep.Results {
		if r.Status == packs.Fail {
			t.Fatalf("%s failed on clean files: %+v", r.ID, r)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
