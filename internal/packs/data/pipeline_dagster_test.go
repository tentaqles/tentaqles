package data

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

var (
	fakeOnce sync.Once
	fakeDir  string
	fakeErr  error
)

// fakeDagster builds testdata/pipeline-fakecli/dagster once and returns
// the folder holding it.
func fakeDagster(t *testing.T) string {
	t.Helper()
	fakeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tq-fake-dagster-")
		if err != nil {
			fakeErr = err
			return
		}
		name := "dagster"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./testdata/pipeline-fakecli/dagster")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeErr = errors.New(string(out))
			return
		}
		fakeDir = dir
	})
	if fakeErr != nil {
		t.Fatalf("building the fake dagster: %v", fakeErr)
	}
	return fakeDir
}

func dagsterInput(t *testing.T, packBody string, files map[string]string) *packs.Input {
	t.Helper()
	root := t.TempDir()
	in := &packs.Input{Root: root, Budget: time.Now().Add(time.Minute), Git: fixtureGit{}}
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		in.Changed = append(in.Changed, packs.ChangedFile{Path: p, Status: 'M'})
	}
	in.Config = packs.Merge(map[string]manifest.PackConfig{"data": mustPack(t, packBody)}, nil)
	in.Tools = packs.NewToolRunner(root, in.Config)
	in.SetWorkspace("acme", root)
	return in
}

func dagsterResult(t *testing.T, in *packs.Input) packs.Result {
	t.Helper()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}})
	for _, r := range rep.Results {
		if r.ID == "data/dagster-validate" {
			return r
		}
	}
	return packs.Result{Status: "not-run"}
}

const dagsterDefs = "import dagster\n\n@dagster.asset\ndef orders():\n    return 1\n"

func TestDagsterValidate(t *testing.T) {
	isolatePipeline(t)
	dir := fakeDagster(t)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKE_DAGSTER_ARGS", argsFile)

	t.Run("declared and passing", func(t *testing.T) {
		t.Setenv("FAKE_DAGSTER_EXIT", "0")
		r := dagsterResult(t, dagsterInput(t, "tools: [dagster]\n", map[string]string{"pipelines/defs.py": dagsterDefs}))
		if r.Status != packs.OK || !r.Required || r.Tool == nil || r.Tool.Name != "dagster" || r.Tool.Version != "1.9.3" {
			t.Fatalf("%+v %+v", r, r.Tool)
		}
		if b, _ := os.ReadFile(argsFile); string(b) != "definitions validate" {
			t.Fatalf("args %q", b)
		}
	})
	t.Run("failing, output redacted", func(t *testing.T) {
		t.Setenv("FAKE_DAGSTER_EXIT", "1")
		t.Setenv("FAKE_DAGSTER_OUT", "Error loading location acme: owner tester@acme.test\nInvalid definitions\n")
		r := dagsterResult(t, dagsterInput(t, "tools: [dagster]\n", map[string]string{"pipelines/defs.py": dagsterDefs}))
		if r.Status != packs.Fail || !r.Required || r.Tool.ExitCode != 1 {
			t.Fatalf("%+v", r)
		}
		if !strings.Contains(r.Detail, "Invalid definitions") || strings.Contains(r.Detail, "tester@acme.test") {
			t.Fatalf("detail %q", r.Detail)
		}
	})
	t.Run("undeclared is advisory", func(t *testing.T) {
		t.Setenv("FAKE_DAGSTER_EXIT", "1")
		r := dagsterResult(t, dagsterInput(t, "enabled: true\n", map[string]string{"pipelines/defs.py": dagsterDefs}))
		if r.Status != packs.Fail || r.Required {
			t.Fatalf("advisory fail: %+v", r)
		}
	})
	t.Run("no dagster change", func(t *testing.T) {
		r := dagsterResult(t, dagsterInput(t, "tools: [dagster]\n", map[string]string{"pipelines/clean.py": "import pandas\n"}))
		if r.Status != packs.Skip {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("workspace.yaml change", func(t *testing.T) {
		t.Setenv("FAKE_DAGSTER_EXIT", "0")
		r := dagsterResult(t, dagsterInput(t, "tools: [dagster]\n", map[string]string{"workspace.yaml": "load_from:\n  - python_module: acme_defs\n"}))
		if r.Status != packs.OK {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		t.Setenv("FAKE_DAGSTER_SLEEP", "10")
		start := time.Now()
		r := dagsterResult(t, dagsterInput(t, "tools: [dagster]\ntimeouts: { dagster: 1s }\n", map[string]string{"pipelines/defs.py": dagsterDefs}))
		if r.Status != packs.Fail || r.Tool == nil || !r.Tool.TimedOut {
			t.Fatalf("%+v %+v", r, r.Tool)
		}
		if time.Since(start) > 9*time.Second {
			t.Fatalf("the timeout did not stop the tool (%v)", time.Since(start))
		}
	})
}

func TestDagsterDeclaredMissing(t *testing.T) {
	isolatePipeline(t)
	in := dagsterInput(t, "tools: [dagster]\n", map[string]string{"pipelines/defs.py": dagsterDefs})
	r := packs.NewToolRunner(in.Root, in.Config)
	r.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	in.Tools = r
	res := dagsterResult(t, in)
	if res.Status != packs.Fail || !res.Required || res.Detail != packs.MissingDetail("data") {
		t.Fatalf("declared but missing: %+v", res)
	}
	in = dagsterInput(t, "enabled: true\n", map[string]string{"pipelines/defs.py": dagsterDefs})
	r = packs.NewToolRunner(in.Root, in.Config)
	r.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	in.Tools = r
	if res := dagsterResult(t, in); res.Status != packs.Skip || res.Required {
		t.Fatalf("undeclared and missing: %+v", res)
	}
}

// specSpy is a ToolRunner that records the Spec it is asked to run.
type specSpy struct{ got toolrun.Spec }

func (s *specSpy) Probe(_, tool string) toolrun.Probe {
	return toolrun.Probe{Tool: tool, Found: true, Path: tool, Version: "1.9.3"}
}

func (s *specSpy) Run(_ context.Context, spec toolrun.Spec) (toolrun.Output, error) {
	s.got = spec
	return toolrun.Output{}, nil
}

// The check passes its own 120 s default to the runner, rather than
// relying on toolrun.DefaultTimeout happening to match (review r0 m2).
func TestDagsterValidatePassesTimeout(t *testing.T) {
	isolatePipeline(t)
	in := dagsterInput(t, "tools: [dagster]\n", map[string]string{"pipelines/defs.py": dagsterDefs})
	spy := &specSpy{}
	in.Tools = spy
	if r := runDagsterValidate(context.Background(), in); r.Status != packs.OK {
		t.Fatalf("%+v", r)
	}
	if spy.got.Timeout != 120*time.Second || spy.got.Tool != "dagster" {
		t.Fatalf("spec %+v", spy.got)
	}
}
