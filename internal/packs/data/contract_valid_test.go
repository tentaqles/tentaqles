package data

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func TestContractValidGolden(t *testing.T) {
	ctGolden(t, "contract-valid", runContractValid)
}

// An unchanged contract is not re-reported, but its id still counts for
// the duplicate check of a changed one.
func TestContractValidDuplicateAgainstUnchanged(t *testing.T) {
	in := ctInput(t, "testdata/contract-valid/duplicate-id")
	var keep []packs.ChangedFile
	for _, c := range in.Changed {
		if c.Path == "contracts/orders.odcs.yaml" {
			keep = append(keep, c)
		}
	}
	in.Changed = keep
	r := runContractValid(context.Background(), in)
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "contracts/orders.odcs.yaml" || r.Findings[0].Rule != "duplicate-contract-id" {
		t.Fatalf("%+v", r)
	}
}

// The plugin's contract template passes the built-in contract checks.
func TestContractTemplatePassesItsChecks(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin", "templates", "process", "data-contract.odcs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "new", "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new", "contracts", "orders.odcs.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := "manifest_data:\n  classes: [public, internal, pii]\n  paths:\n    - table: marts.fct_orders\n      classes: [pii]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	in := ctInput(t, dir)
	for name, run := range map[string]func(context.Context, *packs.Input) packs.Result{"valid": runContractValid, "pii": runContractPII} {
		r := run(context.Background(), in)
		if r.Status != packs.OK || len(r.Findings) != 0 {
			t.Errorf("%s: %+v", name, r)
		}
	}
}
