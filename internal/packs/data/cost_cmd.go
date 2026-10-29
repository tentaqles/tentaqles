package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Exit codes of tq pack data cost estimate.
const (
	costExitWithin = 0
	costExitOver   = 1
	costExitUsage  = 2
	costExitEngine = 3
	costExitAsk    = 4
)

// costEnv is what the estimate command runs with.
type costEnv struct {
	Root      string
	Workspace string
	Config    packs.Config
	Tools     packs.ToolRunner
}

// costCmdEnv builds the environment from the working directory: the trusted
// workspace's manifest (none outside one), tq-packs.yaml at the repo root, and
// the tool runner. Tests replace it.
var costCmdEnv = func() (costEnv, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return costEnv{}, err
	}
	env := costEnv{Root: cwd}
	if r, err := packs.RepoRoot(context.Background(), cwd); err == nil {
		env.Root = r
	}
	var ws *resolve.Workspace
	if reg, err := registry.Load(); err == nil {
		ws = resolve.Resolve(cwd, reg).Workspace
	}
	if ws != nil && ws.Manifest != nil {
		env.Workspace = ws.Name
		cfg, err := packs.LoadConfig(ws.Manifest, env.Root)
		env.Config = cfg
		if err != nil {
			return env, err
		}
	} else {
		cfg, err := packs.LoadConfig(nil, env.Root)
		env.Config = cfg
		if err != nil {
			return env, err
		}
	}
	env.Tools = packs.NewToolRunner(env.Root, env.Config)
	return env, nil
}

// costExit ends the process with a code (tests replace it).
var costExit = os.Exit

func newCostCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cost",
		Short: "Warehouse cost guardrail: estimate a query before it runs",
	}
	c.AddCommand(newCostEstimateCmd())
	return c
}

func newCostEstimateCmd() *cobra.Command {
	var engine, file, server, database, warehouse string
	var stdin, asJSON bool
	c := &cobra.Command{
		Use:   "estimate --engine <e> (--file q.sql | --stdin) [--json]",
		Short: "Measure one SELECT with the engine's plan-only form and judge it against the cap",
		Long: `Reads one SELECT (DML, DDL, several statements and ANALYZE are refused) and
runs only the engine's plan-only form, so the query never runs:

  bigquery    bq query --dry_run --use_legacy_sql=false --format=json
  snowflake   snow sql --stdin --format json   (EXPLAIN USING JSON)
  databricks  databricks api post /api/2.0/sql/statements   (EXPLAIN COST)
  synapse     sqlcmd -G ... (EXPLAIN)
  fabric      sqlcmd -G ... (SET SHOWPLAN_XML ON)
  postgres    psql -c "EXPLAIN (FORMAT JSON) ..."

The CLI uses its own login and the identity's environment; wrap the call in
tq dotenv run when it needs a token. The query text is never printed.

Exit: 0 within the cap, 1 over the cap, 2 usage or config error,
3 the plan-only call failed, 4 unknown estimate or no cap (ask a human).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			usage := func(format string, a ...any) error {
				fmt.Fprintf(cmd.ErrOrStderr(), "tq pack data cost estimate: "+format+"\n", a...)
				costExit(costExitUsage)
				return nil
			}
			switch {
			case engine == "":
				return usage("--engine is required (%s)", strings.Join(costEngines, ", "))
			case !costKnownEngine(engine):
				return usage("unknown engine %q (known: %s)", engine, strings.Join(costEngines, ", "))
			case file != "" && stdin:
				return usage("use --file or --stdin, not both")
			case file == "" && !stdin:
				return usage("give the query with --file or --stdin")
			}
			var sql []byte
			var err error
			if stdin {
				sql, err = io.ReadAll(io.LimitReader(cmd.InOrStdin(), estimateMaxSQL+1))
			} else {
				sql, err = costReadSQLFile(file)
			}
			if err != nil {
				return usage("reading the query: %v", err)
			}
			if len(sql) > estimateMaxSQL {
				return usage("the query is larger than %d bytes", estimateMaxSQL)
			}
			env, err := costCmdEnv()
			if err != nil {
				return usage("%v", err)
			}
			scratch, cleanup, err := toolrun.Scratch(env.Workspace)
			if err != nil {
				return usage("scratch dir: %v", err)
			}
			defer cleanup()
			s := env.Config.Pack("data")
			est, err := RunEstimate(context.Background(), env.Tools, s, EstimateRequest{
				Engine: engine, SQL: sql, Server: server, Database: database, WarehouseID: warehouse, ScratchDir: scratch, Dir: env.Root,
			})
			if err != nil {
				if errors.Is(err, ErrCostEngine) {
					fmt.Fprintln(cmd.ErrOrStderr(), "tq pack data cost estimate: "+err.Error())
					costExit(costExitEngine)
					return nil
				}
				return usage("%v", err)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(est); err != nil {
					return err
				}
			} else {
				writeEstimateText(out, est)
			}
			packs.Count("data", "cost_estimate:"+est.Verdict, 1)
			switch est.Verdict {
			case VerdictOver:
				costExit(costExitOver)
			case VerdictAsk:
				costExit(costExitAsk)
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&engine, "engine", "", "warehouse engine: "+strings.Join(costEngines, ", "))
	f.StringVar(&file, "file", "", "read the SELECT from this file")
	f.BoolVar(&stdin, "stdin", false, "read the SELECT from standard input")
	f.BoolVar(&asJSON, "json", false, "print "+EstimateSchema+" JSON")
	f.StringVar(&server, "server", "", "sqlcmd -S / psql -h (otherwise the CLI's own configuration)")
	f.StringVar(&database, "database", "", "sqlcmd -d / psql -d")
	f.StringVar(&warehouse, "warehouse-id", "", "Databricks SQL warehouse id (default packs.data.cost.databricks.warehouse_id)")
	return c
}

func costReadSQLFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, estimateMaxSQL+1))
}

func writeEstimateText(w io.Writer, est CostEstimate) {
	row := func(k, v string) { fmt.Fprintf(w, "%-9s %s\n", k, v) }
	row("engine", est.Engine+" ("+est.Tool+")")
	row("metric", est.Metric)
	row("estimate", costFormat(est))
	for _, k := range costSortedKeys(est.Reported) {
		row("", fmt.Sprintf("%s %g", k, est.Reported[k]))
	}
	switch {
	case est.Cap != "":
		row("cap", est.Cap+" ("+est.CapKey+")")
	case est.CapKey != "":
		row("cap", "none ("+est.CapKey+" is not set)")
	default:
		row("cap", "none for this engine")
	}
	row("verdict", est.Verdict)
	if est.Note != "" {
		row("note", est.Note)
	}
}

func costSortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
