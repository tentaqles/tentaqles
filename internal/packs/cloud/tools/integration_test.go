package tools_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/baseline"
	_ "github.com/tentaqles/tentaqles/internal/packs/cloud" // registers the cloud pack
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/policy"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"gopkg.in/yaml.v3"
)

// TestMain doubles as every fake IaC tool: a copy of the test binary named
// checkov(.exe), trivy, conftest, tflint, bicep or cfn-lint, run with
// TQ_FAKE_CLOUD=1, prints the fixture named by TQ_FAKE_OUT_<TOOL>, exits
// with TQ_FAKE_EXIT_<TOOL>, and appends its argv to TQ_FAKE_ARGV_DIR.
func TestMain(m *testing.M) {
	if os.Getenv("TQ_FAKE_CLOUD") == "1" {
		fakeTool()
		return
	}
	os.Exit(m.Run())
}

func envKey(tool string) string {
	return strings.ToUpper(strings.ReplaceAll(tool, "-", "_"))
}

func fakeTool() {
	tool := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	k := envKey(tool)
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("%s version %s\n", tool, os.Getenv("TQ_FAKE_VERSION_"+k))
		os.Exit(0)
	}
	if d := os.Getenv("TQ_FAKE_ARGV_DIR"); d != "" {
		f, _ := os.OpenFile(filepath.Join(d, tool+".argv"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, strings.Join(args, "\x1f"))
		f.Close()
		if tool == "conftest" {
			// Keep what conftest was given: its inputs and the bundle listing.
			for i, a := range args {
				if strings.HasSuffix(a, ".json") && !strings.HasPrefix(a, "-") && (i == 0 || args[i-1] != "--data") {
					b, _ := os.ReadFile(a)
					_ = os.WriteFile(filepath.Join(d, "seen-"+filepath.Base(a)), b, 0o600)
				}
				if a == "--policy" && i+1 < len(args) {
					ents, _ := os.ReadDir(args[i+1])
					var names []string
					for _, e := range ents {
						names = append(names, e.Name())
					}
					_ = os.WriteFile(filepath.Join(d, "policy-"+filepath.Base(args[i+1])), []byte(strings.Join(names, ",")), 0o600)
				}
				if a == "--data" && i+1 < len(args) {
					b, _ := os.ReadFile(filepath.Join(args[i+1], "tqconfig.json"))
					_ = os.WriteFile(filepath.Join(d, "data.json"), b, 0o600)
				}
			}
		}
	}
	if os.Getenv("TQ_FAKE_HANG_"+k) == "1" {
		time.Sleep(60 * time.Second)
	}
	if p := os.Getenv("TQ_FAKE_OUT_" + k); p != "" {
		b, _ := os.ReadFile(p)
		os.Stdout.Write(b)
	}
	code := 0
	fmt.Sscan(os.Getenv("TQ_FAKE_EXIT_"+k), &code)
	os.Exit(code)
}

func fakeBin(t *testing.T, dir, name string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
}

func lookOnly(dir string) func(string) (string, error) {
	return func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			p += ".exe"
		}
		if _, err := os.Stat(p); err != nil {
			return "", toolrun.ErrNotFound
		}
		return p, nil
	}
}

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

type fakeGit struct{ files []string }

func (g fakeGit) Show(ref, p string) ([]byte, error) { return nil, os.ErrNotExist }
func (g fakeGit) ListFiles() ([]string, error)       { return g.files, nil }

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testdata(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const cloudConfig = `
cloud:
  enabled: true
  tools: %s
  iac:
    - {path: infra/terraform, kind: terraform}
    - {path: infra/cfn, kind: cloudformation}
    - {path: infra/bicep, kind: bicep}
  conftest: {policy: [policy/]}
  tags: [owner]
%s`

func packsOf(t *testing.T, body string) map[string]manifest.PackConfig {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func repo(t *testing.T) (string, []packs.ChangedFile, []string) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "infra/terraform/main.tf", "resource \"aws_s3_bucket\" \"acme\" {}\n")
	write(t, root, "infra/cfn/template.yaml", testdata(t, "../policy/testdata/template.yaml"))
	write(t, root, "infra/bicep/main.bicep", "param unusedParam string\n")
	write(t, root, "policy/acme.rego", "package main\n")
	changed := []packs.ChangedFile{
		{Path: "infra/terraform/main.tf", Status: 'M'},
		{Path: "infra/cfn/template.yaml", Status: 'M'},
		{Path: "infra/bicep/main.bicep", Status: 'A'},
	}
	tracked := []string{"infra/terraform/main.tf", "infra/cfn/template.yaml", "policy/acme.rego"}
	return root, changed, tracked
}

func input(t *testing.T, tools, extra, root string, changed []packs.ChangedFile, tracked []string) *packs.Input {
	t.Helper()
	cfg := packs.Merge(packsOf(t, fmt.Sprintf(cloudConfig, tools, extra)), nil)
	in := &packs.Input{Root: root, Base: "HEAD", Changed: changed, Config: cfg, Git: fakeGit{tracked}, Budget: time.Now().Add(2 * time.Minute)}
	in.Tools = packs.NewToolRunner(root, cfg)
	in.SetWorkspace("acme", root)
	return in
}

func byID(rep packs.Report) map[string]packs.Result {
	m := map[string]packs.Result{}
	for _, r := range rep.Results {
		m[r.ID] = r
	}
	return m
}

func findings(r packs.Result) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, fmt.Sprintf("%s:%d %s %s", f.File, f.Line, f.Level, f.Rule))
	}
	sort.Strings(out)
	return out
}

// withPlan installs a plan store hook returning one redacted prod plan.
func withPlan(t *testing.T) policy.Plan {
	t.Helper()
	raw := []byte(testdata(t, "../policy/testdata/plan.json"))
	red, err := readers.Redact(model.KindTFPlan, raw)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "p1.json")
	if err := os.WriteFile(p, red, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := policy.Plan{ID: "p1", Env: "prod", Kind: model.KindTFPlan, Path: p}
	old := policy.RecordedPlans
	policy.RecordedPlans = func(*packs.Input) ([]policy.Plan, error) { return []policy.Plan{plan}, nil }
	t.Cleanup(func() { policy.RecordedPlans = old })
	return plan
}

func TestConfigValidation(t *testing.T) {
	if err := manifest.ValidatePacks(packsOf(t, fmt.Sprintf(cloudConfig, "[checkov]", ""))); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	bad := map[string]string{
		"kind":          "cloud: {iac: [{path: infra, kind: ansible}]}",
		"no kind":       "cloud: {iac: [{path: infra}]}",
		"extra key":     "cloud: {iac: [{path: infra, kind: terraform, env: prod}]}",
		"abs":           "cloud: {iac: [{path: /etc, kind: terraform}]}",
		"dotdot":        "cloud: {iac: [{path: ../x, kind: terraform}]}",
		"not a list":    "cloud: {iac: {path: infra, kind: terraform}}",
		"conftest key":  "cloud: {conftest: {policies: [p]}}",
		"conftest abs":  "cloud: {conftest: {policy: [/p]}}",
		"tags scalar":   "cloud: {tags: owner}",
		"tags empty":    "cloud: {tags: ['']}",
		"conftest list": "cloud: {conftest: [p]}",
	}
	for name, body := range bad {
		if err := manifest.ValidatePacks(packsOf(t, body)); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
}

func TestIaCPolicyBuiltin(t *testing.T) {
	isolate(t)
	withPlan(t)
	root, changed, tracked := repo(t)
	in := input(t, "[]", "", root, changed, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}, BuiltinOnly: true})
	res := byID(rep)
	r := res["cloud/iac-policy"]
	if r.Status != packs.Fail || !r.Required {
		t.Fatalf("iac-policy = %+v", r)
	}
	got := findings(r)
	for _, want := range []string{
		"infra/cfn/template.yaml:4 error IAC002",
		"infra/cfn/template.yaml:22 error IAC003",
		"plan:p1:1 error IAC001", // prod: approve by default (a stored plan is one line)
		"plan:p1:1 error IAC006",
		"plan:p1:1 error SUP001",
	} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if !strings.Contains(r.Detail, "1 plan(s)") || !strings.Contains(r.Detail, "1 suppressed") {
		t.Errorf("detail = %q", r.Detail)
	}
	ev, _ := os.ReadFile(packs.EventsFile())
	if !strings.Contains(string(ev), "suppression:iac001") {
		t.Errorf("no suppression counter: %s", ev)
	}
	// No-plan-files passes on a clean tree.
	if np := res["cloud/no-plan-files"]; np.Status != packs.OK || !np.Required {
		t.Errorf("no-plan-files = %+v", np)
	}
}

func TestIaCPolicySkipsWithoutIaCChange(t *testing.T) {
	isolate(t)
	root, _, tracked := repo(t)
	in := input(t, "[checkov]", "", root, []packs.ChangedFile{{Path: "README.md", Status: 'M'}}, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	for _, r := range rep.Results {
		if r.ID != "cloud/no-plan-files" && r.ID != "cloud/baseline" {
			t.Errorf("%s ran with no IaC change: %+v", r.ID, r)
		}
	}
}

func TestPlanStoreErrorFailsClosed(t *testing.T) {
	isolate(t)
	old := policy.RecordedPlans
	policy.RecordedPlans = func(*packs.Input) ([]policy.Plan, error) { return nil, fmt.Errorf("plans.jsonl is corrupt") }
	t.Cleanup(func() { policy.RecordedPlans = old })
	root, _, tracked := repo(t)
	in := input(t, "[]", "", root, []packs.ChangedFile{{Path: "README.md", Status: 'M'}}, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}, BuiltinOnly: true})
	if r := byID(rep)["cloud/iac-policy"]; r.Status != packs.Fail || !strings.Contains(r.Detail, "recorded plans") {
		t.Errorf("iac-policy = %+v", r)
	}
}

func TestNoPlanFiles(t *testing.T) {
	isolate(t)
	root, changed, _ := repo(t)
	in := input(t, "[]", "", root, changed, []string{"infra/terraform/main.tf", "infra/terraform/terraform.tfstate", "infra/terraform/.terraform.lock.hcl", "infra/terraform/.terraform/environment", "prod.tfplan"})
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}, BuiltinOnly: true})
	r := byID(rep)["cloud/no-plan-files"]
	want := []string{"infra/terraform/.terraform/environment:0 error NOPLAN001", "infra/terraform/terraform.tfstate:0 error NOPLAN001", "prod.tfplan:0 error NOPLAN001"}
	if r.Status != packs.Fail || !reflect.DeepEqual(findings(r), want) {
		t.Errorf("no-plan-files = %v (%+v)", findings(r), r)
	}
}

type fakes struct {
	bin, argv string
}

func installFakes(t *testing.T, tools ...string) fakes {
	t.Helper()
	f := fakes{bin: t.TempDir(), argv: t.TempDir()}
	for _, tool := range tools {
		fakeBin(t, f.bin, tool)
		t.Setenv("TQ_FAKE_VERSION_"+envKey(tool), "9.9.9")
	}
	t.Setenv("TQ_FAKE_CLOUD", "1")
	t.Setenv("TQ_FAKE_ARGV_DIR", f.argv)
	t.Setenv("PATH", f.bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func (f fakes) out(t *testing.T, tool, fixture string, exit int) {
	t.Helper()
	t.Setenv("TQ_FAKE_OUT_"+envKey(tool), filepath.Join("testdata", fixture))
	abs, _ := filepath.Abs(filepath.Join("testdata", fixture))
	t.Setenv("TQ_FAKE_OUT_"+envKey(tool), abs)
	t.Setenv("TQ_FAKE_EXIT_"+envKey(tool), fmt.Sprint(exit))
}

func (f fakes) argvs(t *testing.T, tool string) [][]string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.argv, tool+".argv"))
	var out [][]string
	for _, l := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n") {
		if l != "" {
			out = append(out, strings.Split(l, "\x1f"))
		}
	}
	return out
}

func (f fakes) runner(in *packs.Input) {
	in.Tools.(*toolrun.Runner).LookPath = lookOnly(f.bin)
}

func TestExternalChecksWithFakeCLIs(t *testing.T) {
	isolate(t)
	plan := withPlan(t)
	f := installFakes(t, "checkov", "trivy", "tflint", "bicep", "cfn-lint", "conftest")
	f.out(t, "checkov", "checkov.json", 0)
	f.out(t, "trivy", "trivy.json", 0)
	f.out(t, "tflint", "tflint.json", 2)
	f.out(t, "cfn-lint", "cfn-lint.json", 6)
	f.out(t, "conftest", "conftest.json", 0)
	root, changed, tracked := repo(t)
	sarif := strings.ReplaceAll(testdata(t, "testdata/bicep.sarif"), "/ROOT", "/"+strings.TrimPrefix(filepath.ToSlash(root), "/"))
	sp := filepath.Join(t.TempDir(), "bicep.sarif")
	if err := os.WriteFile(sp, []byte(sarif), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TQ_FAKE_OUT_BICEP", sp)
	t.Setenv("TQ_FAKE_EXIT_BICEP", "1")

	in := input(t, "[checkov, trivy, conftest, tflint, bicep, cfn-lint]", "", root, changed, tracked)
	f.runner(in)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	res := byID(rep)

	ck := res["cloud/checkov"]
	if ck.Status != packs.Fail || !ck.Required || ck.Tool == nil || ck.Tool.Version != "9.9.9" {
		t.Fatalf("checkov = %+v", ck)
	}
	got := findings(ck)
	for _, want := range []string{"infra/terraform/main.tf:12 error checkov:CKV_AWS_24", "plan:p1:0 error checkov:CKV_AWS_24"} {
		if !contains(got, want) {
			t.Errorf("checkov missing %s in %v", want, got)
		}
	}
	runs := f.argvs(t, "checkov")
	var dirs []string
	for _, a := range runs {
		if a[0] == "-d" {
			dirs = append(dirs, a[1])
		} else if a[0] == "-f" {
			if a[1] != plan.Path || a[3] != "terraform_plan" {
				t.Errorf("checkov plan argv = %q", a)
			}
		}
		if !contains(a, "--skip-download") || !contains(a, "--soft-fail") {
			t.Errorf("checkov argv = %q", a)
		}
	}
	sort.Strings(dirs)
	if !reflect.DeepEqual(dirs, []string{"infra/bicep", "infra/cfn", "infra/terraform"}) {
		t.Errorf("checkov dirs = %v", dirs)
	}

	tv := res["cloud/trivy"]
	if tv.Status != packs.Fail || !contains(findings(tv), "infra/terraform/main.tf:13 warn trivy:AVD-AWS-0124") {
		t.Errorf("trivy = %v (%+v)", findings(tv), tv)
	}
	if a := f.argvs(t, "trivy"); len(a) != 2 || a[0][0] != "config" || !contains(a[0], "--exit-code") {
		t.Errorf("trivy argv = %q (terraform and cfn entries only)", a)
	}

	tf := res["cloud/tflint"]
	if tf.Status != packs.Fail || !reflect.DeepEqual(findings(tf), []string{"infra/terraform/main.tf:7 error tflint:aws_instance_invalid_type", "infra/terraform/variables.tf:3 warn tflint:terraform_unused_declarations"}) {
		t.Errorf("tflint = %v (%+v)", findings(tf), tf)
	}
	if a := f.argvs(t, "tflint"); len(a) != 1 || !reflect.DeepEqual(a[0], []string{"--format", "json", "--chdir", "infra/terraform"}) {
		t.Errorf("tflint argv = %q", a)
	}

	bc := res["cloud/bicep-lint"]
	if bc.Status != packs.Fail || !reflect.DeepEqual(findings(bc), []string{"infra/bicep/main.bicep:1 warn bicep:no-unused-params", "infra/bicep/main.bicep:3 error bicep:BCP029"}) {
		t.Errorf("bicep = %v (%+v)", findings(bc), bc)
	}
	if a := f.argvs(t, "bicep"); len(a) != 1 || !reflect.DeepEqual(a[0], []string{"lint", "infra/bicep/main.bicep", "--diagnostics-format", "sarif", "--no-restore"}) {
		t.Errorf("bicep argv = %q", a)
	}

	cl := res["cloud/cfn-lint"]
	if cl.Status != packs.Fail || len(cl.Findings) != 2 {
		t.Errorf("cfn-lint = %+v", cl)
	}
	if a := f.argvs(t, "cfn-lint"); len(a) != 1 || !reflect.DeepEqual(a[0], []string{"-f", "json", "--", "infra/cfn/template.yaml"}) {
		t.Errorf("cfn-lint argv = %q", a)
	}

	cf := res["cloud/conftest"]
	if cf.Status != packs.Fail {
		t.Errorf("conftest = %+v", cf)
	}
	if !contains(findings(cf), "plan:p1:0 error conftest:TQR006") && !contains(findings(cf), "infra/cfn/template.yaml:0 error conftest:TQR006") {
		t.Errorf("conftest findings = %v", findings(cf))
	}
	ca := f.argvs(t, "conftest")
	if len(ca) != 2 { // tq.cfn (template, not prod) and tq.terraform (prod plan)
		t.Fatalf("conftest runs = %q", ca)
	}
	for _, a := range ca {
		if a[0] != "test" || !contains(a, "--no-fail") || !contains(a, "--output") || countOf(a, "--policy") != 2 || !contains(a, "main") {
			t.Errorf("conftest argv = %q", a)
		}
	}
	if !contains(ca[0], "tq.cfn") || !contains(ca[1], "tq.terraform") {
		t.Errorf("conftest namespaces = %q", ca)
	}
	listing, _ := os.ReadFile(filepath.Join(f.argv, "policy-"+policy.BundleHash()))
	if string(listing) != "arm.rego,cfn.rego,lib.rego,terraform.rego" {
		t.Errorf("bundle given to conftest = %q", listing)
	}
	data, _ := os.ReadFile(filepath.Join(f.argv, "data.json"))
	if !strings.Contains(string(data), `"tags":["owner"]`) || !strings.Contains(string(data), `"prod":true`) {
		t.Errorf("conftest data = %s", data)
	}
	// conftest sees the normalised, redacted input: no raw plan file.
	seen, _ := filepath.Glob(filepath.Join(f.argv, "seen-*.json"))
	if len(seen) != 2 {
		t.Fatalf("conftest inputs = %v", seen)
	}
	for _, s := range seen {
		b, _ := os.ReadFile(s)
		if strings.Contains(string(b), "format_version") {
			t.Errorf("conftest got a raw plan: %s", b)
		}
	}
}

func TestDeclaredMissingAndUndeclared(t *testing.T) {
	isolate(t)
	f := installFakes(t) // no tool installed
	root, changed, tracked := repo(t)
	in := input(t, "[checkov]", "", root, changed, tracked)
	f.runner(in)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	res := byID(rep)
	if ck := res["cloud/checkov"]; ck.Status != packs.Fail || !ck.Required || ck.Detail != packs.MissingDetail("cloud") {
		t.Errorf("declared missing checkov = %+v", ck)
	}
	for _, id := range []string{"cloud/trivy", "cloud/tflint", "cloud/bicep-lint", "cloud/cfn-lint", "cloud/conftest"} {
		if r := res[id]; r.Status != packs.Skip || r.Required {
			t.Errorf("undeclared %s = %+v", id, r)
		}
	}
}

func TestConftestBelowMinimum(t *testing.T) {
	isolate(t)
	f := installFakes(t, "conftest")
	t.Setenv("TQ_FAKE_VERSION_CONFTEST", "0.56.0")
	root, changed, tracked := repo(t)
	in := input(t, "[conftest]", "", root, changed, tracked)
	f.runner(in)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	if r := byID(rep)["cloud/conftest"]; r.Status != packs.Fail || !strings.Contains(r.Detail, "below the minimum 0.60.0") {
		t.Errorf("old conftest = %+v", r)
	}
}

func TestHangingToolIsKilled(t *testing.T) {
	isolate(t)
	f := installFakes(t, "tflint")
	t.Setenv("TQ_FAKE_HANG_TFLINT", "1")
	root, changed, tracked := repo(t)
	in := input(t, "[tflint]", "  timeouts: {tflint: 1s}\n", root, changed, tracked)
	f.runner(in)
	start := time.Now()
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	r := byID(rep)["cloud/tflint"]
	if r.Status != packs.Fail || r.Tool == nil || !r.Tool.TimedOut || !strings.Contains(r.Detail, "timed out") {
		t.Errorf("hung tflint = %+v", r)
	}
	if time.Since(start) > 30*time.Second {
		t.Errorf("the hung tool was not killed at its timeout (%s)", time.Since(start))
	}
}

func TestBadToolOutputFails(t *testing.T) {
	isolate(t)
	f := installFakes(t, "trivy", "checkov")
	t.Setenv("TQ_FAKE_OUT_TRIVY", "")
	f.out(t, "checkov", "checkov.json", 2) // a crash exit despite --soft-fail
	root, changed, tracked := repo(t)
	in := input(t, "[trivy, checkov]", "", root, changed, tracked)
	f.runner(in)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	res := byID(rep)
	if r := res["cloud/trivy"]; r.Status != packs.Fail || !contains(findings(r), ":0 error trivy:run-failed") {
		t.Errorf("trivy with no output = %v (%+v)", findings(r), r)
	}
	if r := res["cloud/checkov"]; r.Status != packs.Fail || !strings.Contains(r.Detail, "exited 2") {
		t.Errorf("checkov exit 2 = %+v", r)
	}
}

func TestBaselineDowngradesKnownFindings(t *testing.T) {
	isolate(t)
	f := installFakes(t, "tflint")
	f.out(t, "tflint", "tflint.json", 2)
	root, changed, tracked := repo(t)
	in := input(t, "[tflint]", "", root, changed, tracked)
	f.runner(in)
	known := packs.Finding{File: "infra/terraform/main.tf", Line: 7, Level: packs.LevelError, Rule: "tflint:aws_instance_invalid_type",
		Message: `"t1.2xlarge" is an invalid value as instance_type`}
	in.Baseline = baseline.NewSet(baseline.EntryFor("cloud/tflint", "tflint", known))
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	r := byID(rep)["cloud/tflint"]
	if r.Status != packs.OK {
		t.Errorf("baselined tflint = %v (%+v)", findings(r), r)
	}
}

func TestFlags(t *testing.T) {
	isolate(t)
	root, _, tracked := repo(t)
	cases := []struct {
		path  string
		raise bool
	}{
		{"infra/cfn/other.json", true}, // under an iac path
		{"modules/net/main.tf", true},  // *.tf anywhere
		{"apps/web/main.bicep", true},  // *.bicep
		{"cdk/cdk.json", true},         // cdk.json
		{"stacks/Pulumi.acme-dev.yaml", true},
		{"README.md", false},
		{"sam/template.yaml", true}, // declares AWSTemplateFormatVersion
		{"k8s/template.yaml", false},
	}
	write(t, root, "sam/template.yaml", "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n")
	write(t, root, "k8s/template.yaml", "apiVersion: v1\nkind: ConfigMap\n")
	for _, c := range cases {
		in := input(t, "[]", "", root, []packs.ChangedFile{{Path: c.path, Status: 'M'}}, tracked)
		got := contains(packs.RaiseFlags(in), "iac")
		if got != c.raise {
			t.Errorf("%s: iac raised %v, want %v", c.path, got, c.raise)
		}
	}
	cfg := packs.Merge(packsOf(t, fmt.Sprintf(cloudConfig, "[]", "")), nil)
	if crew := packs.Crew(cfg, []string{"iac"}); !contains(crew, "iac-reviewer") {
		t.Errorf("crew(iac) = %v", crew)
	}
	if crew := packs.Crew(cfg, []string{"baseline_change:cloud"}); !reflect.DeepEqual(crew, []string{"iac-reviewer"}) {
		t.Errorf("crew(baseline_change:cloud) = %v", crew)
	}
	if crew := packs.Crew(cfg, []string{"baseline_change:db"}); contains(crew, "iac-reviewer") {
		t.Errorf("crew(baseline_change:db) = %v", crew)
	}
}

func TestRegisteredIDs(t *testing.T) {
	var ids []string
	for _, c := range packs.Checks("cloud") {
		ids = append(ids, c.ID)
	}
	for _, want := range []string{"cloud/iac-policy", "cloud/no-plan-files", "cloud/checkov", "cloud/trivy", "cloud/conftest", "cloud/tflint", "cloud/bicep-lint", "cloud/cfn-lint"} {
		if !contains(ids, want) {
			t.Errorf("%s not registered (%v)", want, ids)
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s || (strings.HasPrefix(s, ":") && strings.HasSuffix(x, s)) {
			return true
		}
	}
	return false
}

func countOf(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}
