package gate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const prodSub = "22222222-2222-2222-2222-222222222222"
const devSub = "11111111-1111-1111-1111-111111111111"

const cloudBlock = `
environments:
  dev:
    match: {dirs: [infra/envs/dev], subscriptions: [` + devSub + `], profiles: [acme-dev]}
    apply: auto
  staging:
    match: {dirs: [infra/envs/staging]}
  prod:
    match: {dirs: [infra/envs/prod], subscriptions: [` + prodSub + `], stacks: [acme-prod, AcmeProd], profiles: [acme-prod], resource_groups: [rg-acme-prod]}
`

type fixture struct {
	t     *testing.T
	repo  string
	home  string
	env   packs.GuardEnv
	vars  map[string]string
	state string
}

func isolate(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	keyring.MockInit()
	for _, k := range []string{"TQ_HUB_TASK", "TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT"} {
		t.Setenv(k, "")
	}
	return home
}

func newFixture(t *testing.T, block string) *fixture {
	home := isolate(t)
	repo := filepath.Join(home, "acme")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(block), &doc); err != nil {
		t.Fatal(err)
	}
	en := true
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"cloud": {Enabled: &en, Raw: *doc.Content[0]}},
		Identities: map[string]manifest.Identity{}}
	f := &fixture{t: t, repo: repo, home: home, vars: map[string]string{}}
	f.env = packs.GuardEnv{Config: packs.Merge(m.Packs, nil), Manifest: m, Workspace: "acme", WorkspaceRoot: repo, RepoRoot: repo}
	f.state = packs.StateDir("acme", "cloud")
	prev := Getenv
	Getenv = func(k string) string { return f.vars[k] }
	prevWd := Getwd
	Getwd = func() (string, error) { return repo, nil }
	t.Cleanup(func() { Getenv, Getwd = prev, prevWd })
	return f
}

func (f *fixture) ctx() packs.GuardCtx { return packs.NewGuardCtx(f.env, "cloud") }

func (f *fixture) run(guard func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool), cmd string) (policy.Action, string, bool, packs.GuardCtx) {
	g := f.ctx()
	a, r, h := guard(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
	return a, r, h, g
}

// record stores a terraform plan for env with the given identity and
// returns the record.
func (f *fixture) record(env, dir, identity string, destroy bool, summary model.Summary) plans.Record {
	f.t.Helper()
	id := plans.NewID(time.Now())
	name := plans.PlanName(id, "tfplan")
	body := []byte("tfplan-binary-" + id)
	if err := plans.WriteFile(f.state, name, body); err != nil {
		f.t.Fatal(err)
	}
	r := plans.Record{ID: id, Env: env, Tool: "terraform", Dir: dir, Subject: dir, PlanFile: name, PlanSHA256: packs.HashBytes(body),
		InputsHash: "sha256:inputs", Identity: identity, Destroy: destroy, Summary: summary}
	if err := plans.Save(f.state, r); err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *fixture) planPath(r plans.Record) string {
	return filepath.ToSlash(filepath.Join(f.state, filepath.FromSlash(r.PlanFile)))
}

func (f *fixture) ask(r plans.Record, action, stack string) string {
	f.t.Helper()
	id, _, err := approval.Ask("acme", approval.Request{Pack: "cloud", Action: action, Subject: r.Subject, Target: r.Env,
		Evidence: r.Evidence(stack), Summary: "plan " + r.ID, Options: []string{"apply", "reject"}})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) answer(id, choice string) {
	f.t.Helper()
	st, err := store.Open("acme", nil)
	if err != nil {
		f.t.Fatal(err)
	}
	defer st.Close()
	key, err := ledger.SigningKey("acme")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := st.AnswerPackApproval(id, choice, "ok", "hub:tester", store.PackSigner(ledger.AnswerSigner("acme", key))); err != nil {
		f.t.Fatal(err)
	}
}

func TestPolicyPerEnvironment(t *testing.T) {
	f := newFixture(t, cloudBlock)
	cases := []struct {
		cmd  string
		want policy.Action
		frag string
	}{
		{"terraform -chdir=infra/envs/dev apply -auto-approve", policy.Allow, ""},
		{"cd infra/envs/dev && terraform apply", policy.Allow, ""},
		{"terraform -chdir=infra/envs/staging apply", policy.Ask, "staging"},
		{"terraform -chdir=infra/envs/staging destroy", policy.Deny, ""}, // staging destroy: approve, bare destroy has no binding
		{"terraform -chdir=infra/envs/dev destroy", policy.Ask, "dev"},
		{"terraform -chdir=infra/envs/prod apply", policy.Deny, "bare apply"},
		{"terraform -chdir=infra/envs/prod destroy", policy.Deny, "deny"},
		{"terraform apply", policy.Deny, "default environment"}, // unresolved: the strictest (prod)
		{"terraform -chdir=infra/envs/dev plan -out=x", policy.Allow, ""},
		{"aws-vault exec acme -- terraform apply", policy.Deny, "cannot read"},
		{"bash deploy.sh && terraform -chdir=infra/envs/dev apply", policy.Allow, ""},
	}
	for _, c := range cases {
		a, r, h, _ := f.run(ApplyEnv, c.cmd)
		if a != c.want || !h || !strings.Contains(r, c.frag) {
			t.Errorf("%q: %v %v %q", c.cmd, a, h, r)
		}
	}
}

func TestSupersedeOnlyWhatItOwns(t *testing.T) {
	f := newFixture(t, cloudBlock)
	// kubectl is not the gate's: the core rule stays (not handled)
	a, _, h, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/dev apply && kubectl apply -f x.yaml")
	if a != policy.Allow || h {
		t.Fatalf("kubectl: %v %v", a, h)
	}
	// an aws s3 rm keeps tq/cloud-delete
	if _, _, h, _ := f.run(ApplyEnv, "aws s3 rm s3://acme/x"); h {
		t.Fatal("s3 rm handled")
	}
	// a commit message is not a command
	if a, _, h, _ := f.run(ApplyEnv, `git commit -m "terraform apply later"`); a != policy.Allow || h {
		t.Fatal("commit message")
	}
	// nothing IaC: not handled
	if _, _, h, _ := f.run(ApplyEnv, "ls -la"); h {
		t.Fatal("ls")
	}
	// az deployment delete is the gate's (supersedes cloud-delete)
	if _, _, h, _ := f.run(ApplyEnv, "az deployment group delete -g rg-acme-dev -n d1"); !h {
		t.Fatal("deployment delete not handled")
	}
}

func TestPowerShellAndWrappers(t *testing.T) {
	f := newFixture(t, cloudBlock)
	for _, cmd := range []string{
		"$env:TF_WORKSPACE='x'; & terraform -chdir=infra/envs/dev apply",
		"tq dotenv run --file .env.dev -- terraform -chdir=infra/envs/dev apply",
		"pwsh -EncodedCommand " + encPS("terraform -chdir=infra/envs/dev apply"),
	} {
		if a, r, h, _ := f.run(ApplyEnv, cmd); a != policy.Allow || !h {
			t.Errorf("%q: %v %q", cmd, a, r)
		}
	}
	if a, _, _, _ := f.run(ApplyEnv, "pwsh -EncodedCommand "+encPS("terraform -chdir=infra/envs/prod apply")); a != policy.Deny {
		t.Fatal("encoded prod apply")
	}
}

func TestApproveFlowSingleUse(t *testing.T) {
	f := newFixture(t, cloudBlock)
	f.vars["ARM_SUBSCRIPTION_ID"] = prodSub
	r := f.record("prod", "infra/envs/prod", "subscription="+prodSub, false, model.Summary{Total: 1, Counts: map[string]int{"create": 1}})
	cmd := "terraform -chdir=infra/envs/prod apply " + f.planPath(r)
	// before any approval: denied
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "tq pack cloud approve") {
		t.Fatalf("no approval: %v %q", a, why)
	}
	id := f.ask(r, "apply", "")
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "not been answered") {
		t.Fatalf("pending: %v %q", a, why)
	}
	f.answer(id, "apply")
	a, why, h, g := f.run(ApplyEnv, cmd)
	if a != policy.Allow || !h {
		t.Fatalf("approved: %v %q", a, why)
	}
	_ = g
	// the hook claims the approval the guard consumed
	if err := ledger.Claim("acme", "cloud", id); err != nil {
		t.Fatal(err)
	}
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "already used") {
		t.Fatalf("second use: %v %q", a, why)
	}
}

func TestApproveMismatches(t *testing.T) {
	f := newFixture(t, cloudBlock)
	f.vars["ARM_SUBSCRIPTION_ID"] = prodSub
	r := f.record("prod", "infra/envs/prod", "subscription="+prodSub, false, model.Summary{})
	id := f.ask(r, "apply", "")
	f.answer(id, "apply")
	cmd := "terraform -chdir=infra/envs/prod apply " + f.planPath(r)
	// a changed plan file
	os.WriteFile(f.planPath(r), []byte("tampered"), 0o600)
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "evidence changed") {
		t.Fatalf("hash: %v %q", a, why)
	}
	os.WriteFile(f.planPath(r), []byte("tfplan-binary-"+r.ID), 0o600)
	if a, _, _, _ := f.run(ApplyEnv, cmd); a != policy.Allow {
		t.Fatal("restored")
	}
	// a different subscription
	f.vars["ARM_SUBSCRIPTION_ID"] = devSub
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny {
		t.Fatalf("subscription: %v %q", a, why)
	}
	f.vars["ARM_SUBSCRIPTION_ID"] = prodSub
	// expired
	prev := approval.Now
	approval.Now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "expired") {
		t.Fatalf("expired: %v %q", a, why)
	}
	approval.Now = prev
	// a plan outside the store
	other := filepath.ToSlash(filepath.Join(f.repo, "prod.tfplan"))
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+other); a != policy.Deny || !strings.Contains(why, "not a plan tq recorded") {
		t.Fatalf("outside: %v %q", a, why)
	}
	// rejected
	r2 := f.record("prod", "infra/envs/prod", "subscription="+prodSub, false, model.Summary{})
	id2 := f.ask(r2, "apply", "")
	f.answer(id2, "reject")
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r2)); a != policy.Deny || !strings.Contains(why, "not granted") {
		t.Fatalf("rejected: %v %q", a, why)
	}
	// the same approval twice in one command
	both := cmd + " && " + cmd
	if a, why, _, _ := f.run(ApplyEnv, both); a != policy.Deny || !strings.Contains(why, "twice") {
		t.Fatalf("twice: %v %q", a, why)
	}
	// an approved apply next to an ask is denied (the approval is not left unused)
	if a, why, _, _ := f.run(ApplyEnv, cmd+" ; terraform -chdir=infra/envs/staging apply"); a != policy.Deny || !strings.Contains(why, "command of its own") {
		t.Fatalf("mixed: %v %q", a, why)
	}
}

func TestStatefulDestroyPlanIsADestroy(t *testing.T) {
	f := newFixture(t, cloudBlock)
	r := f.record("prod", "infra/envs/prod", "none", false, model.Summary{StatefulDeletes: []model.Ref{{Addr: "aws_db_instance.main", Type: "aws_db_instance"}}})
	// prod destroy: deny, so this apply is denied outright
	a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r))
	if a != policy.Deny || !strings.Contains(why, "destroy is deny") && !strings.Contains(why, "deny") {
		t.Fatalf("%v %q", a, why)
	}
	// staging destroy: approve
	r2 := f.record("staging", "infra/envs/staging", "none", true, model.Summary{})
	id := f.ask(r2, "destroy", "")
	f.answer(id, "apply")
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/staging apply "+f.planPath(r2)); a != policy.Allow {
		t.Fatalf("staging destroy plan: %v %q", a, why)
	}
}

func TestIdentityDrift(t *testing.T) {
	f := newFixture(t, cloudBlock)
	azdir := filepath.Join(f.home, ".azure")
	os.MkdirAll(azdir, 0o700)
	profile := "\xef\xbb\xbf" + `{"subscriptions":[{"id":"` + devSub + `","isDefault":true},{"id":"` + prodSub + `","isDefault":false}]}`
	os.WriteFile(filepath.Join(azdir, "azureProfile.json"), []byte(profile), 0o600)
	// az to prod's resource group while the default subscription is dev's
	a, why, _, _ := f.run(IdentityDrift, "az deployment group create -g rg-acme-prod -f main.bicep")
	if a != policy.Deny || !strings.Contains(why, devSub) || !strings.Contains(why, "prod") {
		t.Fatalf("az drift: %v %q", a, why)
	}
	// --subscription fixes it
	if a, why, _, _ := f.run(IdentityDrift, "az deployment group create -g rg-acme-prod -f main.bicep --subscription "+prodSub); a != policy.Allow {
		t.Fatalf("explicit: %v %q", a, why)
	}
	// dev config into prod's subscription
	f.vars["ARM_SUBSCRIPTION_ID"] = prodSub
	if a, why, _, _ := f.run(IdentityDrift, "terraform -chdir=infra/envs/dev apply"); a != policy.Deny || !strings.Contains(why, prodSub) {
		t.Fatalf("tf drift: %v %q", a, why)
	}
	delete(f.vars, "ARM_SUBSCRIPTION_ID")
	// aws profile
	if a, _, _, _ := f.run(IdentityDrift, "AWS_PROFILE=acme-dev cdk deploy AcmeProd"); a != policy.Deny {
		t.Fatal("cdk profile drift")
	}
	if a, _, _, _ := f.run(IdentityDrift, "cdk deploy AcmeProd --profile acme-prod"); a != policy.Allow {
		t.Fatal("cdk right profile")
	}
	// manifest expectation
	f.env.Manifest.Identities["aws"] = manifest.Identity{Profile: "acme-main"}
	if a, why, _, _ := f.run(IdentityDrift, "terraform -chdir=infra/envs/staging apply --profile x && AWS_PROFILE=acme-other terraform -chdir=infra/envs/staging apply"); a != policy.Deny || !strings.Contains(why, "manifest expects acme-main") {
		t.Fatalf("manifest: %v %q", a, why)
	}
	// dry runs are not checked
	if a, _, _, _ := f.run(IdentityDrift, "AWS_PROFILE=acme-dev cdk diff AcmeProd"); a != policy.Allow {
		t.Fatal("diff")
	}
}

func TestAzureProfileLayout(t *testing.T) {
	if ParseAzureProfile([]byte(`{"subscriptions":[{"id":"a","isDefault":false},{"id":"b","isDefault":true}]}`)) != "b" {
		t.Fatal("default")
	}
	if ParseAzureProfile([]byte("not json")) != "" {
		t.Fatal("bad")
	}
}

func TestStateRel(t *testing.T) {
	isolate(t)
	sd := packs.StateDir("acme", "cloud")
	p := filepath.Join(sd, "plans", "p20261028120000-abcdef.tfplan")
	if StateRel(sd, p) != "plans/p20261028120000-abcdef.tfplan" {
		t.Fatal(StateRel(sd, p))
	}
	if StateRel(sd, filepath.Join(sd, "..", "other", "x")) != "" || StateRel(sd, "plans/x.tfplan") != "" {
		t.Fatal("outside")
	}
	if StateRel(sd, "$TQ_HOME/packs/acme/cloud/plans/p20261028120000-abcdef.tfplan") != "plans/p20261028120000-abcdef.tfplan" {
		t.Fatal("TQ_HOME")
	}
}

func TestGuardFailsClosedOnLedgerError(t *testing.T) {
	f := newFixture(t, cloudBlock)
	r := f.record("prod", "infra/envs/prod", "none", false, model.Summary{})
	os.WriteFile(filepath.Join(f.state, ledger.FileName), []byte("{not json\n"), 0o600)
	if a, _, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r)); a != policy.Deny {
		t.Fatal("corrupt ledger")
	}
	_ = errors.New
}

func BenchmarkApplyEnv64KiB(b *testing.B) {
	home := b.TempDir()
	b.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	var doc yaml.Node
	yaml.Unmarshal([]byte(cloudBlock), &doc)
	m := &manifest.Manifest{Packs: map[string]manifest.PackConfig{"cloud": {Raw: *doc.Content[0]}}}
	env := packs.GuardEnv{Config: packs.Merge(m.Packs, nil), Manifest: m, Workspace: "acme"}
	cmd := strings.Repeat("echo terraform plan && ", 64<<10/24) + "terraform -chdir=infra/envs/dev apply"
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ApplyEnv(call, packs.NewGuardCtx(env, "cloud"))
	}
}

func TestApplyEnvUnder50ms(t *testing.T) {
	f := newFixture(t, cloudBlock)
	cmd := strings.Repeat("echo terraform plan && ", 64<<10/24) + "terraform -chdir=infra/envs/dev apply"
	start := time.Now()
	f.run(ApplyEnv, cmd)
	f.run(IdentityDrift, cmd)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("two guards on 64 KiB took %s", d)
	}
}
