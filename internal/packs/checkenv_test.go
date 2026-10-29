package packs

import (
	"context"
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
