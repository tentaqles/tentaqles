package data

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestCostValidateSQL(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"select id from acme.orders where status = 'open';",
		"WITH o AS (SELECT * FROM orders) SELECT count(*) FROM o",
		"  SELECT a FROM t -- trailing comment\n",
	}
	for _, q := range ok {
		if _, err := ValidateEstimateSQL([]byte(q)); err != nil {
			t.Errorf("%q refused: %v", q, err)
		}
	}
	bad := []string{
		"", ";", "DELETE FROM orders", "INSERT INTO t SELECT 1", "UPDATE t SET a = 1", "DROP TABLE t",
		"SELECT 1; DROP TABLE t", "SELECT 1; SELECT 2", "ANALYZE t", "EXPLAIN ANALYZE SELECT 1",
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", "SELECT * INTO new_t FROM t",
		"SELECT 'a\\'' FROM t", "SELECT {{ ref('orders') }}", "SELECT 1\nGO\nDROP TABLE t", "SELECT $(var)",
		"SELECT 1\n:!! dir", "MERGE INTO t USING s ON 1=1", "CALL p()", "SELECT 'x;y'", "COPY t TO '/tmp/x'",
	}
	for _, q := range bad {
		if _, err := ValidateEstimateSQL([]byte(q)); !errors.Is(err, ErrCostRefused) {
			t.Errorf("%q accepted (err %v)", q, err)
		}
	}
}

const costEstimateConfig = `packs:
  data:
    engines: [bigquery, snowflake, databricks, postgres, fabric, synapse]
    cost:
      bigquery:   { max_bytes: 1GB }
      snowflake:  { max_bytes: 100MB }
      databricks: { warehouse_id: 0123abcd, max_bytes: 10GB }
      postgres:   { max_plan_cost: 10000 }
`

func TestCostParsePlans(t *testing.T) {
	costIsolate(t)
	s := costConfig(t, costEstimateConfig).Pack("data")
	for _, e := range []struct{ engine, file string }{
		{engBigQuery, "output.json"}, {engSnowflake, "output.json"}, {engDatabricks, "output.json"},
		{engSynapse, "output.txt"}, {engFabric, "output.txt"}, {engPostgres, "output.json"},
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", "cost-estimate", e.engine, e.file))
		if err != nil {
			t.Fatal(err)
		}
		est := CostEstimate{Schema: EstimateSchema, Engine: e.engine, Tool: costEngineTool[e.engine]}
		costParsePlan(&est, raw)
		costJudge(&est, s)
		costGolden(t, filepath.Join("testdata", "cost-estimate", e.engine, "want.json"), est)
	}
}

func TestCostParsePlanUnknown(t *testing.T) {
	costIsolate(t)
	s := costConfig(t, costEstimateConfig).Pack("data")
	for _, engine := range costEngines {
		est := CostEstimate{Engine: engine}
		costParsePlan(&est, []byte(`{"unexpected": true}`))
		costJudge(&est, s)
		if est.Value != nil || est.Verdict != VerdictAsk {
			t.Errorf("%s: unknown output gave %+v", engine, est)
		}
	}
	// Spark's "no statistics" size is unknown, not huge.
	v, ok, note := databricksSize([]byte(`{"status":{"state":"SUCCEEDED"},"result":{"data_array":[["== Optimized Logical Plan ==\nProject, Statistics(sizeInBytes=8.0 EiB)"]]}}`))
	if ok || note == "" {
		t.Fatalf("8 EiB read as %v", v)
	}
}

// costFakeCLIs builds testdata/costfake once per test into a temp dir, copies it
// under each tool name, and prepends the dir to PATH. Behaviour files go in
// the returned dir (TQ_COST_FAKE_DIR).
func costFakeCLIs(t *testing.T, tools ...string) string {
	t.Helper()
	bin := t.TempDir()
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goTool + costExe()); err != nil {
		goTool = "go"
	}
	exe := filepath.Join(bin, "costfake"+costExe())
	cmd := exec.Command(goTool, "build", "-o", exe, "./testdata/costfake")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake CLI: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range tools {
		if err := os.WriteFile(filepath.Join(bin, name+costExe()), raw, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	t.Setenv("TQ_COST_FAKE_DIR", state)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return state
}

func costExe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func costCopyFixture(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunEstimateWithFakeCLIs(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "bq", "snow", "databricks", "sqlcmd", "psql")
	cfg := costConfig(t, costEstimateConfig)
	s := cfg.Pack("data")
	root := t.TempDir()
	tools := packs.NewToolRunner(root, cfg)
	query := []byte("SELECT status, count(*) FROM acme.orders WHERE customer = 'tester@acme.test' GROUP BY status")
	cases := []struct {
		engine, tool, file, costVerdict string
		check                           func(log string) bool
	}{
		{engBigQuery, "bq", "output.json", VerdictOver, func(l string) bool {
			return strings.Contains(l, "query\x1f--dry_run\x1f--use_legacy_sql=false\x1f--format=json") && strings.Contains(l, "stdin:SELECT status")
		}},
		{engSnowflake, "snow", "output.json", VerdictWithin, func(l string) bool {
			return strings.Contains(l, "sql\x1f--stdin\x1f--format\x1fjson") && strings.Contains(l, "stdin:EXPLAIN USING JSON SELECT")
		}},
		{engDatabricks, "databricks", "output.json", VerdictOver, func(l string) bool {
			return strings.Contains(l, `"statement":"EXPLAIN COST SELECT`) && strings.Contains(l, `"on_wait_timeout":"CANCEL"`) && strings.Contains(l, `"warehouse_id":"0123abcd"`)
		}},
		{engSynapse, "sqlcmd", "output.txt", VerdictAsk, func(l string) bool {
			return strings.Contains(l, "-G\x1f-b\x1f-x\x1f-X1") && strings.Contains(l, "input:EXPLAIN SELECT")
		}},
		{engPostgres, "psql", "output.json", VerdictOver, func(l string) bool {
			return strings.Contains(l, "-c\x1fEXPLAIN (FORMAT JSON) SELECT") && strings.Contains(l, "ON_ERROR_STOP=1")
		}},
	}
	for _, c := range cases {
		costCopyFixture(t, filepath.Join("testdata", "cost-estimate", c.engine, c.file), filepath.Join(state, c.tool+".out"))
		est, err := RunEstimate(context.Background(), tools, s, EstimateRequest{Engine: c.engine, SQL: query})
		if err != nil {
			t.Fatalf("%s: %v", c.engine, err)
		}
		if est.Verdict != c.costVerdict {
			t.Errorf("%s: costVerdict %s, want %s (%+v)", c.engine, est.Verdict, c.costVerdict, est)
		}
		log, _ := os.ReadFile(filepath.Join(state, c.tool+".log"))
		if !c.check(string(log)) {
			t.Errorf("%s: unexpected call:\n%s", c.engine, log)
		}
		_ = os.Remove(filepath.Join(state, c.tool+".log"))
	}
	// Fabric: SHOWPLAN_XML in its own batch, reported only.
	costCopyFixture(t, filepath.Join("testdata", "cost-estimate", "fabric", "output.txt"), filepath.Join(state, "sqlcmd.out"))
	est, err := RunEstimate(context.Background(), tools, s, EstimateRequest{Engine: engFabric, SQL: query, Server: "acme.datawarehouse.fabric.microsoft.com", Database: "wh"})
	if err != nil || est.Verdict != VerdictAsk || est.Value == nil {
		t.Fatalf("fabric: %+v %v", est, err)
	}
	log, _ := os.ReadFile(filepath.Join(state, "sqlcmd.log"))
	if !strings.Contains(string(log), "input:SET SHOWPLAN_XML ON\nGO\nSELECT") || !strings.Contains(string(log), "-S\x1facme.datawarehouse.fabric.microsoft.com\x1f-d\x1fwh") {
		t.Fatalf("fabric call:\n%s", log)
	}
}

func TestRunEstimateFailureHidesQuery(t *testing.T) {
	costIsolate(t)
	state := costFakeCLIs(t, "bq")
	cfg := costConfig(t, costEstimateConfig)
	if err := os.WriteFile(filepath.Join(state, "bq.out"), []byte("Error in query string: tester@acme.test near SELECT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "bq.exit"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := RunEstimate(context.Background(), packs.NewToolRunner(t.TempDir(), cfg), cfg.Pack("data"), EstimateRequest{Engine: engBigQuery, SQL: []byte("SELECT 'tester@acme.test'")})
	if !errors.Is(err, ErrCostEngine) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "tester") || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("error leaks the query: %v", err)
	}
	// A refused query never reaches the CLI.
	_ = os.Remove(filepath.Join(state, "bq.log"))
	if _, err := RunEstimate(context.Background(), packs.NewToolRunner(t.TempDir(), cfg), cfg.Pack("data"), EstimateRequest{Engine: engBigQuery, SQL: []byte("DELETE FROM t")}); !errors.Is(err, ErrCostRefused) {
		t.Fatalf("DML: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "bq.log")); err == nil {
		t.Fatal("the CLI ran for a refused query")
	}
}

func TestRunEstimateMissingCLI(t *testing.T) {
	costIsolate(t)
	cfg := costConfig(t, costEstimateConfig)
	t.Setenv("PATH", t.TempDir())
	_, err := RunEstimate(context.Background(), packs.NewToolRunner(t.TempDir(), cfg), cfg.Pack("data"), EstimateRequest{Engine: engSnowflake, SQL: []byte("SELECT 1")})
	if !errors.Is(err, ErrCostEngine) || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestCostDatabricksNeedsWarehouse(t *testing.T) {
	costIsolate(t)
	s := costConfig(t, "packs:\n  data:\n    engines: [databricks]\n").Pack("data")
	if _, _, err := EstimateSpec(s, EstimateRequest{Engine: engDatabricks}, "SELECT 1"); err == nil {
		t.Fatal("no warehouse id accepted")
	}
}
