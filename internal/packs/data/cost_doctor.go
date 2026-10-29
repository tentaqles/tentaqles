package data

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// costDoctor reports data-cost-cap-missing: an engine in engines with no cap
// in cost (spec C §7).
func costDoctor(in *packs.Input) []doctor.Finding {
	cs := costOf(in.Config.Pack("data"))
	var out []doctor.Finding
	for _, e := range costEngines {
		if !cs.Engine(e) {
			continue
		}
		has := false
		for _, k := range costCapKeys[e] {
			if cs.Raw(e, k) != "" {
				has = true
			}
		}
		if !has {
			out = append(out, doctor.Finding{
				Level: "warn", Code: "data-cost-cap-missing",
				Msg: "engine " + e + " is in packs.data.engines but has no cost cap",
				Fix: "add packs.data.cost." + e + "." + strings.Join(costCapKeys[e], " or ") + " to the manifest (a human edit; see docs/packs/data-cost.md)",
			})
		}
	}
	return out
}

// Triage flag warehouse_cost (spec C §5.4).
const flagWarehouseCost = "warehouse_cost"

const warehouseCostQuestion = "Does this change add or alter warehouse queries, models or compute sizing so that it could scan much more data or cost more to run?"

// costSizingGlobs are compute sizing files: Databricks bundles, Synapse
// Spark and SQL pool definitions.
var costSizingGlobs = []string{"databricks.yml", "databricks.yaml", "bigDataPool/*.json", "**/bigDataPool/*.json", "sqlPool/*.json", "**/sqlPool/*.json"}

var costMaterializedTableRe = regexp.MustCompile(`(?i)materialized\s*[=:]\s*['"]?table\b`)

// costRaiseLimit bounds the files the flag reads.
const costRaiseLimit = 200

// raiseWarehouseCost raises the flag when packs.data.cost changes in
// tq-packs.yaml, a model's materialization changes to table, or a compute
// sizing file changes.
func raiseWarehouseCost(in *packs.Input) bool {
	if in.AnyChanged(costSizingGlobs...) {
		return true
	}
	read := 0
	for _, p := range in.ChangedPaths() {
		if read >= costRaiseLimit {
			break
		}
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		switch {
		case base == "tq-packs.yaml" && p == base:
			read++
			if costNodeChanged(in, p) {
				return true
			}
		case strings.HasSuffix(p, ".sql") || base == "dbt_project.yml" || ((strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) && strings.Contains(p, "models/")):
			read++
			nw, err := in.ReadNew(p, 256<<10)
			if err != nil || !costMaterializedTableRe.Match(nw) {
				continue
			}
			old, err := in.ReadOld(p)
			if err != nil || costMoreTables(old, nw) {
				return true
			}
		}
	}
	return false
}

// costMoreTables reports whether nw has more table materializations than
// old (one more folder or model switched to table in the same file).
func costMoreTables(old, nw []byte) bool {
	return len(costMaterializedTableRe.FindAllIndex(nw, -1)) > len(costMaterializedTableRe.FindAllIndex(old, -1))
}

// costNodeChanged compares the data.cost subtree of both sides of tq-packs.yaml.
func costNodeChanged(in *packs.Input, p string) bool {
	side := func(b []byte, err error) string {
		if err != nil {
			return ""
		}
		n, perr := scan.ParseYAML(b)
		if perr != nil || n == nil {
			return "\x00unparsed"
		}
		c := scan.Get(n, "packs", "data", "cost")
		if c == nil {
			c = scan.Get(n, "data", "cost")
		}
		if c == nil {
			return ""
		}
		out, merr := yaml.Marshal(c)
		if merr != nil {
			return "\x00unmarshalled"
		}
		return string(out)
	}
	return side(in.ReadNew(p, 1<<20)) != side(in.ReadOld(p))
}
