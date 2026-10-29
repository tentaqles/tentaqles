package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

// ---- fake CLIs: copies of this test binary named terraform, az, aws, cdk,
// pulumi; each prints a fixture and records its argv (spec D §21).

func TestMain(m *testing.M) {
	if os.Getenv("TQ_FAKE_CLI") == "1" {
		os.Exit(fakeMain())
	}
	os.Exit(m.Run())
}

func fakeMain() int {
	tool := gate.ToolName(os.Args[0])
	args := os.Args[1:]
	if lf := os.Getenv("TQ_FAKE_LOG"); lf != "" {
		f, _ := os.OpenFile(lf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		json.NewEncoder(f).Encode(append([]string{tool}, args...))
		f.Close()
	}
	fix := os.Getenv("TQ_FAKE_FIX")
	cat := func(p string) int {
		b, err := os.ReadFile(filepath.Join(fix, p))
		if err != nil {
			return 9
		}
		os.Stdout.Write(b)
		return 0
	}
	has := func(s string) bool {
		for _, a := range args {
			if a == s {
				return true
			}
		}
		return false
	}
	if has("--version") || has("version") {
		os.Stdout.WriteString("1.9.0\n")
		return 0
	}
	switch tool {
	case "terraform", "tofu":
		if has("plan") {
			for _, a := range args {
				if strings.HasPrefix(a, "-out=") {
					os.WriteFile(strings.TrimPrefix(a, "-out="), []byte("tfplan-binary-v1"), 0o644)
				}
			}
			os.Stdout.WriteString("Plan: 1 to add, 0 to change, 0 to destroy. db_password = hunter2-tester-pass\n") // tq:allow-secret (fake fixture)
			return 0
		}
		if has("show") {
			return cat(os.Getenv("TQ_FAKE_TFPLAN"))
		}
	case "az":
		if has("what-if") {
			return cat("whatif/cli.json")
		}
	case "aws":
		switch {
		case has("get-caller-identity"):
			os.Stdout.WriteString(`{"Account":"111111111111","Arn":"arn:aws:iam::111111111111:user/tester"}`)
			return 0
		case has("create-change-set"):
			os.Stdout.WriteString(`{"Id":"arn:aws:cloudformation:eu-west-1:111111111111:changeSet/c1/00000000-0000-0000-0000-000000000000","StackId":"arn:x"}`)
			return 0
		case has("wait"):
			return 0
		case has("describe-change-set"):
			return cat("changeset/changeset.json")
		}
	case "cdk":
		if has("synth") {
			os.MkdirAll("cdk.out", 0o755)
			b, _ := os.ReadFile(filepath.Join(fix, "cfn/AcmeStack.template.json"))
			os.WriteFile(filepath.Join("cdk.out", "AcmeStack.template.json"), b, 0o644)
			os.WriteFile(filepath.Join("cdk.out", "manifest.json"), []byte(`{"version":"36.0.0","artifacts":{"AcmeStack":{"type":"aws:cloudformation:stack","displayName":"AcmeStack","properties":{"templateFile":"AcmeStack.template.json","stackName":"acme-prod"}}}}`), 0o644)
			return 0
		}
	case "pulumi":
		if has("preview") {
			for i, a := range args {
				if a == "--save-plan" && i+1 < len(args) {
					os.WriteFile(args[i+1], []byte(`{"plan":"tester"}`), 0o644)
				}
			}
			return cat("pulumi/preview.json")
		}
	}
	os.Stderr.WriteString("fake " + tool + ": unexpected " + strings.Join(args, " ") + "\n")
	return 3
}

type env struct {
	t    *testing.T
	repo string
	home string
	in   *packs.Input
	log  string
	m    *manifest.Manifest
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=tester@acme.test", "GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=tester@acme.test")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

const testBlock = `
require_plan: true
environments:
  dev:
    match: {dirs: [infra/envs/dev]}
    apply: auto
  prod:
    match: {dirs: [infra/envs/prod], stacks: [acme-prod, AcmeStack]}
    require_merged: false
`

func setup(t *testing.T, block string) *env {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	keyring.MockInit()
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "AWS_PROFILE", "ARM_SUBSCRIPTION_ID", "TF_WORKSPACE", "AZURE_CONFIG_DIR"} {
		t.Setenv(k, "")
	}
	repo := filepath.Join(home, "acme")
	for _, d := range []string{"infra/envs/prod", "infra/envs/dev", "infra/bicep", "infra/pulumi"} {
		os.MkdirAll(filepath.Join(repo, d), 0o755)
	}
	os.WriteFile(filepath.Join(repo, "infra/envs/prod/main.tf"), []byte("resource \"terraform_data\" \"acme\" {}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "infra/envs/dev/main.tf"), []byte("resource \"terraform_data\" \"acme\" {}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "infra/bicep/main.bicep"), []byte("param name string\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "infra/bicep/prod.json"), []byte(`{"parameters":{}}`), 0o644)
	os.WriteFile(filepath.Join(repo, "infra/pulumi/Pulumi.yaml"), []byte("name: acme\nruntime: go\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "infra/pulumi/Pulumi.prod.yaml"), []byte("config: {}\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("cdk.out/\n.terraform/\n"), 0o644)
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")
	// fake CLIs first on PATH
	bin := filepath.Join(home, "bin")
	os.MkdirAll(bin, 0o755)
	self, _ := os.Executable()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	for _, tool := range []string{"terraform", "tofu", "az", "aws", "cdk", "pulumi"} {
		b, _ := os.ReadFile(self)
		os.WriteFile(filepath.Join(bin, tool+exe), b, 0o755)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TQ_FAKE_CLI", "1")
	wd, _ := os.Getwd()
	fix, _ := filepath.Abs(filepath.Join(wd, "testdata", "readers"))
	t.Setenv("TQ_FAKE_FIX", fix)
	t.Setenv("TQ_FAKE_TFPLAN", "tfplan/builtin-create.json")
	log := filepath.Join(home, "argv.jsonl")
	t.Setenv("TQ_FAKE_LOG", log)
	t.Cleanup(func() { os.Chdir(wd) })
	os.Chdir(repo)

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		t.Fatal(err)
	}
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"cloud": {Enabled: &en, Raw: *doc.Content[0]}}, Identities: map[string]manifest.Identity{}}
	if err := manifest.ValidatePacks(m.Packs); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	cfg := packs.Merge(m.Packs, nil)
	in := &packs.Input{Root: repo, Manifest: m, Config: cfg, Git: packs.GitRepo{Root: repo}, Tools: packs.NewToolRunner(repo, cfg)}
	in.SetWorkspace("acme", repo)
	return &env{t: t, repo: repo, home: home, in: in.ForPack("cloud"), log: log, m: m}
}

func (e *env) tq(args ...string) (string, error) {
	e.t.Helper()
	parent := &cobra.Command{Use: "cloud", SilenceUsage: true, SilenceErrors: true}
	parent.SetContext(packs.WithInput(context.Background(), e.in, nil))
	for _, c := range commands() {
		parent.AddCommand(c)
	}
	var out bytes.Buffer
	parent.SetOut(&out)
	parent.SetErr(io.Discard)
	parent.SetArgs(args)
	err := parent.ExecuteContext(packs.WithInput(context.Background(), e.in, nil))
	return out.String(), err
}

func (e *env) guardEnv() packs.GuardEnv {
	return packs.GuardEnv{Config: e.in.Config, Manifest: e.m, Workspace: "acme", WorkspaceRoot: e.repo, RepoRoot: e.repo}
}

func (e *env) apply(cmd string) (policy.Action, string, packs.GuardCtx) {
	g := packs.NewGuardCtx(e.guardEnv(), "cloud")
	a, r, _ := gate.ApplyEnv(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
	return a, r, g
}

func planID(t *testing.T, out string) string {
	t.Helper()
	f := strings.Fields(out)
	if len(f) < 2 || f[0] != "plan" || !plans.ValidID(f[1]) {
		t.Fatalf("no plan id in %q", out)
	}
	return f[1]
}

func answer(t *testing.T, decision, choice string) {
	t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, err := ledger.SigningKey("acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(decision, choice, "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		t.Fatal(err)
	}
}

func decisionOf(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "asked: decision ") {
			return strings.Fields(l)[2]
		}
	}
	t.Fatalf("no decision in %q", out)
	return ""
}

func applyLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "then apply with: ") {
			return strings.TrimPrefix(l, "then apply with: ")
		}
		if strings.HasPrefix(l, "apply with: ") {
			return strings.TrimPrefix(l, "apply with: ")
		}
	}
	t.Fatalf("no apply form in %q", out)
	return ""
}

// The §2 success criterion: denied, allowed once after the human answers,
// then denied again; a changed plan hash denies.
func TestTerraformApproveFlow(t *testing.T) {
	e := setup(t, testBlock)
	out, err := e.tq("plan", "--env", "prod", "--", "terraform", "-chdir=infra/envs/prod", "plan", "-out=-")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hunter2") {
		t.Fatalf("plan output leaked a value: %s", out)
	}
	id := planID(t, out)
	rec, err := plans.Load(e.in.StateDir(), id)
	if err != nil || rec.Dir != "infra/envs/prod" || rec.InputsHash == "" || rec.Commit == "" || !rec.Clean || rec.Kind != "tfplan" {
		t.Fatalf("record: %v %+v", err, rec)
	}
	raw, _ := os.ReadFile(filepath.Join(e.in.StateDir(), filepath.FromSlash(plans.RecordName(id))))
	red, _ := os.ReadFile(filepath.Join(e.in.StateDir(), filepath.FromSlash(rec.Redacted[0])))
	if bytes.Contains(raw, []byte("hunter2")) || bytes.Contains(red, []byte("hunter2")) {
		t.Fatal("a sensitive value reached the record or the redacted copy")
	}
	// the tool got the store path for -out
	logb, _ := os.ReadFile(e.log)
	if !strings.Contains(string(logb), "-out="+filepath.ToSlash(e.in.StateDir())) {
		t.Fatalf("-out not rewritten: %s", logb)
	}
	applyCmd := strings.TrimPrefix(applyLine(t, out), "")
	if a, _, _ := e.apply(applyCmd); a != policy.Deny {
		t.Fatalf("before approval: %v", a)
	}
	out, err = e.tq("approve", id)
	if err != nil {
		t.Fatal(err)
	}
	dec := decisionOf(t, out)
	answer(t, dec, "apply")
	a, why, _ := e.apply(applyCmd)
	if a != policy.Allow {
		t.Fatalf("after approval: %v %q (%s)", a, why, applyCmd)
	}
	if err := ledger.Claim("acme", "cloud", dec); err != nil {
		t.Fatal(err)
	}
	if a, why, _ := e.apply(applyCmd); a != policy.Deny || !strings.Contains(why, "used") {
		t.Fatalf("second use: %v %q", a, why)
	}
	// show never prints the plan
	out, err = e.tq("show", id)
	if err != nil || strings.Contains(out, "hunter2") || !strings.Contains(out, "apply with:") {
		t.Fatalf("show: %v %q", err, out)
	}
	out, _ = e.tq("show", id, "--json")
	var back plans.Record
	if json.Unmarshal([]byte(out), &back) != nil || back.ID != id {
		t.Fatalf("show --json: %s", out)
	}
}

func TestPlanRefusals(t *testing.T) {
	e := setup(t, testBlock)
	if _, err := e.tq("plan", "--env", "qa", "--", "terraform", "plan"); err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared env: %v", err)
	}
	if _, err := e.tq("plan", "--env", "prod", "--", "terraform", "-chdir=infra/envs/dev", "plan"); err == nil || !strings.Contains(err.Error(), "targets environment dev") {
		t.Fatalf("env mismatch: %v", err)
	}
	if _, err := e.tq("plan", "--env", "prod", "--", "terraform", "apply"); err == nil || !strings.Contains(err.Error(), "not a plan form") {
		t.Fatalf("apply as plan: %v", err)
	}
	if _, err := e.tq("plan", "--env", "prod", "--", "pulumi", "preview", "--stack", "prod", "--show-secrets"); err == nil || !strings.Contains(err.Error(), "show-secrets") {
		t.Fatalf("show-secrets: %v", err)
	}
	// the dev plan needs no approval
	out, err := e.tq("plan", "--env", "dev", "--", "terraform", "-chdir=infra/envs/dev", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tq("approve", planID(t, out)); err == nil || !strings.Contains(err.Error(), "needs no approval") {
		t.Fatalf("dev approve: %v", err)
	}
}

func TestRequireMergedAndPlanFresh(t *testing.T) {
	e := setup(t, strings.Replace(testBlock, "    require_merged: false\n", "", 1))
	out, err := e.tq("plan", "--env", "prod", "--", "terraform", "-chdir=infra/envs/prod", "plan")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	if _, err := e.tq("approve", id); err == nil || !strings.Contains(err.Error(), "remote ref") {
		t.Fatalf("no remote: %v", err)
	}
	gitRun(t, e.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	if _, err := e.tq("approve", id); err != nil {
		t.Fatalf("merged: %v", err)
	}
	// cloud/plan-fresh: fresh now, stale after an edit
	in := *e.in
	in.Changed = []packs.ChangedFile{{Path: "infra/envs/prod/main.tf", Status: 'M'}}
	if !requirePlan(&in) {
		t.Fatal("require_plan")
	}
	if r := runPlanFresh(context.Background(), &in); r.Status != packs.OK {
		t.Fatalf("fresh: %+v", r)
	}
	os.WriteFile(filepath.Join(e.repo, "infra/envs/prod/main.tf"), []byte("resource \"terraform_data\" \"other\" {}\n"), 0o644)
	r := runPlanFresh(context.Background(), &in)
	if r.Status != packs.Fail || len(r.Findings) != 1 || r.Findings[0].File != "infra/envs/prod" {
		t.Fatalf("stale: %+v", r)
	}
	// a dirty tree: require_merged refuses
	out, _ = e.tq("plan", "--env", "prod", "--", "terraform", "-chdir=infra/envs/prod", "plan")
	if _, err := e.tq("approve", planID(t, out)); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("dirty: %v", err)
	}
}

func TestAzWhatIfFlow(t *testing.T) {
	e := setup(t, testBlock+"  prodaz: {match: {resource_groups: [acme-rg]}, apply: approve, destroy: approve}\n")
	out, err := e.tq("plan", "--env", "prodaz", "--", "az", "deployment", "group", "what-if", "-g", "acme-rg", "--template-file", "infra/bicep/main.bicep", "--parameters", "@infra/bicep/prod.json")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	cmd := applyLine(t, out)
	if !strings.HasPrefix(cmd, "az deployment group create") || strings.Contains(cmd, "--result-format") {
		t.Fatalf("apply form: %q", cmd)
	}
	if _, err := e.tq("approve", id); err == nil || !strings.Contains(err.Error(), "--destroy") {
		t.Fatalf("the fixture deletes a database: %v", err)
	}
	out, err = e.tq("approve", id, "--destroy")
	if err != nil {
		t.Fatal(err)
	}
	answer(t, decisionOf(t, out), "apply")
	if a, why, _ := e.apply(cmd); a != policy.Allow {
		t.Fatalf("az approved: %v %q", a, why)
	}
	// a changed template denies
	os.WriteFile(filepath.Join(e.repo, "infra/bicep/main.bicep"), []byte("param other string\n"), 0o644)
	if a, why, _ := e.apply(cmd); a != policy.Deny || !strings.Contains(why, "evidence") {
		t.Fatalf("template changed: %v %q", a, why)
	}
}

func TestCDKFlowOnePerStack(t *testing.T) {
	e := setup(t, testBlock)
	out, err := e.tq("plan", "--env", "prod", "--", "cdk", "synth")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	out, err = e.tq("approve", id)
	if err != nil {
		t.Fatal(err)
	}
	answer(t, decisionOf(t, out), "apply")
	cmd := "cdk deploy --app cdk.out AcmeStack"
	if a, why, _ := e.apply(cmd); a != policy.Allow {
		t.Fatalf("cdk: %v %q", a, why)
	}
	if a, _, _ := e.apply("cdk deploy --app cdk.out --all"); a != policy.Deny {
		t.Fatal("--all")
	}
	if a, _, _ := e.apply("cdk deploy AcmeStack"); a != policy.Deny {
		t.Fatal("no --app")
	}
	os.WriteFile(filepath.Join(e.repo, "cdk.out", "AcmeStack.template.json"), []byte(`{"Resources":{}}`), 0o644)
	if a, _, _ := e.apply(cmd); a != policy.Deny {
		t.Fatal("changed template")
	}
}

func TestChangeSetFlow(t *testing.T) {
	e := setup(t, strings.Replace(testBlock, "    require_merged: false\n", "    require_merged: false\n    destroy: approve\n", 1))
	out, err := e.tq("plan", "--env", "prod", "--", "aws", "cloudformation", "create-change-set", "--stack-name", "acme-prod", "--change-set-name", "c1", "--template-body", "file://infra/envs/prod/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	cmd := applyLine(t, out)
	rec, _ := plans.Load(e.in.StateDir(), id)
	if rec.Account != "111111111111" || !strings.Contains(cmd, ":changeSet/") {
		t.Fatalf("%+v %q", rec, cmd)
	}
	logb, _ := os.ReadFile(e.log)
	if !strings.Contains(string(logb), "change-set-create-complete") || !strings.Contains(string(logb), "--include-property-values") {
		t.Fatalf("wait/describe: %s", logb)
	}
	// the fixture replaces the database: approve needs --destroy
	if _, err := e.tq("approve", id); !rec.Destroys() || err == nil || !strings.Contains(err.Error(), "--destroy") {
		t.Fatalf("destroy plan: %v", err)
	}
	out, err = e.tq("approve", id, "--destroy")
	if err != nil {
		t.Fatal(err)
	}
	answer(t, decisionOf(t, out), "apply")
	if a, why, _ := e.apply(cmd); a != policy.Allow {
		t.Fatalf("cfn: %v %q", a, why)
	}
	if a, _, _ := e.apply("aws cloudformation execute-change-set --change-set-name c1 --stack-name acme-prod"); a != policy.Deny {
		t.Fatal("by name")
	}
}

func TestPulumiWeakAndPlanForms(t *testing.T) {
	e := setup(t, testBlock+"  pprod: {match: {stacks: [prod]}, apply: approve, destroy: approve}\n")
	out, err := e.tq("plan", "--env", "pprod", "--", "pulumi", "preview", "--cwd", "infra/pulumi", "--stack", "prod")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	rec, _ := plans.Load(e.in.StateDir(), id)
	if !rec.Weak {
		t.Fatal("no --save-plan: the weak binding")
	}
	cmd := applyLine(t, out)
	out, err = e.tq("approve", id, "--destroy")
	if err != nil {
		t.Fatal(err)
	}
	answer(t, decisionOf(t, out), "apply")
	if a, why, _ := e.apply(cmd); a != policy.Allow {
		t.Fatalf("pulumi weak: %v %q (%s)", a, why, cmd)
	}
	// a changed stack config denies
	os.WriteFile(filepath.Join(e.repo, "infra/pulumi/Pulumi.prod.yaml"), []byte("config: {x: 1}\n"), 0o644)
	if a, _, _ := e.apply(cmd); a != policy.Deny {
		t.Fatal("stack config changed")
	}
	// the plan form
	out, err = e.tq("plan", "--env", "pprod", "--", "pulumi", "preview", "--cwd", "infra/pulumi", "--stack", "prod", "--save-plan", "x.json")
	if err != nil {
		t.Fatal(err)
	}
	id = planID(t, out)
	rec, _ = plans.Load(e.in.StateDir(), id)
	cmd = applyLine(t, out)
	if rec.Weak || !strings.Contains(cmd, "--plan ") {
		t.Fatalf("plan form: %+v %q", rec, cmd)
	}
	out, _ = e.tq("approve", id, "--destroy")
	answer(t, decisionOf(t, out), "apply")
	if a, why, _ := e.apply(cmd); a != policy.Allow {
		t.Fatalf("pulumi plan: %v %q", a, why)
	}
}

func TestPrune(t *testing.T) {
	e := setup(t, testBlock)
	out, err := e.tq("plan", "--env", "dev", "--", "terraform", "-chdir=infra/envs/dev", "plan")
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, out)
	if out, _ := e.tq("prune", "--older", "7d"); !strings.Contains(out, "pruned 0") {
		t.Fatal(out)
	}
	if out, _ := e.tq("prune", "--older", "0s"); !strings.Contains(out, "pruned 1") {
		t.Fatal(out)
	}
	if _, err := plans.Load(e.in.StateDir(), id); err != nil {
		t.Fatal("record pruned")
	}
}

func TestRegistration(t *testing.T) {
	names := map[string]bool{}
	for _, c := range packs.Commands("cloud") {
		names[c.Name()] = true
	}
	for _, n := range []string{"plan", "show", "approve", "prune"} {
		if !names[n] {
			t.Errorf("command %s missing", n)
		}
	}
	found := false
	for _, c := range packs.Checks("cloud") {
		found = found || c.ID == planFreshID
	}
	if !found {
		t.Fatal("cloud/plan-fresh")
	}
	// an unknown environments key fails the manifest
	var doc yaml.Node
	yaml.Unmarshal([]byte("environments: {prod: {apply: maybe}}\n"), &doc)
	if err := manifest.ValidatePacks(map[string]manifest.PackConfig{"cloud": {Raw: *doc.Content[0]}}); err == nil {
		t.Fatal("bad policy accepted")
	}
}
