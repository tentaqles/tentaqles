package data

import (
	"os"
	"path/filepath"
	"testing"

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
