package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// tq pack data cost estimate (spec C §3.6): measure one SELECT with the
// engine's plan-only form, never running it, and judge the number against
// the cap. The query text is never printed, logged or put in a finding.

// EstimateSchema is the schema id of the --json output.
const EstimateSchema = "tq.packs.cost/v1"

// EstimateTimeout bounds each plan-only call.
const EstimateTimeout = 60 * time.Second

// estimateMaxSQL caps the query read from --file or --stdin.
const estimateMaxSQL = 1 << 20

// Verdicts.
const (
	VerdictWithin = "within"
	VerdictOver   = "over"
	VerdictAsk    = "ask"
)

// CostEstimate is the result of one estimate. It never holds query text.
type CostEstimate struct {
	Schema   string             `json:"schema"`
	Engine   string             `json:"engine"`
	Tool     string             `json:"tool"`
	Metric   string             `json:"metric"`
	Value    *float64           `json:"value"`
	Unit     string             `json:"unit"`
	Cap      string             `json:"cap,omitempty"`
	CapKey   string             `json:"cap_key,omitempty"`
	CapValue *float64           `json:"cap_value,omitempty"`
	Verdict  string             `json:"verdict"`
	Reported map[string]float64 `json:"reported,omitempty"`
	Note     string             `json:"note,omitempty"`
}

// EstimateRequest is one estimate to make.
type EstimateRequest struct {
	Engine      string
	SQL         []byte
	Server      string // sqlcmd -S / psql -h (optional; the CLI's own config otherwise)
	Database    string // sqlcmd -d / psql -d (optional)
	WarehouseID string // Databricks; cost.databricks.warehouse_id otherwise
	ScratchDir  string // where the sqlcmd input file goes ("" = a temp dir)
	Dir         string // working directory of the CLI ("" = the runner's root)
}

// ErrCostRefused marks a query the estimate will not send.
var ErrCostRefused = errors.New("query refused")

// ErrCostEngine marks a failed plan-only call.
var ErrCostEngine = errors.New("the plan-only call failed")

// costEngineTool is the CLI each engine's plan-only call runs.
var costEngineTool = map[string]string{
	engBigQuery: "bq", engSnowflake: "snow", engDatabricks: "databricks",
	engSynapse: "sqlcmd", engFabric: "sqlcmd", engPostgres: "psql",
}

// costRefusedWords are words that never appear in an estimated SELECT: they
// write, change state, or would make EXPLAIN run the statement.
var costRefusedWords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "UPSERT": true, "CREATE": true,
	"DROP": true, "ALTER": true, "TRUNCATE": true, "GRANT": true, "REVOKE": true, "COPY": true, "CALL": true, "EXEC": true,
	"EXECUTE": true, "ANALYZE": true, "ANALYSE": true, "EXPLAIN": true, "INTO": true, "DECLARE": true, "VACUUM": true,
	"UNLOAD": true, "PUT": true, "REMOVE": true, "SHOWPLAN_XML": true, "SHOWPLAN_ALL": true, "STATISTICS": true, "BEGIN": true,
	"COMMIT": true, "ROLLBACK": true, "USE": true, "LOCK": true, "REFRESH": true, "OPTIMIZE": true, "RESTORE": true, "UNDROP": true}

var sqlcmdCommandRe = regexp.MustCompile(`(?im)^\s*(:|!!|go\b|ed\b|exit\b|quit\b|reset\b)`)

// ValidateEstimateSQL checks that src is one SELECT (or WITH … SELECT) the
// estimate may send, and returns it without the trailing semicolon.
func ValidateEstimateSQL(src []byte) (string, error) {
	text := strings.TrimSpace(string(src))
	text = strings.TrimSpace(strings.TrimRight(text, "; \t\r\n"))
	if text == "" {
		return "", fmt.Errorf("%w: the query is empty", ErrCostRefused)
	}
	if strings.ContainsRune(text, '\x00') {
		return "", fmt.Errorf("%w: the query holds a NUL byte", ErrCostRefused)
	}
	if strings.Contains(text, `\`) {
		return "", fmt.Errorf("%w: backslashes are not accepted (they change quoting between engines)", ErrCostRefused)
	}
	if strings.Contains(text, ";") {
		return "", fmt.Errorf("%w: only one statement can be estimated (a ';' remains inside the query)", ErrCostRefused)
	}
	if strings.Contains(text, "$(") || sqlcmdCommandRe.MatchString(text) {
		return "", fmt.Errorf("%w: the query holds a client command or variable ($(…), :cmd, !!, GO)", ErrCostRefused)
	}
	stmts := scan.SQLStatements([]byte(text))
	if len(stmts) != 1 {
		return "", fmt.Errorf("%w: exactly one statement is estimated, got %d", ErrCostRefused, len(stmts))
	}
	st := stmts[0]
	if kw := st.Keyword(); kw != "SELECT" && kw != "WITH" {
		return "", fmt.Errorf("%w: only SELECT is estimated (got %s); DML, DDL and ANALYZE are refused", ErrCostRefused, costKwOr(kw))
	}
	for _, t := range st.Tokens {
		switch {
		case t.Kind == scan.TokJinja:
			return "", fmt.Errorf("%w: the query holds Jinja; compile it first (dbt compile)", ErrCostRefused)
		case t.Kind == scan.TokWord && costRefusedWords[t.Upper()]:
			return "", fmt.Errorf("%w: the query uses %s; only a read-only SELECT is estimated", ErrCostRefused, t.Upper())
		}
	}
	return text, nil
}

func costKwOr(kw string) string {
	if kw == "" {
		return "no keyword"
	}
	return kw
}

// EstimateSpec builds the plan-only call for an engine. cleanup removes any
// input file it wrote.
func EstimateSpec(s packs.Settings, r EstimateRequest, sql string) (spec toolrun.Spec, cleanup func(), err error) {
	cleanup = func() {}
	tool, ok := costEngineTool[r.Engine]
	if !ok {
		return spec, cleanup, fmt.Errorf("unknown engine %q (known: %s)", r.Engine, strings.Join(costEngines, ", "))
	}
	spec = toolrun.Spec{Pack: "data", Tool: tool, Timeout: EstimateTimeout, Dir: r.Dir}
	switch r.Engine {
	case engBigQuery:
		spec.Args = []string{"query", "--dry_run", "--use_legacy_sql=false", "--format=json"}
		spec.Stdin = []byte(sql + "\n")
	case engSnowflake:
		spec.Args = []string{"sql", "--stdin", "--format", "json"}
		spec.Stdin = []byte("EXPLAIN USING JSON " + sql + ";\n")
	case engDatabricks:
		wh := r.WarehouseID
		if wh == "" {
			wh = costOf(s).Raw(engDatabricks, "warehouse_id")
		}
		if !costIDRe.MatchString(wh) {
			return spec, cleanup, fmt.Errorf("databricks needs a SQL warehouse id: set packs.data.cost.databricks.warehouse_id or pass --warehouse-id")
		}
		body, _ := json.Marshal(map[string]string{
			"warehouse_id": wh, "statement": "EXPLAIN COST " + sql,
			"wait_timeout": "50s", "on_wait_timeout": "CANCEL", "disposition": "INLINE", "format": "JSON_ARRAY",
		})
		spec.Args = []string{"api", "post", "/api/2.0/sql/statements", "--json", string(body)}
	case engSynapse, engFabric:
		input := "EXPLAIN " + sql + "\nGO\n"
		if r.Engine == engFabric {
			input = "SET SHOWPLAN_XML ON\nGO\n" + sql + "\nGO\n"
		}
		dir := r.ScratchDir
		made := false
		if dir == "" {
			d, err := os.MkdirTemp("", "tq-cost-")
			if err != nil {
				return spec, cleanup, err
			}
			dir, made = d, true
		}
		f := filepath.Join(dir, "estimate.sql")
		if err := os.WriteFile(f, []byte(input), 0o600); err != nil {
			return spec, cleanup, err
		}
		cleanup = func() {
			_ = os.Remove(f)
			if made {
				_ = os.RemoveAll(dir)
			}
		}
		spec.Args = []string{"-G", "-b", "-x", "-X1", "-y", "0"}
		if r.Server != "" {
			spec.Args = append(spec.Args, "-S", r.Server)
		}
		if r.Database != "" {
			spec.Args = append(spec.Args, "-d", r.Database)
		}
		spec.Args = append(spec.Args, "-i", f)
	case engPostgres:
		spec.Args = []string{"-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"}
		if r.Server != "" {
			spec.Args = append(spec.Args, "-h", r.Server)
		}
		if r.Database != "" {
			spec.Args = append(spec.Args, "-d", r.Database)
		}
		spec.Args = append(spec.Args, "-c", "EXPLAIN (FORMAT JSON) "+sql)
	}
	return spec, cleanup, nil
}

// costDisplay is the plan-only call as a human would type it, with the
// query replaced by a placeholder (never the query text).
func costDisplay(spec toolrun.Spec) string {
	parts := []string{spec.Tool}
	for i, a := range spec.Args {
		switch {
		case i > 0 && (spec.Args[i-1] == "-c" || spec.Args[i-1] == "--json"):
			parts = append(parts, "<plan-only statement>")
		case i > 0 && spec.Args[i-1] == "-i":
			parts = append(parts, "<input file>")
		default:
			parts = append(parts, a)
		}
	}
	if spec.Stdin != nil {
		parts = append(parts, "< <query file>")
	}
	return strings.Join(parts, " ")
}

// RunEstimate validates the query, runs the plan-only call and judges it.
func RunEstimate(ctx context.Context, tools packs.ToolRunner, s packs.Settings, r EstimateRequest) (CostEstimate, error) {
	est := CostEstimate{Schema: EstimateSchema, Engine: r.Engine, Tool: costEngineTool[r.Engine], Verdict: VerdictAsk}
	sql, err := ValidateEstimateSQL(r.SQL)
	if err != nil {
		return est, err
	}
	spec, cleanup, err := EstimateSpec(s, r, sql)
	defer cleanup()
	if err != nil {
		return est, err
	}
	out, err := tools.Run(ctx, spec)
	if errors.Is(err, toolrun.ErrNotFound) {
		return est, fmt.Errorf("%w: %s is not installed (install it or add packs.data.tool_paths.%s)", ErrCostEngine, spec.Tool, spec.Tool)
	}
	if out.TimedOut {
		return est, fmt.Errorf("%w: %s did not answer within %s", ErrCostEngine, spec.Tool, EstimateTimeout)
	}
	if err != nil && out.ExitCode == 0 {
		return est, fmt.Errorf("%w: %s: %v", ErrCostEngine, spec.Tool, err)
	}
	if out.ExitCode != 0 {
		// The CLI's output may echo the query, so it is not shown.
		return est, fmt.Errorf("%w: %s exited %d; run `%s` yourself to see the error", ErrCostEngine, spec.Tool, out.ExitCode, costDisplay(spec))
	}
	costParsePlan(&est, out.Stdout)
	costJudge(&est, s)
	return est, nil
}

// costParsePlan fills the metric from the engine's plan-only output.
func costParsePlan(est *CostEstimate, stdout []byte) {
	set := func(v float64) { est.Value = &v }
	switch est.Engine {
	case engBigQuery:
		est.Metric, est.Unit = "total_bytes_processed", "bytes"
		if v, ok := bqBytes(stdout); ok {
			set(v)
		}
	case engSnowflake:
		est.Metric, est.Unit = "bytes_assigned", "bytes"
		if gs, ok := snowflakeGlobalStats(stdout); ok {
			est.Reported = map[string]float64{}
			for k, name := range map[string]string{"partitionsTotal": "partitions_total", "partitionsAssigned": "partitions_assigned", "bytesAssigned": "bytes_assigned"} {
				if v, ok := gs[k]; ok {
					est.Reported[name] = v
				}
			}
			if v, ok := gs["bytesAssigned"]; ok {
				set(v)
			}
		}
	case engDatabricks:
		est.Metric, est.Unit = "max_size_in_bytes", "bytes"
		v, ok, note := databricksSize(stdout)
		if ok {
			set(v)
		}
		est.Note = note
	case engSynapse:
		est.Metric, est.Unit = "total_cost", "estimated ms"
		if m := synCostRe.FindSubmatch(stdout); m != nil {
			if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
				set(v)
			}
		}
		if m := synOpsRe.FindSubmatch(stdout); m != nil {
			if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
				est.Reported = map[string]float64{"total_number_operations": v}
			}
		}
	case engFabric:
		est.Metric, est.Unit = "statement_subtree_cost", "optimizer cost units"
		if m := fabricCostRe.FindSubmatch(stdout); m != nil {
			if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
				set(v)
			}
		}
	case engPostgres:
		est.Metric, est.Unit = "total_cost", "planner cost units"
		if v, ok := postgresTotalCost(stdout); ok {
			set(v)
		}
	}
	if est.Value == nil && est.Note == "" {
		est.Note = "the plan-only output did not carry " + est.Metric + "; the estimate is unknown"
	}
}

// costCapFor is the cap each engine's metric is judged against.
var costCapFor = map[string]string{
	engBigQuery: "max_bytes", engSnowflake: "max_bytes", engDatabricks: "max_bytes", engPostgres: "max_plan_cost",
}

func costJudge(est *CostEstimate, s packs.Settings) {
	cs := costOf(s)
	key := costCapFor[est.Engine]
	if key != "" {
		est.CapKey = "packs.data.cost." + est.Engine + "." + key
		est.Cap = cs.Raw(est.Engine, key)
		if v, ok := cs.Cap(est.Engine, key); ok {
			est.CapValue = &v
		}
	}
	switch {
	case est.Value == nil:
		est.Verdict = VerdictAsk
	case est.CapValue == nil:
		est.Verdict = VerdictAsk
		if est.Note == "" {
			if key == "" {
				est.Note = "this engine's estimate is reported only; there is no cap to compare it with"
			} else {
				est.Note = "no cap is configured (" + est.CapKey + ")"
			}
		}
	case *est.Value > *est.CapValue:
		est.Verdict = VerdictOver
	default:
		est.Verdict = VerdictWithin
	}
}

// costFindNumber walks decoded JSON for the first value under key.
func costFindNumber(v any, key string) (float64, bool) {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok {
			if f, ok := costNumber(x); ok {
				return f, true
			}
		}
		for _, x := range t {
			if f, ok := costFindNumber(x, key); ok {
				return f, true
			}
		}
	case []any:
		for _, x := range t {
			if f, ok := costFindNumber(x, key); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func costNumber(x any) (float64, bool) {
	switch n := x.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

var bqTextRe = regexp.MustCompile(`process(?:es)?\s+([0-9]+)\s+bytes`)

// bqBytes reads totalBytesProcessed from `bq query --dry_run --format=json`
// (the job resource: statistics.totalBytesProcessed, statistics.query.…),
// falling back to the text form.
func bqBytes(out []byte) (float64, bool) {
	var v any
	if json.Unmarshal(out, &v) == nil {
		if f, ok := costFindNumber(v, "totalBytesProcessed"); ok {
			return f, true
		}
	}
	if m := bqTextRe.FindSubmatch(out); m != nil {
		f, err := strconv.ParseFloat(string(m[1]), 64)
		return f, err == nil
	}
	return 0, false
}

// snowflakeGlobalStats reads GlobalStats from `snow sql --format json` output
// of EXPLAIN USING JSON: a list of rows whose column (content) holds the plan
// as a JSON string, or the plan object itself.
func snowflakeGlobalStats(out []byte) (map[string]float64, bool) {
	var v any
	if json.Unmarshal(out, &v) != nil {
		return nil, false
	}
	var found map[string]float64
	var walk func(any, int)
	walk = func(x any, depth int) {
		if found != nil || depth > 8 {
			return
		}
		switch t := x.(type) {
		case map[string]any:
			if gs, ok := t["GlobalStats"].(map[string]any); ok {
				found = map[string]float64{}
				for k, val := range gs {
					if f, ok := costNumber(val); ok {
						found[k] = f
					}
				}
				return
			}
			for _, y := range t {
				walk(y, depth+1)
			}
		case []any:
			for _, y := range t {
				walk(y, depth+1)
			}
		case string:
			if strings.Contains(t, "GlobalStats") {
				var inner any
				if json.Unmarshal([]byte(t), &inner) == nil {
					walk(inner, depth+1)
				}
			}
		}
	}
	walk(v, 0)
	return found, found != nil
}

var (
	sizeInBytesRe = regexp.MustCompile(`sizeInBytes=([0-9]+(?:\.[0-9]+)?)\s*([KMGTPE]i?B|B)?`)
	synCostRe     = regexp.MustCompile(`total_cost="([0-9.eE+-]+)"`)
	synOpsRe      = regexp.MustCompile(`total_number_operations="([0-9]+)"`)
	fabricCostRe  = regexp.MustCompile(`StatementSubTreeCost="([0-9.eE+-]+)"`)
)

// databricksSize reads the root sizeInBytes of the Optimized Logical Plan in
// a Statement Execution API response to EXPLAIN COST.
func databricksSize(out []byte) (float64, bool, string) {
	var resp struct {
		Status struct {
			State string `json:"state"`
		} `json:"status"`
		Result struct {
			DataArray [][]any `json:"data_array"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, false, "the Statement Execution API response did not parse"
	}
	if st := strings.ToUpper(resp.Status.State); st != "" && st != "SUCCEEDED" {
		return 0, false, "the EXPLAIN statement ended in state " + st
	}
	var plan strings.Builder
	for _, row := range resp.Result.DataArray {
		for _, c := range row {
			if s, ok := c.(string); ok {
				plan.WriteString(s)
				plan.WriteByte('\n')
			}
		}
	}
	text := plan.String()
	if i := strings.Index(text, "== Optimized Logical Plan =="); i >= 0 {
		text = text[i:]
	}
	if i := strings.Index(text, "== Physical Plan =="); i >= 0 {
		text = text[:i]
	}
	// The root's size is the result; the scans are the cost. The largest
	// node size of the optimized plan is the number compared.
	best, found := 0.0, false
	for _, m := range sizeInBytesRe.FindAllStringSubmatch(text, 1000) {
		f, _ := strconv.ParseFloat(m[1], 64)
		if u := m[2]; u != "" && u != "B" {
			// Spark prints binary units (KiB … EiB; older versions KB … EB, also binary).
			f *= math.Pow(1024, float64(strings.IndexByte("BKMGTPE", u[0])))
		}
		if f >= 8*math.Pow(1024, 6) {
			return 0, false, "Spark reported no statistics (8.0 EiB); run ANALYZE TABLE on the sources for an estimate"
		}
		if f > best {
			best = f
		}
		found = true
	}
	return best, found, ""
}

// postgresTotalCost reads Plan."Total Cost" from EXPLAIN (FORMAT JSON).
func postgresTotalCost(out []byte) (float64, bool) {
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if json.Unmarshal(out, &plans) != nil || len(plans) == 0 || plans[0].Plan == nil {
		return 0, false
	}
	return costNumber(plans[0].Plan["Total Cost"])
}

// costFormat prints an estimate's number for the text output.
func costFormat(est CostEstimate) string {
	if est.Value == nil {
		return "unknown"
	}
	if est.Unit == "bytes" {
		return fmt.Sprintf("%s (%.0f bytes)", costHumanBytes(*est.Value), *est.Value)
	}
	return strconv.FormatFloat(*est.Value, 'f', -1, 64) + " " + est.Unit
}
