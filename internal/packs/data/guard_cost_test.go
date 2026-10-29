package data

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

type costGuardCase struct {
	name, tool, cmd string
	want            policy.Action
	reason          string // substring of the reason, when set
}

func runCostGuardCases(t *testing.T, id string, cfg packs.Config, cases []costGuardCase) {
	t.Helper()
	for _, c := range cases {
		tool := c.tool
		if tool == "" {
			tool = "Bash"
		}
		call := policy.ToolCall{Tool: tool, Command: c.cmd}
		if strings.HasPrefix(tool, "mcp__") {
			call = policy.ToolCall{Tool: tool, Content: c.cmd}
		}
		got, reason := runCostGuard(t, id, cfg, call)
		if got != c.want {
			t.Errorf("%s: %s: action %q, want %q (reason %q)", id, c.name, got, c.want, reason)
			continue
		}
		if c.reason != "" && !strings.Contains(reason, c.reason) {
			t.Errorf("%s: %s: reason %q does not contain %q", id, c.name, reason, c.reason)
		}
	}
}

func TestBQUnboundedQuery(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/bq-unbounded-query", cfg, []costGuardCase{
		{name: "no cap", cmd: `bq query --use_legacy_sql=false 'SELECT 1'`, want: policy.Deny, reason: "--maximum_bytes_billed=50000000000"},
		{name: "cap below", cmd: `bq query --maximum_bytes_billed=1000000 --use_legacy_sql=false 'SELECT 1'`, want: policy.Allow},
		{name: "cap separate value", cmd: `bq query --maximum_bytes_billed 1000000 'SELECT 1'`, want: policy.Allow},
		{name: "cap equal", cmd: `bq query --maximum_bytes_billed=50000000000 'SELECT 1'`, want: policy.Allow},
		{name: "cap above", cmd: `bq query --maximum_bytes_billed=60000000000 'SELECT 1'`, want: policy.Deny, reason: "above"},
		{name: "cap not a number", cmd: `bq query --maximum_bytes_billed=$MAX 'SELECT 1'`, want: policy.Deny},
		{name: "dry run", cmd: `bq query --dry_run --use_legacy_sql=false 'SELECT 1'`, want: policy.Allow},
		{name: "dry run false", cmd: `bq query --dry_run=false 'SELECT 1'`, want: policy.Deny},
		{name: "nodry_run", cmd: `bq query --nodry_run 'SELECT 1'`, want: policy.Deny},
		{name: "global flags first", cmd: `bq --project_id acme-dev --location=EU query 'SELECT 1'`, want: policy.Deny},
		{name: "stdin", cmd: `bq query --use_legacy_sql=false < q.sql`, want: policy.Deny},
		{name: "chained", cmd: `cd x && bq query 'SELECT 1'`, want: policy.Deny},
		{name: "wrapped in dotenv", cmd: `tq dotenv run -- bq query 'SELECT 1'`, want: policy.Deny},
		{name: "windows path", tool: "PowerShell", cmd: `& "C:\Program Files\gcloud\bq.cmd" query "SELECT 1"`, want: policy.Deny},
		{name: "single dash", cmd: `bq query -maximum_bytes_billed=10 'SELECT 1'`, want: policy.Allow},
		{name: "not a query", cmd: `bq ls acme_dataset`, want: policy.Allow},
		{name: "mk", cmd: `bq mk --table acme.orders schema.json`, want: policy.Allow},
		{name: "quoted mention", cmd: `git commit -m "fix bq query cap"`, want: policy.Allow},
		{name: "other tool", cmd: `echo hello`, want: policy.Allow},
	})
}

func TestBQUnboundedNoCapConfigured(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n")
	runCostGuardCases(t, "tq/bq-unbounded-query", cfg, []costGuardCase{
		{name: "no flag", cmd: `bq query 'SELECT 1'`, want: policy.Deny, reason: "--maximum_bytes_billed"},
		{name: "any flag", cmd: `bq query --maximum_bytes_billed=999999999999 'SELECT 1'`, want: policy.Allow},
	})
}

func TestBQGuardNeedsEngine(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [snowflake]\n")
	if a, _ := runCostGuard(t, "tq/bq-unbounded-query", cfg, policy.ToolCall{Tool: "Bash", Command: "bq query 'SELECT 1'"}); a != policy.Allow {
		t.Fatalf("bigquery not in engines: got %q", a)
	}
}

func TestWarehouseResize(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "sf above", cmd: `snow sql -q "ALTER WAREHOUSE acme_wh SET WAREHOUSE_SIZE = 'X-LARGE'"`, want: policy.Deny, reason: "MEDIUM"},
		{name: "sf at", cmd: `snow sql -q "ALTER WAREHOUSE acme_wh SET WAREHOUSE_SIZE = MEDIUM"`, want: policy.Ask},
		{name: "sf below", cmd: `snowsql -q "alter warehouse acme_wh set warehouse_size='xsmall'"`, want: policy.Ask},
		{name: "sf create above", cmd: `snow sql -q "CREATE OR REPLACE WAREHOUSE w WITH WAREHOUSE_SIZE='2X-LARGE'"`, want: policy.Deny},
		{name: "sf unknown size", cmd: `snow sql -q "ALTER WAREHOUSE w SET WAREHOUSE_SIZE = HUGE"`, want: policy.Ask},
		{name: "sf x6", cmd: `snow sql -q "ALTER WAREHOUSE w SET WAREHOUSE_SIZE = X6LARGE"`, want: policy.Deny},
		{name: "sf select", cmd: `snow sql -q "SELECT 1"`, want: policy.Allow},
		{name: "sf mcp", tool: "mcp__snowflake__run_query", cmd: `{"query":"ALTER WAREHOUSE w SET WAREHOUSE_SIZE = 'LARGE'"}`, want: policy.Deny},
		{name: "dbx num-workers above", cmd: `databricks clusters create 13.3.x-scala2.12 --num-workers 20`, want: policy.Deny, reason: "8"},
		{name: "dbx num-workers below", cmd: `databricks clusters edit 0123 13.3.x-scala2.12 --num-workers 4`, want: policy.Ask},
		{name: "dbx json above", cmd: `databricks clusters create --json '{"spark_version":"13.3.x","autoscale":{"min_workers":2,"max_workers":50}}'`, want: policy.Deny},
		{name: "dbx json below", cmd: `databricks clusters create --json '{"num_workers":2}'`, want: policy.Ask},
		{name: "dbx json file", cmd: `databricks clusters create --json @cluster.json`, want: policy.Ask, reason: "file"},
		{name: "dbx resize", cmd: `databricks clusters resize 0123 --num-workers 9`, want: policy.Deny},
		{name: "dbx warehouse", cmd: `databricks warehouses edit abc --cluster-size Large`, want: policy.Ask},
		{name: "dbx list", cmd: `databricks clusters list`, want: policy.Allow},
		{name: "syn sql above", cmd: `az synapse sql pool create --name p --performance-level DW1000c --workspace-name w -g rg`, want: policy.Deny, reason: "DW500c"},
		{name: "syn sql at", cmd: `az synapse sql pool update --name p --performance-level "DW500c" --workspace-name w -g rg`, want: policy.Ask},
		{name: "syn sql tags", cmd: `az synapse sql pool update --name p --tags a=b --workspace-name w -g rg`, want: policy.Allow},
		{name: "syn sql odd level", cmd: `az synapse sql pool update --name p --performance-level DW123 -g rg`, want: policy.Ask},
		{name: "syn spark above", cmd: `az synapse spark pool update --name p --enable-auto-scale --max-node-count 40 -g rg --workspace-name w`, want: policy.Deny},
		{name: "syn spark at", cmd: `az synapse spark pool create --name p --node-count 10 --node-size Small -g rg --workspace-name w --spark-version 3.4`, want: policy.Ask},
		{name: "fabric above", cmd: `az fabric capacity create -g rg --capacity-name acme --sku "{name:F64,tier:Fabric}"`, want: policy.Deny, reason: "F8"},
		{name: "fabric at", cmd: `az fabric capacity update -g rg --capacity-name acme --sku "{name:F8,tier:Fabric}"`, want: policy.Ask},
		{name: "fabric set", cmd: `az fabric capacity update -g rg -n acme --set sku.name=F128`, want: policy.Deny},
		{name: "fabric file", cmd: `az fabric capacity update -g rg -n acme --sku @sku.json`, want: policy.Ask},
		{name: "fabric admin only", cmd: `az fabric capacity update -g rg -n acme --administration "{members:[tester@acme.test]}"`, want: policy.Allow},
	})
}

func TestWarehouseResizeEngineGating(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n")
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "sf off", cmd: `snow sql -q "ALTER WAREHOUSE w SET WAREHOUSE_SIZE = X6LARGE"`, want: policy.Allow},
		{name: "dbx off", cmd: `databricks clusters create x --num-workers 99`, want: policy.Allow},
	})
	cfg = costConfig(t, "packs:\n  data:\n    engines: [snowflake]\n")
	runCostGuardCases(t, "tq/warehouse-resize", cfg, []costGuardCase{
		{name: "no cap asks", cmd: `snow sql -q "ALTER WAREHOUSE w SET WAREHOUSE_SIZE = X6LARGE"`, want: policy.Ask},
	})
}

func TestWarehouseQueryUnbounded(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	runCostGuardCases(t, "tq/warehouse-query-unbounded", cfg, []costGuardCase{
		{name: "sf no limit", cmd: `snow sql -q "SELECT * FROM acme.orders"`, want: policy.Ask, reason: "STATEMENT_TIMEOUT_IN_SECONDS"},
		{name: "sf limit", cmd: `snow sql -q "SELECT * FROM acme.orders LIMIT 10"`, want: policy.Allow},
		{name: "sf timeout", cmd: `snow sql -q "ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=60; SELECT * FROM acme.orders"`, want: policy.Allow},
		{name: "snowsql", cmd: `snowsql --query "select count(*) from orders"`, want: policy.Ask},
		{name: "sf show", cmd: `snow sql -q "SHOW WAREHOUSES"`, want: policy.Allow},
		{name: "sf file", cmd: `snow sql -f q.sql`, want: policy.Allow},
		{name: "pg no limit", cmd: `psql -h db -c "SELECT * FROM orders"`, want: policy.Ask, reason: "statement_timeout"},
		{name: "pg limit", cmd: `psql -c "SELECT * FROM orders LIMIT 5"`, want: policy.Allow},
		{name: "pg set", cmd: `psql -c "SET statement_timeout = '60s'" -c "SELECT * FROM orders"`, want: policy.Allow},
		{name: "pg env", cmd: `PGOPTIONS='-c statement_timeout=60000' psql -c "SELECT * FROM orders"`, want: policy.Allow},
		{name: "pg ps env", tool: "PowerShell", cmd: `$env:PGOPTIONS='-c statement_timeout=60000'; psql --command="SELECT * FROM orders"`, want: policy.Allow},
		{name: "pg string limit", cmd: `psql -c "SELECT 'limit' FROM orders"`, want: policy.Ask},
		{name: "dbx no timeout", cmd: `databricks api post /api/2.0/sql/statements --json '{"warehouse_id":"0123abcd","statement":"SELECT 1"}'`, want: policy.Ask, reason: "on_wait_timeout"},
		{name: "dbx bounded", cmd: `databricks api post /api/2.0/sql/statements --json '{"warehouse_id":"0123abcd","statement":"SELECT 1","wait_timeout":"30s","on_wait_timeout":"CANCEL"}'`, want: policy.Allow},
		{name: "dbx async", cmd: `databricks api post /api/2.0/sql/statements --json '{"statement":"SELECT 1","wait_timeout":"0s","on_wait_timeout":"CANCEL"}'`, want: policy.Ask},
		{name: "dbx file", cmd: `databricks api post /api/2.0/sql/statements --json @body.json`, want: policy.Ask},
		{name: "dbx other api", cmd: `databricks api get /api/2.0/clusters/list`, want: policy.Allow},
		{name: "fabric no top", cmd: `sqlcmd -S acme.datawarehouse.fabric.microsoft.com -d wh -G -Q "SELECT * FROM dbo.orders"`, want: policy.Ask, reason: "TOP"},
		{name: "fabric top", cmd: `sqlcmd -S acme.datawarehouse.fabric.microsoft.com -d wh -G -Q "SELECT TOP 10 * FROM dbo.orders"`, want: policy.Allow},
		{name: "synapse no top", cmd: `sqlcmd -S acme.sql.azuresynapse.net -d pool -G -Q "select * from dbo.orders"`, want: policy.Ask},
		{name: "other sql server", cmd: `sqlcmd -S localhost -Q "SELECT * FROM t"`, want: policy.Allow},
	})
}

func TestWarehouseQueryEngineGating(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, "packs:\n  data:\n    engines: [bigquery]\n")
	runCostGuardCases(t, "tq/warehouse-query-unbounded", cfg, []costGuardCase{
		{name: "pg off", cmd: `psql -c "SELECT * FROM orders"`, want: policy.Allow},
		{name: "sf off", cmd: `snow sql -q "SELECT * FROM orders"`, want: policy.Allow},
		{name: "fabric off", cmd: `sqlcmd -S acme.datawarehouse.fabric.microsoft.com -Q "SELECT * FROM t"`, want: policy.Allow},
	})
}

// The framework turns a guard panic into a deny; the guards here must not
// panic on hostile input (unbalanced quotes, huge commands, odd JSON).
func TestCostGuardsHostileInput(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	inputs := []string{`bq query "unterminated`, `databricks clusters create --json '{"autoscale":`, `az fabric capacity create --sku`,
		`snow sql -q`, `psql -c`, `sqlcmd -S`, `bq`, `bq query --maximum_bytes_billed`, "\x00\xff", strings.Repeat("bq query 'x' ; ", 4000)}
	for _, id := range []string{"tq/bq-unbounded-query", "tq/warehouse-resize", "tq/warehouse-query-unbounded"} {
		for _, in := range inputs {
			runCostGuard(t, id, cfg, policy.ToolCall{Tool: "Bash", Command: in})
		}
	}
}

func TestCostGuardsThroughFramework(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	outs := packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: "bq query 'SELECT 1'"}, packs.GuardEnv{Config: cfg}, nil, nil)
	found := false
	for _, o := range outs {
		if o.ID == "tq/bq-unbounded-query" {
			found = true
			if o.Action != policy.Deny || o.Failed {
				t.Fatalf("outcome %+v", o)
			}
		}
	}
	if !found {
		t.Fatal("tq/bq-unbounded-query did not run")
	}
	// guard.disable switches it off.
	outs = packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: "bq query 'SELECT 1'"}, packs.GuardEnv{Config: cfg}, func(id string) bool { return id == "tq/bq-unbounded-query" }, nil)
	for _, o := range outs {
		if o.ID == "tq/bq-unbounded-query" {
			t.Fatal("disabled guard ran")
		}
	}
	// A disabled data pack runs none of them.
	off := costConfig(t, "packs:\n  data:\n    enabled: false\n    engines: [bigquery]\n")
	if outs := packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: "bq query 'SELECT 1'"}, packs.GuardEnv{Config: off}, nil, nil); len(outs) != 0 {
		for _, o := range outs {
			if strings.HasPrefix(o.ID, "tq/bq") || strings.HasPrefix(o.ID, "tq/warehouse") {
				t.Fatalf("guard ran with the pack disabled: %+v", o)
			}
		}
	}
}

func BenchmarkCostGuards64KiB(b *testing.B) {
	cfg := packs.Merge(nil, nil)
	if m, err := costParseForBench(costFullConfig); err == nil {
		cfg = packs.Merge(m, nil)
	}
	cmd := strings.Repeat(`snow sql -q "SELECT * FROM acme.orders"; bq query --maximum_bytes_billed=10 'SELECT 1'; `, 64*1024/90)
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	ctx := packs.NewGuardCtx(packs.GuardEnv{Config: cfg}, "data")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkBQUnbounded(call, ctx)
		checkWarehouseResize(call, ctx)
		checkWarehouseQuery(call, ctx)
	}
}

func TestCostGuardsUnder50ms(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costFullConfig)
	cmd := strings.Repeat(`snow sql -q "SELECT * FROM acme.orders"; databricks clusters create --json '{"num_workers":2}'; bq query --maximum_bytes_billed=10 'SELECT 1'; `, 64*1024/140)
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	ctx := packs.NewGuardCtx(packs.GuardEnv{Config: cfg}, "data")
	var times []time.Duration
	for i := 0; i < 7; i++ {
		start := time.Now()
		checkBQUnbounded(call, ctx)
		checkWarehouseResize(call, ctx)
		checkWarehouseQuery(call, ctx)
		times = append(times, time.Since(start))
	}
	// median
	for i := range times {
		for j := i + 1; j < len(times); j++ {
			if times[j] < times[i] {
				times[i], times[j] = times[j], times[i]
			}
		}
	}
	if m := times[len(times)/2]; m > 50*time.Millisecond {
		t.Fatalf("three cost guards take %s on a 64 KiB command (median), budget 50ms each", m)
	}
}
