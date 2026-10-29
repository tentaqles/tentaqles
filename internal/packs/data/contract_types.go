package data

// physicalType comparison for the breaking-change rules (spec C §3.2):
// narrowing is breaking, widening is not, and a change tq cannot prove to be
// a widening is breaking.

import (
	"regexp"
	"strconv"
	"strings"
)

type odcsPhys struct {
	raw    string
	base   string
	family string // int, decimal, float, string, temporal, temporal_tz, bool, "" (unknown)
	rank   int    // within int, float and temporal
	args   []int  // (n) or (p,s); nil = unbounded / default
}

var odcsPhysRe = regexp.MustCompile(`^([a-z_][a-z0-9_ ]*?)\s*(?:\(\s*([0-9]+)\s*(?:,\s*([0-9]+)\s*)?\))?$`)

var odcsPhysFamilies = map[string][2]any{
	"tinyint": {"int", 1}, "byteint": {"int", 1}, "int1": {"int", 1},
	"smallint": {"int", 2}, "int2": {"int", 2},
	"mediumint": {"int", 3}, "int": {"int", 3}, "integer": {"int", 3}, "int4": {"int", 3},
	"bigint": {"int", 4}, "int8": {"int", 4}, "int64": {"int", 4}, "long": {"int", 4},
	"decimal": {"decimal", 0}, "numeric": {"decimal", 0}, "number": {"decimal", 0}, "dec": {"decimal", 0}, "bignumeric": {"decimal", 0},
	"real": {"float", 1}, "float4": {"float", 1},
	"float": {"float", 2}, "float8": {"float", 2}, "float64": {"float", 2}, "double": {"float", 2}, "double precision": {"float", 2},
	"varchar": {"string", 0}, "character varying": {"string", 0}, "nvarchar": {"string", 0}, "varchar2": {"string", 0},
	"char": {"string", 0}, "character": {"string", 0}, "nchar": {"string", 0},
	"string": {"string", 0}, "text": {"string", 0},
	"date":      {"temporal", 1},
	"timestamp": {"temporal", 2}, "datetime": {"temporal", 2}, "timestamp_ntz": {"temporal", 2}, "datetime2": {"temporal", 2},
	"timestamptz": {"temporal_tz", 2}, "timestamp_tz": {"temporal_tz", 2}, "timestamp with time zone": {"temporal_tz", 2}, "timestamp_ltz": {"temporal_tz", 2}, "datetimeoffset": {"temporal_tz", 2},
	"boolean": {"bool", 0}, "bool": {"bool", 0}, "bit": {"bool", 0},
}

// odcsIntDigits is how many decimal digits each int rank needs.
var odcsIntDigits = map[int]int{1: 3, 2: 5, 3: 10, 4: 19}

func odcsParsePhys(s string) odcsPhys {
	raw := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
	p := odcsPhys{raw: raw}
	m := odcsPhysRe.FindStringSubmatch(raw)
	if m == nil {
		return p
	}
	base := strings.TrimSpace(m[1])
	f, ok := odcsPhysFamilies[base]
	if !ok {
		return p
	}
	p.base, p.family, p.rank = base, f[0].(string), f[1].(int)
	for _, a := range m[2:] {
		if a != "" {
			n, _ := strconv.Atoi(a)
			p.args = append(p.args, n)
		}
	}
	return p
}

// odcsPhysNarrowed reports whether changing physicalType old -> new can
// break a consumer, with the reason.
func odcsPhysNarrowed(oldT, newT string) (bool, string) {
	o, n := odcsParsePhys(oldT), odcsParsePhys(newT)
	if o.raw == n.raw {
		return false, ""
	}
	if o.family == "" || n.family == "" {
		return true, "changed (tq cannot prove it is a widening)"
	}
	if o.family != n.family {
		if o.family == "int" && n.family == "decimal" {
			if n.args == nil || len(n.args) == 1 && n.args[0] >= odcsIntDigits[o.rank] ||
				len(n.args) == 2 && n.args[0]-n.args[1] >= odcsIntDigits[o.rank] {
				return false, ""
			}
			return true, "narrowed (too few integer digits)"
		}
		if o.family == "temporal" && n.family == "temporal_tz" {
			return true, "changed (time zone semantics)"
		}
		return true, "changed type family"
	}
	switch o.family {
	case "int", "float", "temporal", "temporal_tz":
		if n.rank < o.rank {
			return true, "narrowed"
		}
		// fractional-second precision: timestamp(6) -> timestamp(3) drops digits
		if (o.family == "temporal" || o.family == "temporal_tz") && n.rank == o.rank {
			if op, np := odcsArg(o, 0), odcsArg(n, 0); op >= 0 && np >= 0 && np < op {
				return true, "narrowed (fractional-second precision)"
			}
		}
		return false, ""
	case "string":
		// no length = unbounded
		ol, nl := odcsArg(o, 0), odcsArg(n, 0)
		if nl >= 0 && (ol < 0 || nl < ol) {
			return true, "narrowed (shorter length)"
		}
		return false, ""
	case "decimal":
		if n.args == nil {
			return false, ""
		}
		if o.args == nil {
			return true, "narrowed (bounded precision)"
		}
		op, os := odcsArg(o, 0), odcsArg(o, 1)
		np, ns := odcsArg(n, 0), odcsArg(n, 1)
		if os < 0 {
			os = 0
		}
		if ns < 0 {
			ns = 0
		}
		if np < op || ns < os || np-ns < op-os {
			return true, "narrowed (precision or scale down)"
		}
		return false, ""
	case "bool":
		return false, ""
	}
	return true, "changed"
}

func odcsArg(p odcsPhys, i int) int {
	if i < len(p.args) {
		return p.args[i]
	}
	return -1
}

// contractSameType reports whether two physical type names mean the same
// type: the same text, or the same family, rank and arguments (int and
// integer, numeric and decimal). String types must also share the name.
func contractSameType(a, b string) bool {
	x, y := odcsParsePhys(a), odcsParsePhys(b)
	if x.raw == y.raw {
		return true
	}
	if x.family == "" || x.family != y.family || x.rank != y.rank || len(x.args) != len(y.args) {
		return false
	}
	if x.family == "string" && x.base != y.base {
		return false
	}
	for i := range x.args {
		if x.args[i] != y.args[i] {
			return false
		}
	}
	return true
}
