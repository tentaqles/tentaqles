package packs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

type specTools struct{ got *toolrun.Spec }

func (s specTools) Probe(pack, tool string) toolrun.Probe {
	return toolrun.Probe{Tool: tool, Found: true}
}
func (s specTools) Run(_ context.Context, sp toolrun.Spec) (toolrun.Output, error) {
	*s.got = sp
	return toolrun.Output{Warnings: []string{"dbt resolves inside the repository (venv); the declared secret variable(s) DBT_PASSWORD were not passed"}}, nil
}

// Round 5: a check's declared Check.Env names reach the tool run.
func TestCheckEnvReachesTheToolRun(t *testing.T) {
	var got toolrun.Spec
	ct := checkTools{ToolRunner: specTools{got: &got}, env: []string{"DBT_PROFILES_DIR"}}
	if _, err := ct.Run(context.Background(), toolrun.Spec{Tool: "dbt", PassEnv: []string{"DBT_TARGET"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.PassEnv, "DBT_PROFILES_DIR") || !slices.Contains(got.PassEnv, "DBT_TARGET") {
		t.Fatalf("PassEnv %v", got.PassEnv)
	}
}

// R5-5: a run's framework warnings are collected for the check's result.
func TestToolWarningsAreCollected(t *testing.T) {
	var got toolrun.Spec
	w := &toolWarnings{}
	ct := checkTools{ToolRunner: specTools{got: &got}, warns: w}
	ct.Run(context.Background(), toolrun.Spec{Tool: "dbt"})
	ct.Run(context.Background(), toolrun.Spec{Tool: "dbt"})
	if l := w.list(); len(l) != 1 || !strings.Contains(l[0], "DBT_PASSWORD") {
		t.Fatalf("%q", l)
	}
}

// R6-3: a binary reached through a junction (or symlink) to a folder under
// the repo is repo-local, by the handle-based FinalPath.
func TestRepoLocalThroughALink(t *testing.T) {
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	os.MkdirAll(tools, 0o700)
	os.WriteFile(filepath.Join(tools, "dbt.exe"), []byte("x"), 0o700)
	link := filepath.Join(t.TempDir(), "j")
	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command("cmd", "/c", "mklink", "/J", link, tools).Run()
	} else {
		err = os.Symlink(tools, link)
	}
	if err != nil {
		t.Skipf("cannot create a link: %v", err)
	}
	r := &toolrun.Runner{Root: root}
	if !r.RepoLocal(toolrun.Probe{Path: filepath.Join(link, "dbt.exe"), Source: "path"}) {
		t.Fatal("a binary under the repo, reached through a link, passed as outside it")
	}
	out := filepath.Join(t.TempDir(), "dbt.exe")
	os.WriteFile(out, []byte("x"), 0o700)
	if r.RepoLocal(toolrun.Probe{Path: out, Source: "path"}) {
		t.Fatal("an outside binary counted as repo-local")
	}
}
