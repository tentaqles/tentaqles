package data

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// The expected verdict and rules of the cases added for review r0 (the
// goldens pin the full output; this pins the intent).
func TestContractBreakingReviewCases(t *testing.T) {
	cases := []struct {
		dir     string
		status  packs.Status
		want    []string // rules that must appear
		notWant []string // rules that must not
	}{
		{"quality-anonymous-unchanged", packs.OK, nil, []string{"quality-removed", "quality-loosened", "version-not-bumped"}},
		{"quality-anonymous-loosened", packs.Fail, []string{"quality-loosened"}, []string{"quality-removed"}},
		{"quality-anonymous-removed", packs.Fail, []string{"quality-removed"}, nil},
		{"quality-query-changed", packs.Fail, []string{"quality-loosened"}, []string{"quality-removed"}},
		{"type-options-unique-items", packs.Fail, []string{"type-options-loosened"}, nil},
		{"sla-percent-loosened", packs.Fail, []string{"sla-loosened"}, []string{"sla-changed"}},
		{"retired-mixed-case", packs.Fail, []string{"retired-with-consumers"}, nil},
		{"contract-id-changed", packs.Fail, []string{"contract-id-changed", "property-removed"}, nil},
		{"version-decreased", packs.Fail, []string{"version-decreased"}, nil},
	}
	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			r := runContractBreaking(context.Background(), ctInput(t, "testdata/contract-breaking/"+c.dir))
			rules := map[string]bool{}
			for _, f := range r.Findings {
				rules[f.Rule] = true
				if strings.Contains(f.Message, "SELECT") || strings.Contains(f.Message, "amount <") {
					t.Errorf("finding quotes query text: %s", f.Message)
				}
			}
			if r.Status != c.status {
				t.Errorf("status %s, want %s: %+v", r.Status, c.status, r.Findings)
			}
			for _, w := range c.want {
				if !rules[w] {
					t.Errorf("missing %s: %+v", w, r.Findings)
				}
			}
			for _, w := range c.notWant {
				if rules[w] {
					t.Errorf("unexpected %s: %+v", w, r.Findings)
				}
			}
		})
	}
	// The consumers listed under the old id survive an id change.
	r := runContractBreaking(context.Background(), ctInput(t, "testdata/contract-breaking/contract-id-changed"))
	found := false
	for _, f := range r.Findings {
		if f.Rule == "version-not-bumped" && strings.Contains(f.Message, "acme-finance-dashboard") {
			found = true
		}
	}
	if !found {
		t.Errorf("consumers under the old id lost: %+v", r.Findings)
	}
}

// ctErrGit fails every old-side read (a git timeout, a bad ref).
type ctErrGit struct{ ctFakeGit }

func (g ctErrGit) Show(ref, p string) ([]byte, error) {
	if ref == ":" {
		return g.ctFakeGit.Show(ref, p)
	}
	return nil, errors.New("git show: signal: killed")
}

type ctBadYAMLGit struct{ ctFakeGit }

func (g ctBadYAMLGit) Show(ref, p string) ([]byte, error) {
	if ref == ":" {
		return g.ctFakeGit.Show(ref, p)
	}
	return []byte("a: [unclosed\n"), nil
}

// M1: a modified, renamed or deleted contract exists at Base, so a failed
// old-side read fails the check instead of passing it.
func TestContractBreakingOldSideUnreadable(t *testing.T) {
	for _, dir := range []string{"property-removed", "file-deleted"} {
		in := ctInput(t, "testdata/contract-breaking/"+dir)
		in.Git = ctErrGit{in.Git.(ctFakeGit)}
		r := runContractBreaking(context.Background(), in)
		if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].Rule != "contract-old-unreadable" || r.Findings[0].Level != packs.LevelError {
			t.Fatalf("%s: %+v", dir, r)
		}
		if strings.Contains(r.Findings[0].Message, "killed") {
			t.Errorf("%s: the git error text leaked: %s", dir, r.Findings[0].Message)
		}
	}
	// No GitReader at all, with a base: the same.
	in := ctInput(t, "testdata/contract-breaking/property-removed")
	in.Git = nil
	if r := runContractBreaking(context.Background(), in); r.Status != packs.Fail {
		t.Errorf("nil Git: %+v", r)
	}
	// An old side that is not YAML was not a contract: skipped, not failed.
	in = ctInput(t, "testdata/contract-breaking/property-removed")
	in.Git = ctBadYAMLGit{in.Git.(ctFakeGit)}
	if r := runContractBreaking(context.Background(), in); r.Status != packs.OK {
		t.Errorf("unparsable old side: %+v", r)
	}
}

// A rename compares the new path with the old path's base side.
func TestContractBreakingRename(t *testing.T) {
	in := ctInput(t, "testdata/contract-breaking/property-removed")
	b, err := os.ReadFile(filepath.Join(in.Root, "contracts", "orders.odcs.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "contracts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "contracts", "orders_v2.odcs.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	in.Root = root
	in.Changed = []packs.ChangedFile{{Path: "contracts/orders_v2.odcs.yaml", OldPath: "contracts/orders.odcs.yaml", Status: 'R'}}
	r := runContractBreaking(context.Background(), in)
	if r.Status != packs.Fail {
		t.Fatalf("%+v", r)
	}
	got := map[string]string{}
	for _, f := range r.Findings {
		got[f.Rule] = f.File
	}
	if got["property-removed"] != "contracts/orders_v2.odcs.yaml" || got["version-not-bumped"] == "" {
		t.Fatalf("%+v", r.Findings)
	}
}

// The doctor probe reads dbt.contract_paths relative to the dbt project.
func TestContractDoctorProjectSubdir(t *testing.T) {
	in := ctInput(t, "testdata/contract-dbt-sync/in-sync")
	in.Config = ctConfigText(t, "dbt:\n  contract_paths: [\"models/marts/**\"]\n")
	in = in.ForPack("data")
	g := in.Git.(ctFakeGit)
	g.files = []string{"analytics/dbt_project.yml", "analytics/models/marts/fct_orders.sql", "analytics/models/staging/stg_orders.sql"}
	in.Git = g
	fs := contractDoctor(in)
	if len(fs) != 1 || fs[0].Code != "data-contracts-none" || !strings.HasPrefix(fs[0].Msg, "1 model ") {
		t.Fatalf("auto-detected project: %v", fs)
	}
	in.Config = ctConfigText(t, "dbt:\n  project: analytics\n  contract_paths: [\"models/marts/**\"]\n")
	in = in.ForPack("data")
	if fs := contractDoctor(in); len(fs) != 1 {
		t.Fatalf("dbt.project set: %v", fs)
	}
}
