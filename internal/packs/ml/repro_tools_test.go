package ml

// External checks against fake tools on PATH: the test binary copied as
// ruff, uv, poetry and dvc (an init in leak_harness_test.go dispatches on TQ_M1_FAKE_TOOL).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// toolEnv puts fake tools first on PATH and returns a runner input over a
// temp repo holding files (all changed).
func toolEnv(t *testing.T, cfg string, files map[string]string, tools ...string) *packs.Input {
	t.Helper()
	lintIsolate(t)
	bin := t.TempDir()
	for _, tool := range tools {
		lintFakeBin(t, bin, tool)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_M1_FAKE_VERSION", "1.0.0")
	in := flagInput(t, cfg, files)
	in.Tools = packs.NewToolRunner(in.Root, in.Config)
	return in
}

func fakeOut(t *testing.T, mode, out, exit string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(f, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("TQ_M1_FAKE_TOOL", mode)
	t.Setenv("TQ_M1_FAKE_OUT", f)
	t.Setenv("TQ_M1_FAKE_EXIT", exit)
	t.Setenv("TQ_M1_FAKE_ARGS", args)
	return args
}

func lintResult(t *testing.T, in *packs.Input, id string) packs.Result {
	t.Helper()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"ml"}})
	for _, r := range rep.Results {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no result for %s in %+v", id, rep.Results)
	return packs.Result{}
}

func readArgs(t *testing.T, p string) (string, string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the fake tool did not run: %v", err)
	}
	parts := strings.SplitN(string(b), "\n", 2)
	return parts[0], parts[1]
}

func TestRuffFindings(t *testing.T) {
	in := toolEnv(t, "ml: {tools: [ruff]}", map[string]string{"src/train.py": "import numpy as np\nnp.random.rand(3)\n"}, "ruff")
	abs := filepath.Join(in.Root, "src", "train.py")
	out := `[{"cell": null, "code": "NPY002", "filename": ` + jsonStr(abs) + `, "location": {"column": 1, "row": 2}, "message": "Replace legacy ` + "`np.random.rand`" + ` call with ` + "`np.random.Generator`" + `"},
 {"cell": 3, "code": "PD011", "filename": "notebooks/eda.ipynb", "location": {"column": 1, "row": 1}, "message": "Use ` + "`.to_numpy()`" + ` instead of ` + "`.values`" + `"}]`
	args := fakeOut(t, "ruff", out, "0")
	r := lintResult(t, in, "ml/ruff")
	if r.Status != packs.Fail || !r.Required || len(r.Findings) != 2 {
		t.Fatalf("result = %+v", r)
	}
	if f := r.Findings[0]; f.File != "src/train.py" || f.Line != 2 || f.Rule != "NPY002" || !strings.Contains(f.Message, "ml/seeds") {
		t.Errorf("finding 0 = %+v", f)
	}
	if f := r.Findings[1]; !strings.HasPrefix(f.Message, "cell 3, line 1: ") {
		t.Errorf("finding 1 = %+v", f)
	}
	argv, _ := readArgs(t, args)
	if argv != "check --no-cache --exit-zero --select NPY,PD --output-format json -- src/train.py" {
		t.Errorf("argv = %q", argv)
	}
}

// A changed file whose name starts with "-" reaches ruff as a path, after
// "--", never as an option.
func TestRuffDashFileIsAPath(t *testing.T) {
	in := toolEnv(t, "ml: {tools: [ruff]}", map[string]string{"-x.py": "x = 1\n", "a.py": "y = 2\n"}, "ruff")
	args := fakeOut(t, "ruff", "[]", "0")
	if r := lintResult(t, in, "ml/ruff"); r.Status != packs.OK {
		t.Fatalf("result = %+v", r)
	}
	argv, _ := readArgs(t, args)
	if !strings.HasSuffix(argv, " -- -x.py a.py") {
		t.Errorf("argv = %q, want the paths after --", argv)
	}
}

// noTools makes the runner find no tool at all, whatever is installed on the
// machine, without touching PATH.
func noTools(in *packs.Input) {
	r := packs.NewToolRunner(in.Root, in.Config)
	r.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	in.Tools = r
}

// A tool that outlives its timeout fails the check with "timed out" and a
// ToolRun that says so, for each external check.
func TestToolTimeoutFails(t *testing.T) {
	cases := []struct {
		tool, id string
		files    map[string]string
	}{
		{"ruff", "ml/ruff", map[string]string{"a.py": "x = 1\n"}},
		{"uv", "ml/lock-check", map[string]string{"pyproject.toml": "[project]\nname = \"acme\"\n", "uv.lock": "version = 1\n"}},
		{"poetry", "ml/lock-check-poetry", map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"acme\"\n", "poetry.lock": "# lock\n"}},
		{"dvc", "ml/dvc-status", map[string]string{"dvc.yaml": "stages: {}\n"}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			in := toolEnv(t, "ml: {tools: ["+c.tool+"], timeouts: {"+c.tool+": 1s}}", c.files, c.tool)
			fakeOut(t, c.tool, "", "0")
			t.Setenv("TQ_M1_FAKE_SLEEP", "1")
			start := time.Now()
			r := lintResult(t, in, c.id)
			if r.Status != packs.Fail || r.Detail != c.tool+" timed out" || r.Tool == nil || !r.Tool.TimedOut {
				t.Fatalf("result = %+v", r)
			}
			if d := time.Since(start); d > 20*time.Second {
				t.Errorf("the check took %s; the timeout did not stop the tool", d)
			}
		})
	}
}

func jsonStr(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestRuffBadOutputFailsWithTail(t *testing.T) {
	in := toolEnv(t, "ml: {tools: [ruff]}", map[string]string{"a.py": "x = 1\n"}, "ruff")
	fakeOut(t, "ruff", "error: unexpected config for tester@acme.test\n", "2")
	r := lintResult(t, in, "ml/ruff")
	if r.Status != packs.Fail || !strings.Contains(r.Detail, "ruff exited 2") {
		t.Fatalf("result = %+v", r)
	}
	if strings.Contains(r.Detail, "tester@acme.test") {
		t.Errorf("the tail is not redacted: %q", r.Detail)
	}
}

func TestRuffUndeclaredMissingSkips(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {enabled: true}", map[string]string{"a.py": "x = 1\n"})
	in.Tools = packs.NewToolRunner(in.Root, in.Config)
	noTools(in)
	r := lintResult(t, in, "ml/ruff")
	if r.Status != packs.Skip || r.Required {
		t.Fatalf("result = %+v", r)
	}
}

func TestUVLockCheck(t *testing.T) {
	files := map[string]string{"pyproject.toml": "[project]\nname = \"acme\"\n", "uv.lock": "version = 1\n"}
	in := toolEnv(t, "ml: {tools: [uv]}", files, "uv")
	args := fakeOut(t, "uv", "", "0")
	if r := lintResult(t, in, "ml/lock-check"); r.Status != packs.OK || len(r.Findings) != 0 {
		t.Fatalf("clean lock: %+v", r)
	}
	argv, wd := readArgs(t, args)
	if argv != "lock --check" {
		t.Errorf("argv = %q", argv)
	}
	if !strings.EqualFold(filepath.Clean(filepath.FromSlash(wd)), filepath.Clean(in.Root)) {
		rr, _ := filepath.EvalSymlinks(in.Root)
		if !strings.EqualFold(filepath.Clean(filepath.FromSlash(wd)), filepath.Clean(rr)) {
			t.Errorf("cwd = %q, want %q", wd, in.Root)
		}
	}
	fakeOut(t, "uv", "error: The lockfile at `uv.lock` needs to be updated\n", "1")
	r := lintResult(t, in, "ml/lock-check")
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "uv.lock" || r.Findings[0].Rule != "lock-outdated" {
		t.Fatalf("stale lock: %+v", r)
	}
}

func TestPoetryLockCheck(t *testing.T) {
	files := map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"acme\"\n", "poetry.lock": "# lock\n"}
	in := toolEnv(t, "ml: {tools: [poetry]}", files, "poetry")
	args := fakeOut(t, "poetry", "pyproject.toml changed significantly since poetry.lock was last generated.\n", "1")
	r := lintResult(t, in, "ml/lock-check-poetry")
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "poetry.lock" {
		t.Fatalf("result = %+v", r)
	}
	if argv, _ := readArgs(t, args); argv != "check --lock" {
		t.Errorf("argv = %q", argv)
	}
}

func TestLockCheckSkipsWithoutLock(t *testing.T) {
	in := toolEnv(t, "ml: {tools: [uv]}", map[string]string{"pyproject.toml": "[project]\nname = \"acme\"\n"}, "uv")
	fakeOut(t, "uv", "", "0")
	if r := lintResult(t, in, "ml/lock-check"); r.Status != packs.Skip {
		t.Fatalf("result = %+v", r)
	}
}

func TestDVCStatus(t *testing.T) {
	files := map[string]string{"dvc.yaml": "stages: {}\n", "data/raw.csv.dvc": "outs: []\n"}
	in := toolEnv(t, "ml: {tools: [dvc]}", files, "dvc")
	args := fakeOut(t, "dvc", "{}\n", "0")
	if r := lintResult(t, in, "ml/dvc-status"); r.Status != packs.OK || len(r.Findings) != 0 || !strings.Contains(r.Detail, "up to date") {
		t.Fatalf("clean: %+v", r)
	}
	if argv, _ := readArgs(t, args); argv != "status --json" {
		t.Errorf("argv = %q (must stay local: no -c)", argv)
	}
	out := `{"train": [{"changed deps": {"data/prepared/train.parquet": "modified"}}, {"changed outs": {"models/churn.pkl": "deleted"}}],
 "data/raw.csv.dvc": [{"changed outs": {"data/raw.csv": "modified"}}], "report": ["always changed"]}`
	fakeOut(t, "dvc", out, "0")
	r := lintResult(t, in, "ml/dvc-status")
	if r.Status != packs.Fail || len(r.Findings) != 3 {
		t.Fatalf("changed: %+v", r)
	}
	got := map[string]string{}
	for _, f := range r.Findings {
		got[f.File+" "+f.Rule] = f.Level
	}
	want := map[string]string{"data/raw.csv.dvc dvc-changed-outs": packs.LevelError, "dvc.yaml dvc-changed-deps": packs.LevelWarn, "dvc.yaml dvc-changed-outs": packs.LevelError}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	fakeOut(t, "dvc", "not json at all\n", "0")
	if r := lintResult(t, in, "ml/dvc-status"); r.Status != packs.Fail || !strings.Contains(r.Detail, "cannot parse") {
		t.Fatalf("bad output: %+v", r)
	}
}

func TestDeclaredMissingToolFails(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {tools: [dvc]}", map[string]string{"dvc.yaml": "stages: {}\n"})
	in.Tools = packs.NewToolRunner(in.Root, in.Config)
	noTools(in)
	r := lintResult(t, in, "ml/dvc-status")
	if r.Status != packs.Fail || !r.Required || r.Detail != packs.MissingDetail("ml") {
		t.Fatalf("result = %+v", r)
	}
}
