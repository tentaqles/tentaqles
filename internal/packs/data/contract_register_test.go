package data

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

func TestContractChecksRegistered(t *testing.T) {
	want := map[string]packs.Kind{
		"data/contract-valid": packs.Builtin, "data/contract-breaking": packs.Builtin, "data/contract-pii": packs.Builtin,
		"data/contract-dbt-sync": packs.Builtin, "data/datacontract-lint": packs.External, "data/datacontract-breaking": packs.External,
	}
	got := map[string]packs.Check{}
	for _, c := range packs.Checks("data") {
		got[c.ID] = c
	}
	for id, k := range want {
		c, ok := got[id]
		if !ok || c.Kind != k {
			t.Errorf("%s: registered=%v kind=%s", id, ok, c.Kind)
		}
		if k == packs.External && c.Tool != "datacontract" {
			t.Errorf("%s: tool %q", id, c.Tool)
		}
	}
	var flag *packs.Flag
	for _, f := range packs.Flags("data") {
		if f.Name == "data_contract" {
			f := f
			flag = &f
		}
	}
	if flag == nil || len(flag.Reviewers) != 1 || flag.Reviewers[0] != "pipeline-reviewer" || flag.Raise == nil {
		t.Fatalf("data_contract flag: %+v", flag)
	}
	in := ctInput(t, "testdata/contract-breaking/file-deleted")
	if !flag.Raise(in) {
		t.Error("deleting a contract must raise data_contract")
	}
	in.Changed = []packs.ChangedFile{{Path: "models/orders.sql", Status: 'M'}}
	if flag.Raise(in) {
		t.Error("a SQL change must not raise data_contract")
	}
	hint := false
	for _, h := range packs.Hints("data") {
		hint = hint || h.Glob == "*.odcs.yaml"
	}
	if !hint {
		t.Error("no *.odcs.yaml hint")
	}
}

// The framework runs the built-in contract checks end to end.
func TestContractChecksThroughRunChecks(t *testing.T) {
	in := ctInput(t, "testdata/contract-breaking/property-removed")
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}, BuiltinOnly: true})
	status := map[string]packs.Result{}
	for _, r := range rep.Results {
		status[r.ID] = r
	}
	for id, st := range map[string]packs.Status{"data/contract-valid": packs.OK, "data/contract-breaking": packs.Fail,
		"data/contract-pii": packs.OK, "data/contract-dbt-sync": packs.OK} {
		r, ok := status[id]
		if !ok || r.Status != st || !r.Required {
			t.Errorf("%s: %+v", id, r)
		}
	}
	if !rep.Failed() {
		t.Error("a breaking change without a bump must fail the run")
	}
}

func TestContractsConfigValidation(t *testing.T) {
	good := []string{
		"paths: [\"contracts/**/*.odcs.yaml\"]\nenum_added: breaking\nconsumers:\n  acme-orders: [acme-finance-dashboard]\n",
		"enum_added: warn\n",
	}
	bad := []string{
		"paths: contracts/\n",
		"enum_added: maybe\n",
		"consumers:\n  acme-orders: acme-finance-dashboard\n",
		"owners: [tester]\n",
		"[a, b]\n",
	}
	for _, s := range good {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(s), &n); err != nil {
			t.Fatal(err)
		}
		if err := validateContractsKey(*n.Content[0]); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range bad {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(s), &n); err != nil {
			t.Fatal(err)
		}
		if err := validateContractsKey(*n.Content[0]); err == nil {
			t.Errorf("%q: accepted", s)
		}
	}
	// Through the manifest validator registered by RegisterPack.
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte("data:\n  contracts:\n    enum_added: nope\n"), &m); err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidatePacks(m); err == nil || !strings.Contains(err.Error(), "enum_added") {
		t.Errorf("manifest validation: %v", err)
	}
}

// The repo layer may add contract paths and consumers, never change
// enum_added once the manifest set it.
func TestContractsMerge(t *testing.T) {
	var trusted, repo map[string]manifest.PackConfig
	_ = yaml.Unmarshal([]byte("data:\n  contracts:\n    paths: [\"contracts/**/*.odcs.yaml\"]\n    enum_added: breaking\n    consumers:\n      acme-orders: [acme-crm]\n"), &trusted)
	_ = yaml.Unmarshal([]byte("data:\n  contracts:\n    paths: [\"data/**/*.odcs.yaml\"]\n    enum_added: warn\n    consumers:\n      acme-orders: [acme-finance-dashboard]\n"), &repo)
	s := packs.Merge(trusted, repo).Pack("data")
	if p := contractPaths(s); len(p) != 2 {
		t.Errorf("paths %v", p)
	}
	if !contractEnumAddedBreaking(s) {
		t.Error("the repo file loosened enum_added")
	}
	if c := s.Strings("contracts.consumers.acme-orders"); len(c) != 2 {
		t.Errorf("consumers %v", c)
	}
}

func TestContractDoctor(t *testing.T) {
	in := ctInput(t, "testdata/contract-dbt-sync/in-sync")
	in.Config = ctConfigText(t, "dbt:\n  contract_paths: [\"models/**\"]\n")
	in = in.ForPack("data")
	if fs := contractDoctor(in); len(fs) != 0 {
		t.Fatalf("a contract exists: %v", fs)
	}
	g := in.Git.(ctFakeGit)
	g.files = []string{"dbt_project.yml", "models/marts/fct_orders.sql"}
	in.Git = g
	fs := contractDoctor(in)
	if len(fs) != 1 || fs[0].Code != "data-contracts-none" || fs[0].Level != "info" {
		t.Fatalf("%v", fs)
	}
}

func ctConfigText(t testing.TB, body string) packs.Config {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(body), &n); err != nil {
		t.Fatal(err)
	}
	return ctConfig(t, *n.Content[0])
}

// ctFakeCLI builds testdata/contract-fakecli as dir/datacontract(.exe) and
// prepends dir to PATH.
func ctFakeCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "datacontract"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./testdata/contract-fakecli")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake datacontract: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestDatacontractTool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	ctFakeCLI(t)
	tqHome := ""
	setup := func(t *testing.T, c, cfg string) *packs.Input {
		in := ctInput(t, "testdata/contract-breaking/"+c)
		tqHome = os.Getenv("TQ_HOME")
		in.Config = ctConfigText(t, "tools: [datacontract]\n"+cfg)
		in = in.ForPack("data")
		in.Tools = packs.NewToolRunner(in.Root, in.Config)
		log := filepath.Join(t.TempDir(), "argv.log")
		t.Setenv("TQ_FAKE_DC_LOG", log)
		t.Setenv("TQ_FAKE_DC_EXIT", "0")
		t.Setenv("TQ_FAKE_DC_FAIL_ON", "")
		t.Setenv("TQ_FAKE_DC_SLEEP", "")
		return in
	}
	result := func(rep packs.Report, id string) packs.Result {
		for _, r := range rep.Results {
			if r.ID == id {
				return r
			}
		}
		t.Fatalf("no result %s in %+v", id, rep.Results)
		return packs.Result{}
	}
	t.Run("lint and breaking pass", func(t *testing.T) {
		in := setup(t, "non-breaking", "")
		rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"data"}})
		for _, id := range []string{"data/datacontract-lint", "data/datacontract-breaking"} {
			r := result(rep, id)
			if r.Status != packs.OK || !r.Required || r.Tool == nil || r.Tool.Version != "0.11.2" {
				t.Errorf("%s: %+v %+v", id, r, r.Tool)
			}
		}
		b, _ := os.ReadFile(os.Getenv("TQ_FAKE_DC_LOG"))
		log := string(b)
		if !strings.Contains(log, "lint --no-inline-references ") || !strings.Contains(log, "breaking --no-inline-references ") {
			t.Fatalf("argv: %s", log)
		}
		if !strings.Contains(log, filepath.Join(tqHome, "packs", "acme", "scratch")) {
			t.Errorf("tool did not read scratch copies: %s", log)
		}
		if ents, _ := os.ReadDir(filepath.Join(tqHome, "packs", "acme", "scratch")); len(ents) != 0 {
			t.Errorf("scratch not cleaned: %d entries", len(ents))
		}
	})
	t.Run("lint fails", func(t *testing.T) {
		in := setup(t, "non-breaking", "")
		t.Setenv("TQ_FAKE_DC_EXIT", "1")
		t.Setenv("TQ_FAKE_DC_OUT", "contract has errors\n")
		r := runDatacontractLint(context.Background(), in)
		if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "contracts/orders.odcs.yaml" || !strings.Contains(r.Detail, "contract has errors") {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("breaking without bump is an error, with bump a warn", func(t *testing.T) {
		in := setup(t, "enum-value-removed", "")
		t.Setenv("TQ_FAKE_DC_FAIL_ON", "orders.odcs.yaml")
		r := runDatacontractBreaking(context.Background(), in)
		if r.Status != packs.Fail || r.Findings[0].Level != packs.LevelError {
			t.Fatalf("%+v", r)
		}
		in = setup(t, "major-bumped", "")
		t.Setenv("TQ_FAKE_DC_FAIL_ON", "orders.odcs.yaml")
		r = runDatacontractBreaking(context.Background(), in)
		if r.Status != packs.OK || r.Findings[0].Level != packs.LevelWarn {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		in := setup(t, "non-breaking", "timeouts: {datacontract: 1s}\n")
		t.Setenv("TQ_FAKE_DC_SLEEP", "20s")
		r := runDatacontractLint(context.Background(), in)
		if r.Status != packs.Fail || r.Tool == nil || !r.Tool.TimedOut {
			t.Fatalf("%+v %+v", r, r.Tool)
		}
	})
	t.Run("added contract has nothing to compare", func(t *testing.T) {
		in := setup(t, "non-breaking", "")
		in.Changed = []packs.ChangedFile{{Path: "contracts/orders.odcs.yaml", Status: 'A'}}
		if r := runDatacontractBreaking(context.Background(), in); r.Status != packs.Skip {
			t.Fatalf("%+v", r)
		}
	})
}
