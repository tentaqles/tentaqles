package data

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// The warehouse cost guards (spec C §3.6). They are pure: a regex and a small
// tokenizer over the command, plus the loaded config. They read no file and
// run nothing. Queries inside scripts are the lint's job (data/bq-job-max-bytes).

const (
	GuardBQUnbounded    = "tq/bq-unbounded-query"
	GuardWarehouseSize  = "tq/warehouse-resize"
	GuardWarehouseQuery = "tq/warehouse-query-unbounded"
)

// costVerdict collects the strongest action over the segments of one command.
type costVerdict struct {
	action policy.Action
	reason string
}

func (v *costVerdict) add(a policy.Action, reason string) {
	rank := func(a policy.Action) int {
		switch a {
		case policy.Deny:
			return 2
		case policy.Ask:
			return 1
		}
		return 0
	}
	if rank(a) > rank(v.action) {
		v.action, v.reason = a, reason
	}
}

func (v costVerdict) result() (policy.Action, string, bool) {
	return v.action, v.reason, v.action != policy.Allow
}

// bqCommands are the bq subcommands; the first of them after `bq` (skipping
// flags and their values) is the command.
var bqCommands = map[string]bool{"query": true, "mk": true, "rm": true, "ls": true, "show": true, "load": true, "extract": true,
	"cp": true, "head": true, "insert": true, "update": true, "partition": true, "cancel": true, "wait": true, "help": true,
	"init": true, "shell": true, "version": true, "mkdef": true, "truncate": true, "undelete": true,
	"add-iam-policy-binding": true, "get-iam-policy": true, "set-iam-policy": true, "remove-iam-policy-binding": true}

// bqQuery reports whether the args after `bq` run `bq query`, and the flags.
func bqQuery(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if bqCommands[a] {
			return a == "query"
		}
	}
	return false
}

// bqFlag returns the values of an absl flag (--name or -name, = or space).
func bqFlag(args []string, name string) []string {
	return costFlagValues(args, "--"+name, "-"+name)
}

// bqDryRun reports a dry run (--dry_run, --dry_run=true); --nodry_run and
// --dry_run=false are not.
func bqDryRun(args []string) bool {
	dry := false
	for _, a := range args {
		t := strings.TrimLeft(a, "-")
		if !strings.HasPrefix(a, "-") {
			continue
		}
		switch {
		case t == "dry_run":
			dry = true
		case strings.HasPrefix(t, "dry_run="):
			v := strings.ToLower(strings.TrimPrefix(t, "dry_run="))
			dry = v == "true" || v == "1" || v == "t" || v == "yes"
		case t == "nodry_run":
			dry = false
		}
	}
	return dry
}

func checkBQUnbounded(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	cs := costOf(g.Settings())
	low := strings.ToLower(call.Command)
	if !cs.Engine(engBigQuery) || (!strings.Contains(low, "bq") && !costMayNest(low)) {
		return policy.Allow, "", false
	}
	limit, hasCap := cs.Cap(engBigQuery, "max_bytes")
	suggest := "--maximum_bytes_billed=<bytes>"
	if hasCap {
		suggest = fmt.Sprintf("--maximum_bytes_billed=%.0f", limit)
	}
	var v costVerdict
	segs, opaque := costSegmentsDeep(call.Command)
	if opaque && costNamesTool(low, "bq") {
		v.add(policy.Ask, "a nested shell command the guard cannot read names bq; confirm it carries --maximum_bytes_billed")
	}
	for _, seg := range segs {
		i := seg.find("bq")
		if i < 0 {
			continue
		}
		args := seg.after(i)
		if !bqQuery(args) || bqDryRun(args) {
			continue
		}
		vals := bqFlag(args, "maximum_bytes_billed")
		if len(vals) == 0 {
			msg := "bq query without a byte cap: add " + suggest
			if hasCap {
				msg += " (packs.data.cost.bigquery.max_bytes = " + cs.Raw(engBigQuery, "max_bytes") + ")"
			}
			v.add(policy.Deny, msg+", or measure it first with --dry_run or tq pack data cost estimate")
			continue
		}
		for _, raw := range vals {
			n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
			if err != nil || n <= 0 {
				v.add(policy.Deny, "bq query --maximum_bytes_billed must be a literal byte count; use "+suggest)
				continue
			}
			if hasCap && float64(n) > limit {
				v.add(policy.Deny, fmt.Sprintf("bq query --maximum_bytes_billed=%d is above the cap %s (packs.data.cost.bigquery.max_bytes); use %s", n, cs.Raw(engBigQuery, "max_bytes"), suggest))
			}
		}
	}
	return v.result()
}

var (
	whStmtRe = regexp.MustCompile(`(?is)\b(ALTER|CREATE)\s+(?:OR\s+REPLACE\s+)?WAREHOUSE\b([^;]*)`)
	whSizeRe = regexp.MustCompile(`(?i)\bWAREHOUSE_SIZE\s*=?\s*[\\'"]*([A-Za-z0-9-]+)`)
)

// snowflakeResize judges every ALTER/CREATE WAREHOUSE … WAREHOUSE_SIZE in text.
func snowflakeResize(text string, cs costSettings, v *costVerdict) {
	if !whStmtRe.MatchString(text) {
		return
	}
	limit, hasCap := cs.Cap(engSnowflake, "max_warehouse_size")
	for _, m := range whStmtRe.FindAllStringSubmatch(text, 50) {
		sz := whSizeRe.FindStringSubmatch(m[2])
		if sz == nil {
			if strings.Contains(strings.ToLower(m[2]), "warehouse_size") {
				// a size the guard cannot read: never allow it
				v.add(policy.Ask, "sets a Snowflake WAREHOUSE_SIZE the guard cannot read; confirm the size")
				continue
			}
			if strings.EqualFold(m[1], "CREATE") {
				v.add(policy.Ask, "creates a Snowflake warehouse; confirm its size and auto-suspend")
			}
			continue
		}
		r, ok := snowflakeRank(sz[1])
		switch {
		case !ok:
			v.add(policy.Ask, "Snowflake warehouse size "+strconv.Quote(sz[1])+" is not on the known scale; confirm it")
		case hasCap && float64(r) > limit:
			v.add(policy.Deny, fmt.Sprintf("Snowflake WAREHOUSE_SIZE %s is above the cap %s (packs.data.cost.snowflake.max_warehouse_size)", sz[1], cs.Raw(engSnowflake, "max_warehouse_size")))
		default:
			v.add(policy.Ask, "resizes a Snowflake warehouse to "+sz[1]+"; confirm it")
		}
	}
}

// snowQueries returns the -q/--query texts of a `snow sql` or `snowsql`
// segment, and whether the segment is one.
func snowQueries(seg costSegment) ([]string, bool) {
	if i := seg.find("snowsql"); i >= 0 {
		return costFlagValues(seg.after(i), "-q", "--query"), true
	}
	if i := seg.find("snow"); i >= 0 {
		args := seg.after(i)
		if costSubPath(args, snowValueFlags, snowGroups) != nil {
			return costFlagValues(args, "-q", "--query"), true
		}
	}
	return nil, false
}

func costIntFlag(args []string, names ...string) (int, bool, bool) {
	vals := costFlagValues(args, names...)
	if len(vals) == 0 {
		return 0, false, false
	}
	best, okAll := 0, true
	for _, s := range vals {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			okAll = false
			continue
		}
		if n > best {
			best = n
		}
	}
	return best, true, okAll
}

// databricksResize judges clusters create|edit|resize and warehouses create|edit.
func databricksResize(args []string, cs costSettings, v *costVerdict) {
	pos := costSubPath(args, dbxValueFlags, dbxGroups)
	if len(pos) < 2 {
		return
	}
	group, verb := pos[0], pos[1]
	switch {
	case group == "warehouses" && (verb == "create" || verb == "edit"):
		v.add(policy.Ask, "creates or resizes a Databricks SQL warehouse (cluster size and cluster count); confirm the sizing")
		return
	case group == "clusters" && (verb == "create" || verb == "edit" || verb == "resize"):
	default:
		return
	}
	limit, hasCap := cs.Cap(engDatabricks, "max_workers")
	workers, seen := 0, false
	if n, ok, clean := costIntFlag(args, "--num-workers"); ok {
		if !clean {
			v.add(policy.Ask, "databricks clusters --num-workers is not a literal number; confirm the cluster size")
		}
		workers, seen = n, true
	}
	for _, body := range costFlagValues(args, "--json") {
		if strings.HasPrefix(strings.TrimSpace(body), "@") {
			v.add(policy.Ask, "databricks clusters --json reads a file the guard cannot see; confirm num_workers and autoscale.max_workers")
			continue
		}
		var req struct {
			NumWorkers *int `json:"num_workers"`
			Autoscale  *struct {
				MaxWorkers *int `json:"max_workers"`
			} `json:"autoscale"`
		}
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			v.add(policy.Ask, "databricks clusters --json is not valid JSON to the guard; confirm the cluster size")
			continue
		}
		if req.NumWorkers != nil && *req.NumWorkers > workers {
			workers, seen = *req.NumWorkers, true
		}
		if req.Autoscale != nil && req.Autoscale.MaxWorkers != nil && *req.Autoscale.MaxWorkers > workers {
			workers, seen = *req.Autoscale.MaxWorkers, true
		}
	}
	if costHasFlag(args, "--json-file") {
		v.add(policy.Ask, "databricks clusters --json-file reads a file the guard cannot see; confirm the cluster size")
	}
	switch {
	case seen && hasCap && float64(workers) > limit:
		v.add(policy.Deny, fmt.Sprintf("databricks cluster with %d workers is above the cap %s (packs.data.cost.databricks.max_workers)", workers, cs.Raw(engDatabricks, "max_workers")))
	default:
		v.add(policy.Ask, "creates or resizes a Databricks cluster; confirm the worker count")
	}
}

// synapseResize judges az synapse sql pool create|update|restore and spark pool create|update.
func synapseResize(pos, args []string, cs costSettings, v *costVerdict) {
	if len(pos) < 4 || pos[0] != "synapse" || pos[2] != "pool" {
		return
	}
	kind, verb := pos[1], pos[3]
	switch {
	case kind == "sql" && (verb == "create" || verb == "update" || verb == "restore"):
		levels := costFlagValues(args, "--performance-level")
		if len(levels) == 0 {
			if verb == "create" {
				v.add(policy.Ask, "creates a Synapse dedicated SQL pool; confirm its performance level")
			}
			return
		}
		limit, hasCap := cs.Cap(engSynapse, "max_dwu")
		for _, l := range levels {
			n, ok := synapseDWU(l)
			switch {
			case !ok:
				v.add(policy.Ask, "Synapse performance level "+strconv.Quote(l)+" is not on the known scale; confirm it")
			case hasCap && float64(n) > limit:
				v.add(policy.Deny, fmt.Sprintf("Synapse --performance-level %s is above the cap %s (packs.data.cost.synapse.max_dwu)", l, cs.Raw(engSynapse, "max_dwu")))
			default:
				v.add(policy.Ask, "sets a Synapse SQL pool to "+l+"; confirm it")
			}
		}
	case kind == "spark" && (verb == "create" || verb == "update"):
		n, ok, clean := costIntFlag(args, "--node-count", "--max-node-count")
		if !ok {
			if verb == "create" {
				v.add(policy.Ask, "creates a Synapse Spark pool; confirm its node count")
			}
			return
		}
		limit, hasCap := cs.Cap(engSynapse, "max_spark_nodes")
		switch {
		case !clean:
			v.add(policy.Ask, "Synapse Spark node count is not a literal number; confirm it")
		case hasCap && float64(n) > limit:
			v.add(policy.Deny, fmt.Sprintf("Synapse Spark pool with %d nodes is above the cap %s (packs.data.cost.synapse.max_spark_nodes)", n, cs.Raw(engSynapse, "max_spark_nodes")))
		default:
			v.add(policy.Ask, "sizes a Synapse Spark pool; confirm the node count")
		}
	}
}

var skuInTextRe = regexp.MustCompile(`(?i)\bF([0-9]+)\b`)

// fabricResize judges az fabric capacity create|update --sku (and --set sku…).
func fabricResize(pos, args []string, cs costSettings, v *costVerdict) {
	if len(pos) < 3 || pos[0] != "fabric" || pos[1] != "capacity" || (pos[2] != "create" && pos[2] != "update") {
		return
	}
	var skus []string
	skus = append(skus, costFlagValues(args, "--sku")...)
	for _, s := range costFlagValues(args, "--set") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "sku") {
			skus = append(skus, s)
		}
	}
	if len(skus) == 0 {
		if pos[2] == "create" {
			v.add(policy.Ask, "creates a Fabric capacity; confirm its SKU")
		}
		return
	}
	limit, hasCap := cs.Cap(engFabric, "max_capacity_sku")
	for _, s := range skus {
		if strings.HasPrefix(strings.TrimSpace(s), "@") {
			v.add(policy.Ask, "az fabric capacity --sku reads a file the guard cannot see; confirm the SKU")
			continue
		}
		m := skuInTextRe.FindStringSubmatch(s)
		n, ok := 0, false
		if m != nil {
			n, ok = fabricSKU("F" + m[1])
		}
		switch {
		case !ok:
			v.add(policy.Ask, "Fabric capacity SKU is not on the known scale (F2 … F2048); confirm it")
		case hasCap && float64(n) > limit:
			v.add(policy.Deny, fmt.Sprintf("Fabric capacity SKU F%d is above the cap %s (packs.data.cost.fabric.max_capacity_sku)", n, cs.Raw(engFabric, "max_capacity_sku")))
		default:
			v.add(policy.Ask, fmt.Sprintf("sets a Fabric capacity to F%d; confirm it", n))
		}
	}
}

func checkWarehouseResize(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	cs := costOf(g.Settings())
	var v costVerdict
	if call.IsMCP() {
		if cs.Engine(engSnowflake) {
			// The input is JSON: judge its decoded strings, so a \n or \t
			// escape in a multi-line statement cannot hide WAREHOUSE_SIZE.
			for _, s := range costMCPStrings(call.Content) {
				snowflakeResize(s, cs, &v)
			}
		}
		return v.result()
	}
	low := strings.ToLower(call.Command)
	if !strings.Contains(low, "snow") && !strings.Contains(low, "databricks") && !strings.Contains(low, "az") && !costMayNest(low) {
		return policy.Allow, "", false
	}
	segs, opaque := costSegmentsDeep(call.Command)
	if opaque && ((cs.Engine(engSnowflake) && (costNamesTool(low, "snow") || costNamesTool(low, "snowsql"))) ||
		(cs.Engine(engDatabricks) && costNamesTool(low, "databricks")) ||
		((cs.Engine(engSynapse) || cs.Engine(engFabric)) && costNamesTool(low, "az"))) {
		v.add(policy.Ask, "a nested shell command the guard cannot read names a warehouse CLI; confirm the compute size")
	}
	for _, seg := range segs {
		if cs.Engine(engSnowflake) {
			if qs, ok := snowQueries(seg); ok {
				for _, q := range qs {
					snowflakeResize(q, cs, &v)
				}
			}
		}
		if cs.Engine(engDatabricks) {
			if i := seg.find("databricks"); i >= 0 {
				databricksResize(seg.after(i), cs, &v)
			}
		}
		if i := seg.find("az"); i >= 0 {
			args := seg.after(i)
			pos := costSubPath(args, azValueFlags, azGroups)
			if cs.Engine(engSynapse) {
				synapseResize(pos, args, cs, &v)
			}
			if cs.Engine(engFabric) {
				fabricResize(pos, args, cs, &v)
			}
		}
	}
	return v.result()
}

var (
	costSelectRe     = regexp.MustCompile(`(?i)\b(SELECT|WITH)\b`)
	costLimitRe      = regexp.MustCompile(`(?i)\bLIMIT\s+[0-9]+|\bFETCH\s+(FIRST|NEXT)\b`)
	costTopRe        = regexp.MustCompile(`(?i)\bSELECT\s+(DISTINCT\s+)?TOP\b`)
	costSelectWordRe = regexp.MustCompile(`(?i)\bSELECT\b`)
	fabricHost       = regexp.MustCompile(`(?i)[a-z0-9-]+\.datawarehouse\.fabric\.microsoft\.com`)
	synHost          = regexp.MustCompile(`(?i)[a-z0-9-]+\.sql\.azuresynapse\.net`)
	costQuotedRe     = regexp.MustCompile(`'(?:[^']|'')*'`)
)

// costUnquoted strips SQL string literals so `SELECT 'limit'` does not count.
func costUnquoted(q string) string { return costQuotedRe.ReplaceAllString(q, "''") }

func checkWarehouseQuery(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	cs := costOf(g.Settings())
	low := strings.ToLower(call.Command)
	if !strings.Contains(low, "snow") && !strings.Contains(low, "psql") && !strings.Contains(low, "databricks") && !strings.Contains(low, "sqlcmd") && !costMayNest(low) {
		return policy.Allow, "", false
	}
	var v costVerdict
	sf, pg, dbx := cs.Engine(engSnowflake), cs.Engine(engPostgres), cs.Engine(engDatabricks)
	segs, opaque := costSegmentsDeep(call.Command)
	if opaque && ((sf && (costNamesTool(low, "snow") || costNamesTool(low, "snowsql"))) || (pg && costNamesTool(low, "psql")) ||
		(dbx && costNamesTool(low, "databricks")) || costNamesTool(low, "sqlcmd")) {
		v.add(policy.Ask, "a nested shell command the guard cannot read runs a warehouse query; confirm it is bounded")
	}
	// pgEnv is a PGOPTIONS statement timeout exported by an earlier segment
	// (export PGOPTIONS=…, $env:PGOPTIONS = …, set PGOPTIONS=…).
	pgEnv := false
	for _, seg := range segs {
		if sf {
			if qs, ok := snowQueries(seg); ok {
				// One snow/snowsql call is one session: the timeout must be
				// set in it, before the query, to a non-zero value.
				bounded := false
				for _, q := range qs {
					for _, st := range costStatements(q) {
						if set, n := costTimeoutSet(sfTimeoutRe, sfTimeoutAnyRe, st); set {
							bounded = n
							continue
						}
						if !bounded && costUnbounded(st) {
							v.add(policy.Ask, "Snowflake query with no statement timeout and no LIMIT: add ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=<s>; in the same command, or a LIMIT")
						}
					}
				}
			}
		}
		if pg {
			if i := seg.find("psql"); i >= 0 {
				bounded := pgEnv
				if set, n := costPGOptions(strings.Join(seg.Words[:i], " ")); set {
					bounded = n
				}
				for _, q := range costFlagValues(seg.after(i), "-c", "--command") {
					for _, st := range costStatements(q) {
						if set, n := costTimeoutSet(pgTimeoutRe, pgTimeoutAnyRe, st); set {
							bounded = n
							continue
						}
						if !bounded && costUnbounded(st) {
							v.add(policy.Ask, "Postgres query with no statement timeout and no LIMIT: add -c \"SET statement_timeout = '60s'\" before it, PGOPTIONS='-c statement_timeout=60000', or a LIMIT")
						}
					}
				}
			} else if len(seg.Words) > 0 {
				first := strings.ToLower(seg.Words[0])
				if first == "export" || first == "set" || strings.HasPrefix(first, "$env:pgoptions") {
					if set, n := costPGOptions(strings.Join(seg.Words, " ")); set {
						pgEnv = n
					}
				}
			}
		}
		if dbx {
			if i := seg.find("databricks"); i >= 0 {
				args := seg.after(i)
				pos := costSubPath(args, dbxValueFlags, dbxGroups)
				if len(pos) >= 3 && pos[0] == "api" && pos[1] == "post" {
					endpoint := ""
					for _, p := range pos[2:] {
						if strings.HasPrefix(p, "/") {
							endpoint = p
							break
						}
					}
					if strings.Contains(endpoint, "/sql/statements") && !strings.Contains(endpoint, "/cancel") {
						databricksStatement(costFlagValues(args, "--json"), &v)
					}
				}
			}
		}
		if i := seg.find("sqlcmd"); i >= 0 {
			args := seg.after(i)
			joined := strings.Join(args, " ")
			engine := ""
			switch {
			case fabricHost.MatchString(joined):
				engine = engFabric
			case synHost.MatchString(joined):
				engine = engSynapse
			}
			if engine == "" || !cs.Engine(engine) {
				continue
			}
			for _, q := range costFlagValues(args, "-Q", "-q") {
				q = costUnquoted(q)
				if costSelectWordRe.MatchString(q) && !costTopRe.MatchString(q) {
					v.add(policy.Ask, "warehouse query through sqlcmd with no TOP: add SELECT TOP <n>, or estimate it first with tq pack data cost estimate")
				}
			}
		}
	}
	return v.result()
}

// databricksStatement judges a Statement Execution API body.
func databricksStatement(bodies []string, v *costVerdict) {
	const msg = "Databricks SQL statement without wait_timeout and on_wait_timeout CANCEL: add \"wait_timeout\":\"30s\",\"on_wait_timeout\":\"CANCEL\" to the body"
	if len(bodies) == 0 {
		v.add(policy.Ask, msg)
		return
	}
	for _, b := range bodies {
		if strings.HasPrefix(strings.TrimSpace(b), "@") {
			v.add(policy.Ask, "the statement body is a file the guard cannot see; "+msg)
			continue
		}
		var req struct {
			WaitTimeout   string `json:"wait_timeout"`
			OnWaitTimeout string `json:"on_wait_timeout"`
		}
		if err := json.Unmarshal([]byte(b), &req); err != nil {
			v.add(policy.Ask, msg)
			continue
		}
		wt := strings.TrimSpace(req.WaitTimeout)
		if wt == "" || wt == "0s" || !strings.EqualFold(req.OnWaitTimeout, "CANCEL") {
			v.add(policy.Ask, msg)
		}
	}
}
