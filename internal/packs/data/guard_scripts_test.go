package data

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 2: a guard that asks on pwsh -File also asks on a shell
// script file (bash x.sh, ./x.sh) whose content names its tool, read through
// ReadLocal; an unreadable script stays allowed unless the command line
// names the tool.
func TestGuardsAskOnScriptFiles(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	scripts := map[string]string{
		"run_dag.sh":  "#!/bin/sh\nairflow dags trigger orders_daily\n",
		"build.sh":    "set -e\ndbt run --target prod\n",
		"query.sh":    "bq query 'SELECT 1'\n",
		"resize.sh":   "databricks clusters edit --json x\n",
		"psql.sh":     "psql -c 'select * from orders'\n",
		"harmless.sh": "echo hello\nls -la\n",
	}
	for n, body := range scripts {
		if err := os.WriteFile(filepath.Join(repo, n), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "packs:\n  data:\n    enabled: true\n    engines: [bigquery, databricks, postgres]\n"
	m, err := manifest.ParseRepoPacks([]byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	env := packs.GuardEnv{Config: packs.Merge(m, nil), Manifest: &manifest.Manifest{Packs: m}, Workspace: "acme",
		WorkspaceRoot: repo, RepoRoot: repo, Cwd: repo}
	action := func(id, cmd string) policy.Action {
		for _, o := range packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: cmd}, env, nil, nil) {
			if o.ID == id && o.Action != "" {
				return o.Action
			}
		}
		return policy.Allow
	}
	for _, c := range []struct {
		id, cmd string
		want    policy.Action
	}{
		{"tq/pipeline-run", "bash run_dag.sh", policy.Ask},
		{"tq/pipeline-run", "./run_dag.sh", policy.Ask},
		{"tq/pipeline-run", "cd . && sh -e run_dag.sh", policy.Ask},
		{"tq/pipeline-run", "bash harmless.sh", policy.Allow},
		{"tq/pipeline-run", "bash missing.sh", policy.Allow},
		{"tq/pipeline-run", "./missing.sh --then airflow", policy.Ask},
		{guardProdTarget, "bash build.sh", policy.Ask},
		{guardFullRefresh, "./build.sh", policy.Ask},
		{guardProdTarget, "./harmless.sh", policy.Allow},
		{"tq/bq-unbounded-query", "zsh query.sh", policy.Ask},
		{"tq/bq-unbounded-query", "./harmless.sh", policy.Allow},
		{"tq/warehouse-resize", "./resize.sh", policy.Ask},
		{"tq/warehouse-query-unbounded", "bash psql.sh", policy.Ask},
		{"tq/warehouse-query-unbounded", "bash harmless.sh", policy.Allow},
		// Outside the ReadLocal roots: unreadable, and the line names no tool.
		{"tq/pipeline-run", "bash " + filepath.Join(t.TempDir(), "x.sh"), policy.Allow},
	} {
		if got := action(c.id, c.cmd); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.id, c.cmd, got, c.want)
		}
	}
}

// Many script words on a 64 KiB command stay inside the guard budget (at
// most MaxScripts reads, a linear split).
func TestScriptFilesGuardBudget(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	env := guardEnv(t, repo, "data: {enabled: true}")
	env.Cwd = repo
	cmd := strings.Repeat("bash x.sh; ./y.sh; source z.sh; ", 64<<10/32)
	g := packs.NewGuardCtx(env, "data")
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	for name, fn := range map[string]func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool){
		"dbt-prod-target": withScripts(checkProdTarget, dbtToolRe, nil, "dbt"),
		"pipeline-run":    withScripts(pipelineRunGuard, pipelineToolRe, nil, "x"),
		"bq":              withScripts(checkBQUnbounded, bqToolRe, nil, "bq"),
	} {
		var durs []time.Duration
		for i := 0; i < 9; i++ {
			start := time.Now()
			fn(call, g)
			durs = append(durs, time.Since(start))
		}
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		t.Logf("%s median %v", name, durs[len(durs)/2])
		if med := durs[len(durs)/2]; med > packs.GuardBudget {
			t.Fatalf("%s median %v", name, med)
		}
	}
	// The whole hook-side evaluation is not failed by a blown budget.
	for _, o := range packs.EvaluateGuards(call, env, nil, nil) {
		if o.Failed {
			t.Fatalf("guard failed on the budget: %+v", o)
		}
	}
}
