package ml

// Golden harness for the M1 lints. A case is a directory
// testdata/<check>/<case>/ with:
//
//	new/…        the new side; every file in it is a changed path
//	old/…        the old side at Base "HEAD" (optional; without it there is no old side)
//	config.yaml  the packs map (optional; default `ml: {enabled: true}`)
//	extra.txt    unchanged new-side files, one repo path per line, that live under new/ (optional)
//	want.json    the expected {status, detail, findings} of the check
//
// Run `go test ./internal/packs/ml -run Golden -update` to rewrite want.json.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite the golden want.json files")

func TestMain(m *testing.M) {
	if mode := os.Getenv("TQ_FAKE_TOOL"); mode != "" {
		fakeTool(mode)
		return
	}
	os.Exit(m.Run())
}

func isolate(t testing.TB) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// dirGit is a GitReader over an old/ directory and a file list.
type dirGit struct {
	old   string
	files []string
}

func (g dirGit) Show(ref, p string) ([]byte, error) {
	if g.old == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(g.old, filepath.FromSlash(p)))
}

func (g dirGit) ListFiles() ([]string, error) { return g.files, nil }

func walk(t testing.TB, root string) []string {
	t.Helper()
	var out []string
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			r, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(r))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func loadConfig(t testing.TB, dir string) packs.Config {
	t.Helper()
	body := []byte("ml: {enabled: true}\n")
	if b, err := os.ReadFile(filepath.Join(dir, "config.yaml")); err == nil {
		body = b
	}
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidatePacks(m); err != nil {
		t.Fatalf("config.yaml: %v", err)
	}
	return packs.Merge(m, nil)
}

// caseInput builds the Input of a golden case.
func caseInput(t testing.TB, dir string) *packs.Input {
	t.Helper()
	newDir := filepath.Join(dir, "new")
	oldDir := filepath.Join(dir, "old")
	extra := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dir, "extra.txt")); err == nil {
		for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				extra[l] = true
			}
		}
	}
	oldFiles := map[string]bool{}
	for _, p := range walk(t, oldDir) {
		oldFiles[p] = true
	}
	all := walk(t, newDir)
	in := &packs.Input{Root: newDir, Config: loadConfig(t, dir)}
	g := dirGit{files: all}
	if len(oldFiles) > 0 {
		in.Base = "HEAD"
		g.old = oldDir
	}
	in.Git = g
	for _, p := range all {
		if extra[p] {
			continue
		}
		st := byte('A')
		if oldFiles[p] {
			st = 'M'
		}
		in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: st})
	}
	return in
}

type golden struct {
	Status   packs.Status    `json:"status"`
	Detail   string          `json:"detail"`
	Findings []packs.Finding `json:"findings"`
}

// runCase runs one check over a case and returns its result ("" status when
// the check did not apply).
func runCase(t testing.TB, id string, in *packs.Input) packs.Result {
	t.Helper()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"ml"}, BuiltinOnly: true})
	for _, r := range rep.Results {
		if r.ID == id {
			return r
		}
	}
	return packs.Result{ID: id, Findings: []packs.Finding{}}
}

// goldenDirs maps a testdata directory to the check it exercises.
var goldenDirs = map[string]string{
	"leakage":           "ml/leakage",
	"repro-seeds":       "ml/seeds",
	"repro-envlock":     "ml/env-lock",
	"repro-snapshot":    "ml/data-snapshot",
	"repro-tracking":    "ml/run-tracking",
	"repro-deserialize": "ml/unsafe-deserialize",
	"repro-notebook":    "ml/notebook-outputs",
}

func TestGolden(t *testing.T) {
	isolate(t)
	names := make([]string, 0, len(goldenDirs))
	for d := range goldenDirs {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, d := range names {
		id := goldenDirs[d]
		cases, err := os.ReadDir(filepath.Join("testdata", d))
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		for _, c := range cases {
			if !c.IsDir() {
				continue
			}
			dir := filepath.Join("testdata", d, c.Name())
			t.Run(d+"/"+c.Name(), func(t *testing.T) {
				r := runCase(t, id, caseInput(t, dir))
				got := golden{Status: r.Status, Detail: r.Detail, Findings: r.Findings}
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetIndent("", "  ")
				enc.SetEscapeHTML(false)
				if err := enc.Encode(got); err != nil {
					t.Fatal(err)
				}
				wantPath := filepath.Join(dir, "want.json")
				if *update {
					if err := os.WriteFile(wantPath, buf.Bytes(), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(wantPath)
				if err != nil {
					t.Fatalf("no want.json (run with -update): %v", err)
				}
				if norm(want) != norm(buf.Bytes()) {
					t.Errorf("result differs from %s\n--- got\n%s\n--- want\n%s", wantPath, buf.String(), want)
				}
				assertCaseIntent(t, c.Name(), got)
			})
		}
	}
}

func norm(b []byte) string { return strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")) }

// assertCaseIntent keeps -update honest: a case named *-pos must have a
// finding, and one named *-neg must have none of the rule it names.
// A case name may carry a variant after "__" (L1__notebook-pos).
func assertCaseIntent(t *testing.T, name string, g golden) {
	t.Helper()
	ruleOf := func(suffix string) string {
		r := strings.TrimSuffix(name, suffix)
		if i := strings.Index(r, "__"); i >= 0 {
			r = r[:i]
		}
		return r
	}
	switch {
	case strings.HasSuffix(name, "-pos"):
		if len(g.Findings) == 0 {
			t.Errorf("positive case %s has no finding", name)
		}
		rule := ruleOf("-pos")
		for _, f := range g.Findings {
			if strings.HasPrefix(strings.ToLower(f.Rule), strings.ToLower(rule)) {
				return
			}
		}
		t.Errorf("positive case %s has no finding of rule %s*", name, rule)
	case strings.HasSuffix(name, "-neg"):
		rule := ruleOf("-neg")
		for _, f := range g.Findings {
			if strings.HasPrefix(strings.ToLower(f.Rule), strings.ToLower(rule)) {
				t.Errorf("negative case %s has finding %s", name, f)
			}
		}
	}
}

// --- fake tools ---------------------------------------------------------------

// fakeTool is the test binary run as ruff, uv, poetry or dvc: it prints
// TQ_FAKE_OUT (a file) and exits with TQ_FAKE_EXIT, and records its argv
// in TQ_FAKE_ARGS when set.
func fakeTool(mode string) {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		os.Stdout.WriteString(mode + " " + os.Getenv("TQ_FAKE_VERSION") + "\n")
		os.Exit(0)
	}
	if f := os.Getenv("TQ_FAKE_ARGS"); f != "" {
		wd, _ := os.Getwd()
		_ = os.WriteFile(f, []byte(strings.Join(args, " ")+"\n"+filepath.ToSlash(wd)), 0o600)
	}
	if f := os.Getenv("TQ_FAKE_OUT"); f != "" {
		b, _ := os.ReadFile(f)
		os.Stdout.Write(b)
	}
	code := 0
	if e := os.Getenv("TQ_FAKE_EXIT"); e != "" {
		for _, c := range e {
			code = code*10 + int(c-'0')
		}
	}
	os.Exit(code)
}

// fakeBin copies the test binary to dir/name(.exe).
func fakeBin(t *testing.T, dir, name string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
