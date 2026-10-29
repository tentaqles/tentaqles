package data

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Fix round 1 (review r0): nested shells, global flags before the subcommand,
// JSON escapes in MCP input, per-segment timeouts.

func pwshEncoded(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, len(u)*2)
	for _, r := range u {
		b = append(b, byte(r), byte(r>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestCostGuardsNestedShells(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/bq-unbounded-query", cfg, []costGuardCase{
		{name: "bash -c", cmd: `bash -c "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "sh -c", cmd: `sh -c 'bq query "SELECT 1"'`, want: policy.Deny},
		{name: "bash -lc", cmd: `bash -lc "cd x && bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "zsh -ec", cmd: `zsh -ec "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "pwsh -Command", tool: "PowerShell", cmd: `pwsh -NoProfile -Command "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "powershell -c", tool: "PowerShell", cmd: `powershell.exe -c "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "powershell default command", tool: "PowerShell", cmd: `powershell -ExecutionPolicy Bypass "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "pwsh encoded", tool: "PowerShell", cmd: `pwsh -EncodedCommand ` + pwshEncoded(`bq query 'SELECT 1'`), want: policy.Deny},
		{name: "pwsh encoded capped", tool: "PowerShell", cmd: `pwsh -enc ` + pwshEncoded(`bq query --maximum_bytes_billed=10 'SELECT 1'`), want: policy.Allow},
		{name: "pwsh encoded garbage", tool: "PowerShell", cmd: `pwsh -e "!!! bq query"`, want: policy.Ask},
		{name: "cmd /c", cmd: `cmd /c "bq query SELECT 1"`, want: policy.Deny},
		{name: "cmd /c unquoted", cmd: `cmd.exe /c bq query SELECT 1`, want: policy.Deny},
		{name: "wsl sh -c", cmd: `wsl -d Ubuntu sh -c "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "wsl direct", cmd: `wsl bq query 'SELECT 1'`, want: policy.Deny},
		{name: "eval", cmd: `eval "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "iex", tool: "PowerShell", cmd: `Invoke-Expression "bq query 'SELECT 1'"`, want: policy.Deny},
		{name: "xargs sh -c", cmd: `cat q.txt | xargs -I{} sh -c "bq query '{}'"`, want: policy.Deny},
		{name: "nested capped", cmd: `bash -c "bq query --maximum_bytes_billed=10 'SELECT 1'"`, want: policy.Allow},
		{name: "two levels", cmd: `bash -c "sh -c \"bq query x\""`, want: policy.Deny},
		{name: "too deep", cmd: deepNest(`bq query x`, 8), want: policy.Ask},
		{name: "nested other", cmd: `bash -c "echo hello"`, want: policy.Allow},
		{name: "plain script", cmd: `bash run.sh`, want: policy.Allow},
	})
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "dbx in bash", cmd: `bash -c "databricks clusters create --num-workers 100"`, want: policy.Deny},
		{name: "sf in pwsh", tool: "PowerShell", cmd: `pwsh -Command "snow sql -q 'ALTER WAREHOUSE w SET WAREHOUSE_SIZE = X4LARGE'"`, want: policy.Deny},
		{name: "az in cmd", cmd: `cmd /c az fabric capacity update -g rg -n acme --set sku.name=F128`, want: policy.Deny},
		{name: "encoded dbx", tool: "PowerShell", cmd: `pwsh -ec ` + pwshEncoded(`databricks clusters resize 01 --num-workers 99`), want: policy.Deny},
	})
	runCostGuardCases(t, "tq/warehouse-query-unbounded", cfg, []costGuardCase{
		{name: "psql in bash", cmd: `bash -c "psql -c 'SELECT * FROM t'"`, want: policy.Ask},
		{name: "snow in sh", cmd: `sh -c "snow sql -q 'SELECT * FROM t'"`, want: policy.Ask},
		{name: "psql in bash bounded", cmd: `bash -c "psql -c 'SELECT * FROM t LIMIT 1'"`, want: policy.Allow},
	})
}

func deepNest(inner string, n int) string {
	s := inner
	for i := 0; i < n; i++ {
		s = "bash -c " + shQuote(s)
	}
	return s
}

func shQuote(s string) string { return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'` }

func TestCostGuardsGlobalFlags(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "dbx -p", cmd: `databricks -p prod clusters create --num-workers 100`, want: policy.Deny},
		{name: "dbx --profile json", cmd: `databricks --profile prod clusters create --json '{"num_workers":100}'`, want: policy.Deny},
		{name: "dbx -t -o", cmd: `databricks -t dev -o json clusters resize 0123 --num-workers 50`, want: policy.Deny},
		{name: "dbx --log-level", cmd: `databricks --log-level debug warehouses edit abc --cluster-size Large`, want: policy.Ask},
		{name: "dbx -p list", cmd: `databricks -p prod clusters list`, want: policy.Allow},
		{name: "snow --connection", cmd: `snow --connection acme sql -q "alter warehouse w set warehouse_size = x4large"`, want: policy.Deny},
		{name: "snow sql -c", cmd: `snow sql -c acme -q "alter warehouse w set warehouse_size = x4large"`, want: policy.Deny},
		{name: "az --subscription", cmd: `az --subscription acme-sub synapse sql pool update --name p --performance-level DW3000c -g rg`, want: policy.Deny},
		{name: "az -o fabric", cmd: `az -o json fabric capacity update -g rg -n acme --sku "{name:F64}"`, want: policy.Deny},
	})
	runCostGuardCases(t, "tq/warehouse-query-unbounded", cfg, []costGuardCase{
		{name: "dbx -p api", cmd: `databricks -p prod api post /api/2.0/sql/statements --json '{"statement":"SELECT 1"}'`, want: policy.Ask},
		{name: "dbx api json first", cmd: `databricks api post --json '{"statement":"SELECT 1"}' /api/2.0/sql/statements`, want: policy.Ask},
		{name: "dbx -p api bounded", cmd: `databricks -p prod api post /api/2.0/sql/statements --json '{"statement":"SELECT 1","wait_timeout":"30s","on_wait_timeout":"CANCEL"}'`, want: policy.Allow},
		{name: "snow --connection query", cmd: `snow --connection acme sql -q "SELECT * FROM t"`, want: policy.Ask},
	})
}

func TestWarehouseResizeMCPEscapes(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "newline", tool: "mcp__snowflake__run_query", cmd: `{"sql":"alter warehouse w set\nwarehouse_size=xxlarge"}`, want: policy.Deny},
		{name: "tab", tool: "mcp__snowflake__run_query", cmd: `{"sql":"ALTER WAREHOUSE w SET\twarehouse_size = 'X4LARGE'"}`, want: policy.Deny},
		{name: "unicode escape", tool: "mcp__snowflake__run_query", cmd: `{"sql":"ALTER WAREHOUSE w SET\u0020WAREHOUSE_SIZE\u003d'LARGE'"}`, want: policy.Deny},
		{name: "nested object", tool: "mcp__snowflake__run_query", cmd: `{"args":{"statements":["SELECT 1","ALTER WAREHOUSE w SET\nWAREHOUSE_SIZE=X6LARGE"]}}`, want: policy.Deny},
		{name: "not json", tool: "mcp__snowflake__run_query", cmd: `alter warehouse w set\nwarehouse_size=xxlarge`, want: policy.Deny},
		{name: "below cap", tool: "mcp__snowflake__run_query", cmd: `{"sql":"alter warehouse w set\nwarehouse_size=small"}`, want: policy.Ask},
		{name: "select", tool: "mcp__snowflake__run_query", cmd: `{"sql":"SELECT\n1"}`, want: policy.Allow},
	})
	// Backstop: a size the regex cannot read asks, never allows.
	var v costVerdict
	snowflakeResize("ALTER WAREHOUSE w SET WAREHOUSE_SIZE /* x */ = XLARGE", costOf(cfg.Pack("data")), &v)
	if v.action != policy.Ask && v.action != policy.Deny {
		t.Fatalf("unparsed WAREHOUSE_SIZE: got %q, want ask", v.action)
	}
}

func TestWarehouseQueryTimeoutPerSegment(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/warehouse-query-unbounded", cfg, []costGuardCase{
		{name: "pg timeout zero", cmd: `psql -c "SET statement_timeout = 0" -c "SELECT * FROM t"`, want: policy.Ask},
		{name: "pg timeout after", cmd: `psql -c "SELECT * FROM t" -c "SET statement_timeout = '60s'"`, want: policy.Ask},
		{name: "pg show", cmd: `psql -c "SHOW statement_timeout" -c "SELECT * FROM t"`, want: policy.Ask},
		{name: "pg set to", cmd: `psql -c "SET statement_timeout TO '30s'; SELECT * FROM t"`, want: policy.Allow},
		{name: "pg cross segment", cmd: `psql -c "SELECT * FROM big"; snow sql -q "ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=60; SELECT 1 LIMIT 1"`, want: policy.Ask, reason: "Postgres"},
		{name: "pg env zero", cmd: `PGOPTIONS='-c statement_timeout=0' psql -c "SELECT * FROM t"`, want: policy.Ask},
		{name: "pg export", cmd: `export PGOPTIONS='-c statement_timeout=60000'; psql -c "SELECT * FROM t"`, want: policy.Allow},
		{name: "pg ps env spaced", tool: "PowerShell", cmd: `$env:PGOPTIONS = '-c statement_timeout=60000'; psql -c "SELECT * FROM t"`, want: policy.Allow},
		{name: "pg plain var not exported", cmd: `PGOPTIONS='-c statement_timeout=60000'; psql -c "SELECT * FROM t"`, want: policy.Ask},
		{name: "pg other segment env", cmd: `PGOPTIONS='-c statement_timeout=60000' true; psql -c "SELECT * FROM t"`, want: policy.Ask},
		{name: "sf timeout zero", cmd: `snow sql -q "ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=0; SELECT * FROM t"`, want: policy.Ask},
		{name: "sf timeout after", cmd: `snow sql -q "SELECT * FROM t; ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=60"`, want: policy.Ask},
		{name: "sf show param", cmd: `snow sql -q "SHOW PARAMETERS LIKE 'STATEMENT_TIMEOUT_IN_SECONDS'; SELECT * FROM t"`, want: policy.Ask},
		{name: "sf cross segment", cmd: `snow sql -q "ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=60"; snow sql -q "SELECT * FROM t"`, want: policy.Ask},
	})
}

func TestBQLintQueryAndWait(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	src := []byte(`from google.cloud import bigquery

client = bigquery.Client()
capped = bigquery.Client(default_query_job_config=bigquery.QueryJobConfig(maximum_bytes_billed=10))
cfg = bigquery.QueryJobConfig(maximum_bytes_billed=10)
rows = client.query_and_wait("SELECT 1")
rows = client.query_and_wait("SELECT 2", job_config=cfg)
rows = capped.query_and_wait("SELECT 3")
rows = bigquery.Client().query("SELECT 4")
rows = bigquery.Client(project="acme").query_and_wait("SELECT 5")
rows = bigquery.Client(default_query_job_config=cfg).query("SELECT 6")
rows = bigquery.Client().query("SELECT 7", job_config=cfg)
`)
	cs := costOf(cfg.Pack("data"))
	limit, has := cs.Cap(engBigQuery, "max_bytes")
	l := &bqLint{file: "jobs/q.py", limit: limit, hasCap: has}
	l.lintPython(src, nil)
	var lines []int
	for _, f := range l.findings {
		if f.Rule != "bq-query-no-cap" {
			t.Fatalf("unexpected finding %+v", f)
		}
		lines = append(lines, f.Line)
	}
	want := []int{6, 9, 10}
	if len(lines) != len(want) {
		t.Fatalf("findings on lines %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("findings on lines %v, want %v", lines, want)
		}
	}
}

func TestBQLintNestedShell(t *testing.T) {
	l := &bqLint{file: "scripts/x.sh"}
	l.lintShell("bash -c \"bq query 'SELECT 1'\"\n")
	if len(l.findings) != 1 || l.findings[0].Rule != "bq-cli-no-cap" {
		t.Fatalf("findings %+v", l.findings)
	}
}

func TestBQJobMaxBytesUnreadableFails(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	root := t.TempDir()
	// scripts/load.sh is changed but cannot be read (it is missing on disk).
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(root, "scripts", "big.sh")
	if err := os.WriteFile(big, []byte(strings.Repeat("# x\n", (lintMaxBytes/4)+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	in := (&packs.Input{Root: root, Config: cfg, Changed: []packs.ChangedFile{
		{Path: "scripts/big.sh", Status: 'M'}, {Path: "scripts/load.sh", Status: 'M'},
	}}).ForPack("data")
	r := costCheck(t, "data/bq-job-max-bytes").Run(context.Background(), in)
	levels := map[string]string{}
	for _, f := range r.Findings {
		levels[f.File] = f.Level
	}
	if levels["scripts/big.sh"] != packs.LevelWarn {
		t.Fatalf("over the size bound: %+v", r.Findings)
	}
	if levels["scripts/load.sh"] != packs.LevelError || r.Status != packs.Fail {
		t.Fatalf("unreadable file must fail the check: status %v findings %+v", r.Status, r.Findings)
	}
}

func TestWarehouseCostFlagCountsTableMaterializations(t *testing.T) {
	if n := costMaterializedTableRe.FindAll([]byte("a:\n  +materialized: table\nb:\n  +materialized: table\n"), -1); len(n) != 2 {
		t.Fatalf("regex count %d", len(n))
	}
	if !costMoreTables([]byte("+materialized: table\n"), []byte("+materialized: table\n+materialized: 'table'\n")) {
		t.Fatal("a second table materialization must raise the flag")
	}
	if costMoreTables([]byte("+materialized: table\n+materialized: table\n"), []byte("+materialized: table\n")) {
		t.Fatal("removing one must not raise it")
	}
	if costMoreTables([]byte("+materialized: table\n"), []byte("+materialized: table\n")) {
		t.Fatal("same count must not raise it")
	}
}
