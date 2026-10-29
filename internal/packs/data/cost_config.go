package data

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Warehouse engines of packs.data.engines (spec C §3.1). The cost rules of an
// engine apply only when it is listed.
const (
	engBigQuery   = "bigquery"
	engSnowflake  = "snowflake"
	engDatabricks = "databricks"
	engPostgres   = "postgres"
	engFabric     = "fabric"
	engSynapse    = "synapse"
)

// costEngines lists the known engines in a stable order.
var costEngines = []string{engBigQuery, engSnowflake, engDatabricks, engPostgres, engFabric, engSynapse}

// costCapKind is how a cost key's value is read.
type costCapKind int

const (
	costCapBytes  costCapKind = iota // 50GB, 512MiB, 1000000
	costCapInt                       // a positive integer
	costCapNumber                    // a positive number
	costCapSnowflakeSize
	costCapSynapseDWU
	costCapFabricSKU
	costCapID // an id, not a secret
)

// costKeys are the keys of packs.data.cost.<engine>.
var costKeys = map[string]map[string]costCapKind{
	engBigQuery:   {"max_bytes": costCapBytes},
	engSnowflake:  {"max_warehouse_size": costCapSnowflakeSize, "statement_timeout_s": costCapInt, "max_bytes": costCapBytes},
	engDatabricks: {"max_workers": costCapInt, "warehouse_id": costCapID, "max_bytes": costCapBytes},
	engFabric:     {"max_capacity_sku": costCapFabricSKU},
	engSynapse:    {"max_dwu": costCapSynapseDWU, "max_spark_nodes": costCapInt},
	engPostgres:   {"max_plan_cost": costCapNumber, "statement_timeout_s": costCapInt},
}

// costCapKeys are the keys that bound cost (data-cost-cap-missing looks for one).
var costCapKeys = map[string][]string{
	engBigQuery:   {"max_bytes"},
	engSnowflake:  {"max_warehouse_size", "statement_timeout_s", "max_bytes"},
	engDatabricks: {"max_workers", "max_bytes"},
	engFabric:     {"max_capacity_sku"},
	engSynapse:    {"max_dwu", "max_spark_nodes"},
	engPostgres:   {"max_plan_cost", "statement_timeout_s"},
}

func costKnownEngine(e string) bool {
	for _, x := range costEngines {
		if x == e {
			return true
		}
	}
	return false
}

// validateEngines checks packs.data.engines.
func validateEngines(n yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: engines must be a list (%s)", n.Line, strings.Join(costEngines, ", "))
	}
	for _, it := range n.Content {
		if it.Kind != yaml.ScalarNode || !costKnownEngine(it.Value) {
			return fmt.Errorf("line %d: unknown engine %q (known: %s)", it.Line, it.Value, strings.Join(costEngines, ", "))
		}
	}
	return nil
}

// validateCost checks packs.data.cost.
func validateCost(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: cost must be a mapping of engines", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value == "estimate_in_dod" {
			if v.Kind != yaml.ScalarNode || (v.Value != "true" && v.Value != "false") {
				return fmt.Errorf("line %d: cost.estimate_in_dod must be true or false", v.Line)
			}
			continue
		}
		keys, ok := costKeys[k.Value]
		if !ok {
			return fmt.Errorf("line %d: unknown cost key %q (known: %s, estimate_in_dod)", k.Line, k.Value, strings.Join(costEngines, ", "))
		}
		if v.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: cost.%s must be a mapping", v.Line, k.Value)
		}
		for j := 0; j+1 < len(v.Content); j += 2 {
			ck, cv := v.Content[j], v.Content[j+1]
			kind, ok := keys[ck.Value]
			if !ok {
				names := make([]string, 0, len(keys))
				for name := range keys {
					names = append(names, name)
				}
				sort.Strings(names)
				return fmt.Errorf("line %d: unknown key cost.%s.%s (known: %s)", ck.Line, k.Value, ck.Value, strings.Join(names, ", "))
			}
			if cv.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: cost.%s.%s must be a scalar", cv.Line, k.Value, ck.Value)
			}
			if _, err := costParseCap(kind, cv.Value); err != nil {
				return fmt.Errorf("line %d: cost.%s.%s: %v", cv.Line, k.Value, ck.Value, err)
			}
		}
	}
	return nil
}

var (
	costBytesRe = regexp.MustCompile(`^(?i)\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgtpe]i?b|b)?\s*$`)
	costIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// costParseBytes reads 50GB (decimal), 512MiB (binary) or a plain byte count.
func costParseBytes(s string) (float64, error) {
	m := costBytesRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%q is not a byte size (for example 50GB or 512MiB)", s)
	}
	f, _ := strconv.ParseFloat(m[1], 64)
	if u := strings.ToLower(m[2]); u != "" && u != "b" {
		base := 1000.0
		if strings.Contains(u, "i") {
			base = 1024
		}
		f *= math.Pow(base, float64(strings.IndexByte("bkmgtpe", u[0])))
	}
	return f, nil
}

// costParseCap reads a cap value of the given kind into a comparable number.
func costParseCap(kind costCapKind, s string) (float64, error) {
	s = strings.TrimSpace(s)
	switch kind {
	case costCapBytes:
		return costParseBytes(s)
	case costCapInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%q is not a positive integer", s)
		}
		return float64(n), nil
	case costCapNumber:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
			return 0, fmt.Errorf("%q is not a positive number", s)
		}
		return f, nil
	case costCapSnowflakeSize:
		if r, ok := snowflakeRank(s); ok {
			return float64(r), nil
		}
		return 0, fmt.Errorf("%q is not a Snowflake warehouse size (XSMALL … X6LARGE)", s)
	case costCapSynapseDWU:
		if r, ok := synapseDWU(s); ok {
			return float64(r), nil
		}
		return 0, fmt.Errorf("%q is not a Synapse performance level (DW100c … DW30000c)", s)
	case costCapFabricSKU:
		if r, ok := fabricSKU(s); ok {
			return float64(r), nil
		}
		return 0, fmt.Errorf("%q is not a Fabric capacity SKU (F2 … F2048)", s)
	case costCapID:
		if !costIDRe.MatchString(s) {
			return 0, fmt.Errorf("%q is not an id", s)
		}
		return 0, nil
	}
	return 0, fmt.Errorf("unknown cap kind")
}

// snowflakeSizes is Snowflake's ordered warehouse size scale, keyed by the
// normalised spelling (upper case, no '-', '_' or quotes).
var snowflakeSizes = map[string]int{
	"XSMALL": 1, "SMALL": 2, "MEDIUM": 3, "LARGE": 4, "XLARGE": 5,
	"XXLARGE": 6, "X2LARGE": 6, "2XLARGE": 6,
	"XXXLARGE": 7, "X3LARGE": 7, "3XLARGE": 7,
	"X4LARGE": 8, "4XLARGE": 8, "X5LARGE": 9, "5XLARGE": 9, "X6LARGE": 10, "6XLARGE": 10,
}

func costNormSize(s string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", "_", "", "'", "", `"`, "", `\`, "", " ", "").Replace(s))
}

func snowflakeRank(s string) (int, bool) {
	r, ok := snowflakeSizes[costNormSize(s)]
	return r, ok
}

var synapseLevels = map[int]bool{100: true, 200: true, 300: true, 400: true, 500: true, 1000: true, 1500: true, 2000: true,
	2500: true, 3000: true, 5000: true, 6000: true, 7500: true, 10000: true, 15000: true, 30000: true}

var (
	costDWURe = regexp.MustCompile(`^DW([0-9]+)C$`)
	costSKURe = regexp.MustCompile(`^F([0-9]+)$`)
)

// synapseDWU reads a dedicated SQL pool performance level (Gen2, DW…c).
func synapseDWU(s string) (int, bool) {
	m := costDWURe.FindStringSubmatch(costNormSize(s))
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, synapseLevels[n]
}

// fabricSKU reads a Fabric capacity SKU (F2 … F2048, powers of two).
func fabricSKU(s string) (int, bool) {
	m := costSKURe.FindStringSubmatch(costNormSize(s))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 2 || n > 2048 || n&(n-1) != 0 {
		return 0, false
	}
	return n, true
}

// costSettings is the engine view of packs.data used by the guards, the
// checks and the estimate.
type costSettings struct{ s packs.Settings }

func costOf(s packs.Settings) costSettings { return costSettings{s} }

// Engine reports whether engine is listed in packs.data.engines.
func (c costSettings) Engine(engine string) bool {
	for _, e := range c.s.Strings("engines") {
		if e == engine {
			return true
		}
	}
	return false
}

// Raw returns the configured cap text ("" when unset).
func (c costSettings) Raw(engine, key string) string {
	return c.s.String("cost." + engine + "." + key)
}

// Cap returns a cap as a number; ok is false when it is unset or unreadable.
func (c costSettings) Cap(engine, key string) (float64, bool) {
	raw := c.Raw(engine, key)
	if raw == "" {
		return 0, false
	}
	kind, known := costKeys[engine][key]
	if !known {
		return 0, false
	}
	v, err := costParseCap(kind, raw)
	return v, err == nil
}

// EstimateInDoD reports packs.data.cost.estimate_in_dod.
func (c costSettings) EstimateInDoD() bool { return c.s.String("cost.estimate_in_dod") == "true" }

// costHumanBytes prints a byte count with a decimal unit.
func costHumanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	i := 0
	for b >= 1000 && i < len(units)-1 {
		b /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f B", b)
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}
