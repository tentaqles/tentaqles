package cloud

// Workstream D-OI's tests (spec D §19): the packs.Count counter names the
// cloud pack writes, pinned against the guard and check sources and the
// framework's name rule, and the cloud doctor probes and detection hints
// (doctor.go). The doctor tests live here because D-OI owns this test file
// and doctor.go only.

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

// ---- counters ----------------------------------------------------------------

// ApplyDeniedReasons are the reasons of cloud: apply_denied:<reason>. §19
// lists the first six; "policy" (an environment whose policy is deny) is
// what the guard writes beyond that list.
var applyDeniedReasons = []string{"expired", "hash-mismatch", "identity-drift", "no-approval", "policy", "unresolved-env", "used"}

// applyAllowed are the cloud: apply_allowed:<policy> counters the guard
// writes (an ask is not an allow).
var applyAllowed = []string{"apply_allowed:approve", "apply_allowed:auto"}

func srcDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	return filepath.Dir(file)
}

func parseSrc(t *testing.T, rel string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(srcDir(t), filepath.FromSlash(rel)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func strLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

// counterLits are the string literals of f that start a counter of family.
func counterLits(f *ast.File, families ...string) []string {
	re := regexp.MustCompile(`^(` + strings.Join(families, "|") + `):[a-z0-9_.-]*$`)
	set := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if s, ok := strLit(e); ok && re.MatchString(s) {
				set[s] = true
			}
		}
		return true
	})
	return sorted(set)
}

// reasonLits are the reason literals of f: the last argument of the deny
// helpers, the returns of deniedWhy, and kebab-case literals assigned to
// counter or why.
func reasonLits(f *ast.File, helpers ...string) []string {
	kebab := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	set := map[string]bool{}
	isHelper := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		if !ok {
			return false
		}
		for _, h := range helpers {
			if id.Name == h {
				return true
			}
		}
		return false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if isHelper(x.Fun) && len(x.Args) > 0 {
				for _, a := range x.Args[len(x.Args)-1:] {
					if s, ok := strLit(a); ok {
						set[s] = true
					}
				}
				// deny(what, where, env, why, counter, res): the counter is
				// the literal before a trailing non-literal argument
				if len(x.Args) >= 2 {
					if _, last := strLit(x.Args[len(x.Args)-1]); !last {
						if s, ok := strLit(x.Args[len(x.Args)-2]); ok && kebab.MatchString(s) {
							set[s] = true
						}
					}
				}
			}
		case *ast.FuncDecl:
			if x.Name.Name == "deniedWhy" {
				ast.Inspect(x.Body, func(m ast.Node) bool {
					if r, ok := m.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
						if s, ok := strLit(r.Results[0]); ok {
							set[s] = true
						}
					}
					return true
				})
			}
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && (id.Name == "counter" || id.Name == "why") && i < len(x.Rhs) {
					if s, ok := strLit(x.Rhs[i]); ok && kebab.MatchString(s) {
						set[s] = true
					}
				}
			}
		}
		return true
	})
	return sorted(set)
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eqList(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s = %q, want %q", what, g, w)
	}
}

// The guard writes exactly the §19 apply counters (plus "policy").
func TestCounterNamesInTheApplyGuard(t *testing.T) {
	f := parseSrc(t, "gate/guard.go")
	eqList(t, "apply_allowed literals", counterLits(f, "apply_allowed"), applyAllowed)
	eqList(t, "apply_denied literals", counterLits(f, "apply_denied"), []string{"apply_denied:", "apply_denied:identity-drift"})
	eqList(t, "apply_denied reasons", reasonLits(f, "deny", "denied"), applyDeniedReasons)
	for _, a := range applyAllowed {
		if envgate.Rank(strings.TrimPrefix(a, "apply_allowed:")) >= len(envgate.Order) {
			t.Errorf("%s does not name a policy value", a)
		}
	}
	// no other counter family is written by the cloud gate
	eqList(t, "cloud gate counter literals", counterLits(f, `[a-z_]+`), append([]string{"apply_denied:", "apply_denied:identity-drift"}, applyAllowed...))
}

// Both cloud checks that honour tq-allow write suppression:<rule>.
func TestSuppressionCounterInTheCloudChecks(t *testing.T) {
	for _, rel := range []string{"register_policy.go", "register_iam.go"} {
		found := false
		ast.Inspect(parseSrc(t, rel), func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || len(c.Args) != 3 {
				return true
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Count" {
				return true
			}
			pack, _ := strLit(c.Args[0])
			if b, ok := c.Args[1].(*ast.BinaryExpr); ok && pack == "cloud" {
				if s, _ := strLit(b.X); s == "suppression:" {
					found = true
				}
			}
			return true
		})
		if !found {
			t.Errorf("%s: no packs.Count(\"cloud\", \"suppression:\"+rule, n)", rel)
		}
	}
}

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

type eventLine struct {
	Pack, Counter string
	N             int
}

func readEvents(t *testing.T) []eventLine {
	t.Helper()
	f, err := os.Open(packs.EventsFile())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []eventLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e struct {
			Pack    string `json:"pack"`
			Counter string `json:"counter"`
			N       int    `json:"n"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Counter != "" {
			out = append(out, eventLine{e.Pack, e.Counter, e.N})
		}
	}
	return out
}

// Every §19 cloud counter name survives the framework's name rule (names
// only: a name the rule drops never reaches tq insights).
func TestCloudCounterNamesAreRecorded(t *testing.T) {
	isolateHome(t)
	var want []string
	for _, r := range applyDeniedReasons {
		want = append(want, "apply_denied:"+r)
	}
	want = append(want, applyAllowed...)
	want = append(want, "apply_allowed:ask", "suppression:iac001", "suppression:iam001", "suppression:azr001", "suppression:gcp001", "suppression:sup001")
	for _, c := range want {
		packs.Count("cloud", c, 1)
	}
	var got []string
	for _, e := range readEvents(t) {
		if e.Pack == "cloud" {
			got = append(got, e.Counter)
		}
	}
	eqList(t, "recorded", got, want)
}

var probeOnce sync.Once

const probeID = "tq/d-oi-counter-probe"

// The guard's counters reach the outcome the hook writes from.
func TestApplyGuardCountersReachTheOutcome(t *testing.T) {
	probeOnce.Do(func() {
		packs.RegisterGuard(packs.Guard{ID: probeID, Pack: "cloud", Category: guardcfg.CatPacks, Check: gate.ApplyEnv})
	})
	e := setup(t, testBlock)
	cases := map[string]string{
		"terraform -chdir=infra/envs/prod apply": "apply_denied:no-approval",
		"terraform -chdir=infra/envs/dev apply":  "apply_allowed:auto",
	}
	for cmd, want := range cases {
		// The package's TestMain gives the guards generous budgets, so a slow
		// box no longer blows the 50 ms one: a failed outcome is a failure.
		var o packs.GuardOutcome
		for _, x := range packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: cmd}, e.guardEnv(), nil, nil) {
			if x.ID == probeID {
				o = x
			}
		}
		if o.Failed {
			t.Fatalf("%s: the guard failed: %+v", cmd, o)
		}
		var names []string
		for _, c := range o.Counters {
			names = append(names, c.Pack+": "+c.Name)
		}
		if len(names) != 1 || names[0] != "cloud: "+want {
			t.Errorf("%s: counters %q, want cloud: %s (%v %q)", cmd, names, want, o.Action, o.Reason)
		}
	}
}

// ---- doctor ------------------------------------------------------------------

func codes(fs []doctor.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Level+" "+f.Code)
	}
	sort.Strings(out)
	return out
}

func find(fs []doctor.Finding, code string) (doctor.Finding, bool) {
	for _, f := range fs {
		if f.Code == code {
			return f, true
		}
	}
	return doctor.Finding{}, false
}

func TestDoctorStateFilesIgnoredAndUntracked(t *testing.T) {
	e := setup(t, testBlock) // .gitignore ignores .terraform/ only
	fs := Doctor(e.in)
	f, ok := find(fs, "cloud-state-not-ignored")
	if !ok || f.Level != "warn" || !strings.Contains(f.Msg, "*.tfstate, *.tfplan") || strings.Contains(f.Msg, ".terraform/") {
		t.Fatalf("not ignored: %+v (%q)", f, codes(fs))
	}
	if _, ok := find(fs, "cloud-state-tracked"); ok {
		t.Fatal("nothing is tracked")
	}

	// only a local exclude: still a warning, and it says so
	os.WriteFile(filepath.Join(e.repo, ".git", "info", "exclude"), []byte("*.tfplan\n"), 0o644)
	f, _ = find(Doctor(e.in), "cloud-state-not-ignored")
	if !strings.Contains(f.Msg, "*.tfplan is ignored only by your own excludes") {
		t.Fatalf("local exclude: %+v", f)
	}

	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte("cdk.out/\n.terraform/\n*.tfstate\n*.tfstate.*\n*.tfplan\n"), 0o644)
	if fs := Doctor(e.in); len(fs) != 0 {
		t.Fatalf("clean repo: %q", codes(fs))
	}

	// a negation un-ignores
	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte(".terraform/\n*.tfstate\n*.tfplan\n!terraform.tfstate\n"), 0o644)
	if f, _ := find(Doctor(e.in), "cloud-state-not-ignored"); !strings.Contains(f.Msg, "*.tfstate") {
		t.Fatalf("negation: %+v", f)
	}

	os.WriteFile(filepath.Join(e.repo, "infra/envs/prod/terraform.tfstate"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(e.repo, "infra/envs/prod/.terraform"), 0o755)
	os.WriteFile(filepath.Join(e.repo, "infra/envs/prod/.terraform/environment"), []byte("prod"), 0o644)
	os.WriteFile(filepath.Join(e.repo, "infra/envs/prod/.terraform.lock.hcl"), []byte(""), 0o644)
	gitRun(t, e.repo, "add", "-f", "infra/envs/prod/terraform.tfstate", "infra/envs/prod/.terraform/environment", "infra/envs/prod/.terraform.lock.hcl")
	f, ok = find(Doctor(e.in), "cloud-state-tracked")
	if !ok || f.Level != "error" || !strings.Contains(f.Msg, "2 Terraform") || strings.Contains(f.Msg, "lock.hcl") {
		t.Fatalf("tracked: %+v", f)
	}
}

func TestDoctorStateFilesWithoutTerraform(t *testing.T) {
	e := setup(t, testBlock)
	gitRun(t, e.repo, "rm", "-q", "--cached", "infra/envs/prod/main.tf", "infra/envs/dev/main.tf")
	if _, ok := find(Doctor(e.in), "cloud-state-not-ignored"); ok {
		t.Fatal("no Terraform file is tracked: the ignore entries are not required")
	}
	e.in.Git = nil
	if fs := Doctor(e.in); len(fs) != 0 {
		t.Fatalf("no git: %q", codes(fs))
	}
}

func TestStateFile(t *testing.T) {
	for p, want := range map[string]bool{
		"terraform.tfstate": true, "infra/x.tfstate.backup": true, "plan.tfplan": true, "a/.terraform/providers/x": true,
		".terraform.lock.hcl": false, "main.tf": false, "docs/tfstate.md": false, "terraform/x.tf": false,
	} {
		if StateFile(p) != want {
			t.Errorf("StateFile(%s) = %v", p, !want)
		}
	}
}

const envBlock = `
default_environment: nope
environments:
  dev:
    match: {dirs: [infra/envs/dev]}
  prod:
    match: {dirs: [infra/envs/prod, infra/gone], var_files: [infra/prod.tfvars]}
  qa:
    match: {dirs: [infra/envs]}
  staging: {}
`

func TestDoctorEnvironments(t *testing.T) {
	e := setup(t, envBlock)
	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte(".terraform/\n*.tfstate\n*.tfplan\n"), 0o644)
	fs := Doctor(e.in)
	var msgs []string
	for _, f := range fs {
		msgs = append(msgs, f.Level+" "+f.Code+": "+f.Msg)
	}
	sort.Strings(msgs)
	want := []string{
		`warn cloud-env-default-unknown: default_environment nope is not a declared environment; an unresolved target counts as the strictest one`,
		`warn cloud-env-overlap: environments dev and qa both match dirs "infra/envs/dev" / "infra/envs"; a target there resolves to the stricter one (dev)`,
		`warn cloud-env-overlap: environments prod and qa both match dirs "infra/envs/prod" / "infra/envs"; a target there resolves to the stricter one (prod)`,
		`warn cloud-env-unresolvable: environment staging has no match entries: tq resolves a target to it only by name or as the default`,
		`warn cloud-env-unresolvable: environments.prod.match.dirs: infra/gone does not exist in the repository, so it never matches`,
		`warn cloud-env-unresolvable: environments.prod.match.var_files: infra/prod.tfvars does not exist in the repository, so it never matches`,
	}
	if strings.Join(msgs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(msgs, "\n"), strings.Join(want, "\n"))
	}
	for _, f := range fs {
		if f.Workspace != "acme" {
			t.Errorf("%s: workspace %q", f.Code, f.Workspace)
		}
	}
}

// A repo-only environment is part of the effective set (it denies), so its
// overlap with a manifest environment is reported too.
func TestDoctorEnvironmentsWithTheRepoLayer(t *testing.T) {
	e := setup(t, testBlock)
	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte(".terraform/\n*.tfstate\n*.tfplan\n"), 0o644)
	repo := map[string]manifest.PackConfig{}
	if err := yaml.Unmarshal([]byte(`cloud: {environments: {sandbox: {match: {dirs: [infra/envs/dev]}}}}`), &repo); err != nil {
		t.Fatal(err)
	}
	e.in.Config = packs.Merge(e.m.Packs, repo)
	f, ok := find(Doctor(e.in), "cloud-env-overlap")
	if !ok || !strings.Contains(f.Msg, "dev and sandbox") || !strings.Contains(f.Msg, "(sandbox)") {
		t.Fatalf("repo-layer overlap: %+v", f)
	}
}

func TestDoctorPlanStore(t *testing.T) {
	e := setup(t, testBlock)
	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte(".terraform/\n*.tfstate\n*.tfplan\n"), 0o644)
	if fs := Doctor(e.in); len(fs) != 0 {
		t.Fatalf("store outside the repo: %q", codes(fs))
	}
	if runtime.GOOS != "windows" {
		dir := filepath.Join(e.in.StateDir(), "plans")
		os.MkdirAll(dir, 0o700)
		os.WriteFile(filepath.Join(dir, "p1.json"), []byte("{}"), 0o644)
		f, ok := find(Doctor(e.in), "cloud-plan-store-mode")
		if !ok || !strings.Contains(f.Msg, "1 file(s)") {
			t.Fatalf("file mode: %+v", f)
		}
		os.Chmod(filepath.Join(dir, "p1.json"), 0o600)
		os.Chmod(dir, 0o755)
		if f, ok := find(Doctor(e.in), "cloud-plan-store-mode"); !ok || !strings.Contains(f.Msg, "not a 0700 folder") {
			t.Fatalf("dir mode: %+v", f)
		}
		os.Chmod(dir, 0o700)
		if _, ok := find(Doctor(e.in), "cloud-plan-store-mode"); ok {
			t.Fatal("0700/0600 store flagged")
		}
	}
	t.Setenv("TQ_HOME", filepath.Join(e.repo, ".tq"))
	f, ok := find(Doctor(e.in), "cloud-plan-store-in-repo")
	if !ok || f.Level != "error" {
		t.Fatalf("store in the repo: %+v", f)
	}
}

// fakeTools is a ToolRunner that runs nothing: it records Run calls.
type fakeTools struct {
	probe toolrun.Probe
	out   string
	ran   int
}

func (f *fakeTools) Probe(pack, tool string) toolrun.Probe { return f.probe }
func (f *fakeTools) Run(ctx context.Context, s toolrun.Spec) (toolrun.Output, error) {
	f.ran++
	if s.Tool != "opa" || strings.Join(s.Args, " ") != "version" {
		return toolrun.Output{ExitCode: 2}, nil
	}
	return toolrun.Output{Stdout: []byte(f.out)}, nil
}

func TestDoctorOPAVersion(t *testing.T) {
	e := setup(t, "require_plan: false")
	pc := e.m.Packs["cloud"]
	pc.Tools = []string{"opa"}
	e.m.Packs["cloud"] = pc
	e.in.Config = packs.Merge(e.m.Packs, nil)
	os.WriteFile(filepath.Join(e.repo, ".gitignore"), []byte(".terraform/\n*.tfstate\n*.tfplan\n"), 0o644)
	outside := filepath.Join(e.home, "bin", "opa")
	os.WriteFile(outside, []byte("not run"), 0o755) // RepoLocal resolves the path: it must exist
	for _, c := range []struct {
		probe   toolrun.Probe
		out     string
		want    string
		runs    int
		contain string
	}{
		{toolrun.Probe{Found: true, Path: outside, Source: "path"}, "Version: 0.70.0\nBuild Commit: x\n", "warn cloud-tool-old", 1, "opa 0.70.0 is below the cloud pack minimum 1.0.0"},
		{toolrun.Probe{Found: true, Path: outside, Source: "path"}, "Version: 1.21.1\n", "", 1, ""},
		{toolrun.Probe{Found: true, Path: outside, Source: "path", Version: "0.9.0"}, "", "warn cloud-tool-old", 0, "opa 0.9.0"},
		{toolrun.Probe{Found: true, Path: outside, Source: "path"}, "unknown\n", "info cloud-tool-version-unknown", 1, "cannot be read"},
		{toolrun.Probe{Found: true, Path: filepath.Join(e.repo, "node_modules", ".bin", "opa"), Source: "path"}, "Version: 0.1.0\n", "info cloud-tool-version-unknown", 0, "inside the repository"},
		{toolrun.Probe{Found: true, Path: filepath.Join(e.repo, ".venv", "bin", "opa"), Source: "venv"}, "Version: 0.1.0\n", "info cloud-tool-version-unknown", 0, "never runs a repo-local binary"},
		{toolrun.Probe{}, "", "", 0, ""},
	} {
		ft := &fakeTools{probe: c.probe, out: c.out}
		e.in.Tools = ft
		fs := Doctor(e.in)
		got := strings.Join(codes(fs), ",")
		if got != c.want || ft.ran != c.runs || (c.contain != "" && !strings.Contains(fs[0].Msg, c.contain)) {
			t.Errorf("%+v: findings %q (%v), runs %d", c.probe, got, fs, ft.ran)
		}
	}
	// undeclared: not probed
	e2 := setup(t, testBlock)
	ft := &fakeTools{probe: toolrun.Probe{Found: true, Path: outside, Version: "0.1.0"}}
	e2.in.Tools = ft
	if _, ok := find(Doctor(e2.in), "cloud-tool-old"); ok || ft.ran != 0 {
		t.Fatal("an undeclared opa was judged")
	}
}

func TestDoctorRegisteredAndHints(t *testing.T) {
	hs := packs.Hints("cloud")
	if len(hs) != len(Hints) {
		t.Fatalf("hints registered: %d of %d", len(hs), len(Hints))
	}
	isolateHome(t)
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	cases := map[string]string{
		"infra/main.tf":           "cloud pack: Terraform files (infra/main.tf)",
		"infra/main.tf.json":      "cloud pack: Terraform files (infra/main.tf.json)",
		"infra/main.tofu":         "cloud pack: OpenTofu files (infra/main.tofu)",
		"infra/main.bicep":        "cloud pack: Bicep files (infra/main.bicep)",
		"app/cdk.json":            "cloud pack: an AWS CDK app (app/cdk.json)",
		"stack/Pulumi.yaml":       "cloud pack: a Pulumi project (stack/Pulumi.yaml)",
		"stack/Pulumi.yml":        "cloud pack: a Pulumi project (stack/Pulumi.yml)",
		"cfn/template.yaml":       "cloud pack: CloudFormation templates (cfn/template.yaml)",
		"cfn/bucket.json":         "cloud pack: CloudFormation templates (cfn/bucket.json)",
		"cfn/sam.yml":             "cloud pack: CloudFormation templates (cfn/sam.yml)",
		"cfn/legacy.template":     "cloud pack: CloudFormation templates (cfn/legacy.template)",
		"config/settings.yaml":    "",
		"package.json":            "",
		"docs/terraform.md":       "",
		".github/workflows/x.yml": "",
	}
	bodies := map[string]string{
		"cfn/template.yaml":       "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n",
		"cfn/bucket.json":         `{"Resources": {"B": {"Type": "AWS::S3::Bucket"}}}`,
		"cfn/sam.yml":             "Transform: AWS::Serverless-2016-10-31\nResources:\n  F:\n    Type: AWS::Serverless::Function\n",
		"cfn/legacy.template":     `{"AWSTemplateFormatVersion": "2010-09-09"}`,
		"config/settings.yaml":    "type: aws:s3:Bucket\nname: acme\n",
		"package.json":            `{"name": "acme", "type": "module"}`,
		".github/workflows/x.yml": "on: push\n",
	}
	for f, want := range cases {
		write(f, bodies[f])
		in := &packs.Input{Root: root, Config: packs.Merge(nil, nil)}
		var got []string
		for _, s := range packs.Suggest(in, []string{f}) {
			if strings.HasPrefix(s, "cloud pack:") {
				got = append(got, s)
			}
		}
		if (want == "" && len(got) != 0) || (want != "" && (len(got) != 1 || got[0] != want)) {
			t.Errorf("%s: %q, want %q", f, got, want)
		}
	}
}
