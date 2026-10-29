package data

import (
	"context"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestContractDBTSyncGolden(t *testing.T) {
	ctGolden(t, "contract-dbt-sync", runContractDBTSync)
}

// A change to a dbt YAML alone re-checks every contract that names a model.
func TestContractDBTSyncRunsOnDBTChangeOnly(t *testing.T) {
	in := ctInput(t, "testdata/contract-dbt-sync/type-mismatch")
	in.Changed = []packs.ChangedFile{{Path: "models/marts/orders.yml", Status: 'M'}}
	if !contractDBTApplies(in) {
		t.Fatal("a changed dbt YAML must apply")
	}
	r := runContractDBTSync(context.Background(), in)
	if r.Status != packs.Fail {
		t.Fatalf("%+v", r)
	}
	in.Changed = []packs.ChangedFile{{Path: "README.md", Status: 'M'}}
	if contractDBTApplies(in) {
		t.Fatal("a README change must not apply")
	}
}

func TestSameDataType(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"bigint", "BIGINT", true}, {"int", "integer", true}, {"numeric(12, 2)", "decimal(12,2)", true},
		{"varchar(20)", "varchar(21)", false}, {"int", "bigint", false}, {"geography", "Geography", true},
		{"text", "string", false},
	} {
		if got := contractSameType(c.a, c.b); got != c.same {
			t.Errorf("%s vs %s: %v", c.a, c.b, got)
		}
	}
}
