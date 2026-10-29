package data

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// One case per row of the spec C §3.2 breaking-change table.
func TestContractBreakingGolden(t *testing.T) {
	ctGolden(t, "contract-breaking", runContractBreaking)
}

// Every row of the table has a golden case, so a row can't silently lose
// its coverage.
func TestContractBreakingCoversEveryRow(t *testing.T) {
	rows := map[string]string{
		"contract file deleted":              "file-deleted",
		"object removed":                     "object-removed",
		"object renamed":                     "object-renamed",
		"property removed":                   "property-removed",
		"property renamed":                   "property-renamed",
		"logicalType changed":                "logical-type-changed",
		"logicalType widenings":              "logical-type-widened",
		"physicalType narrowed":              "physical-type-narrowed",
		"physicalType widened":               "physical-type-widened",
		"required true -> false":             "required-loosened",
		"unique true -> false":               "unique-loosened",
		"primaryKey reordered":               "primary-key-reordered",
		"logicalTypeOptions loosened":        "type-options-loosened",
		"enum value removed":                 "enum-value-removed",
		"enum value added (warn)":            "enum-value-added",
		"enum value added (breaking)":        "enum-value-added-breaking",
		"quality rule removed":               "quality-removed",
		"quality threshold loosened":         "quality-loosened",
		"SLA loosened":                       "sla-loosened",
		"server removed":                     "server-removed",
		"server location changed":            "server-changed",
		"partitioned changed (warn)":         "partitioning-changed",
		"non-breaking changes":               "non-breaking",
		"classification changed":             "classification-changed",
		"status retired with consumers":      "retired-with-consumers",
		"major bump with no breaking (warn)": "major-without-breaking",
	}
	for row, c := range rows {
		in := ctInput(t, "testdata/contract-breaking/"+c)
		if len(in.Changed) == 0 {
			t.Errorf("row %q: case %s changes nothing", row, c)
		}
	}
}

func TestContractBreakingSkipsWithoutOldSide(t *testing.T) {
	in := ctInput(t, "testdata/contract-breaking/property-removed")
	in.All, in.Base = true, ""
	if r := runContractBreaking(context.Background(), in); r.Status != packs.Skip {
		t.Fatalf("status %s, want skip", r.Status)
	}
}

func TestContractBreakingFindingsCarryNoEnumValues(t *testing.T) {
	in := ctInput(t, "testdata/contract-breaking/enum-value-removed")
	r := runContractBreaking(context.Background(), in)
	for _, f := range r.Findings {
		if strings.Contains(f.Message, "delivered") {
			t.Fatalf("finding quotes a data value: %s", f)
		}
	}
}

func TestPhysNarrowed(t *testing.T) {
	cases := []struct {
		old, new string
		bad      bool
	}{
		{"varchar(20)", "varchar(10)", true},
		{"varchar(20)", "VARCHAR(40)", false},
		{"varchar(20)", "text", false},
		{"text", "varchar(200)", true},
		{"bigint", "int", true},
		{"int", "bigint", false},
		{"int4", "int8", false},
		{"decimal(12,2)", "decimal(10,2)", true},
		{"decimal(12,2)", "decimal(12,1)", true},
		{"decimal(12,2)", "decimal(12,4)", true}, // integer digits 10 -> 8
		{"decimal(12,2)", "numeric(14,3)", false},
		{"numeric", "numeric(10,2)", true},
		{"int", "decimal(12,0)", false},
		{"bigint", "decimal(12,0)", true},
		{"date", "timestamp", false},
		{"timestamp", "date", true},
		{"timestamp", "timestamptz", true},
		{"double", "real", true},
		{"varchar(10)", "int", true},
		{"geography", "geometry", true},
		{"geography", "GEOGRAPHY", false},
		{"char(2)", "varchar(2)", false},
	}
	for _, c := range cases {
		if bad, why := odcsPhysNarrowed(c.old, c.new); bad != c.bad {
			t.Errorf("%s -> %s: narrowed=%v (%s), want %v", c.old, c.new, bad, why, c.bad)
		}
	}
}

func TestOdcsMatchByIDThenName(t *testing.T) {
	type el struct{ id, name string }
	key := func(e el) (string, string) { return e.id, e.name }
	olds := []el{{"a", "x"}, {"", "y"}, {"c", "z"}}
	news := []el{{"a", "x2"}, {"b", "y"}, {"", "w"}}
	pairs, removed, added := odcsMatch(olds, news, key)
	if len(pairs) != 2 || pairs[0][1].name != "x2" || pairs[1][1].id != "b" {
		t.Fatalf("pairs %v", pairs)
	}
	if len(removed) != 1 || removed[0].id != "c" || len(added) != 1 || added[0].name != "w" {
		t.Fatalf("removed %v added %v", removed, added)
	}
}

func TestSemverAndVersionRule(t *testing.T) {
	if M, m, p, ok := odcsSemver("v2.10.3"); !ok || M != 2 || m != 10 || p != 3 {
		t.Fatal("semver")
	}
	if _, _, _, ok := odcsSemver("2024-01"); ok {
		t.Fatal("not semver")
	}
	old := &odcsContract{Version: "1.0.0"}
	nw := &odcsContract{Version: "next", VersionLine: 5}
	fs, broke := applyVersionRule("c.odcs.yaml", old, nw, []odcsChange{{Line: 9, Rule: "property-removed", Breaking: true, Message: "x"}}, nil)
	if !broke || fs[len(fs)-1].Rule != "version-not-semver" || fs[len(fs)-1].Level != packs.LevelError {
		t.Fatalf("%v", fs)
	}
	nw.Version = "0.9.0"
	fs, _ = applyVersionRule("c.odcs.yaml", old, nw, nil, nil)
	if len(fs) != 1 || fs[0].Rule != "version-decreased" {
		t.Fatalf("%v", fs)
	}
}
