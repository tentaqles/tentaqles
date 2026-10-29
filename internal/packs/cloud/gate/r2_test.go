package gate

// Final review round 0 fixes (feat/packs): each test reproduces a finding.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// dev is auto, prod (the strictest, so the default) approves. dev owns the
// dev workspace, stack, subscription and resource group.
const selectBlock = `
environments:
  dev:
    match: {workspaces: [dev], stacks: [dev], subscriptions: [` + devSub + `], resource_groups: [rg-app]}
    apply: auto
    destroy: auto
  prod:
    match: {workspaces: [prod], stacks: [prod], subscriptions: [` + prodSub + `]}
`

func selectFixture(t *testing.T, workspace string) *fixture {
	f := newFixture(t, selectBlock)
	os.MkdirAll(filepath.Join(f.repo, ".terraform"), 0o755)
	os.MkdirAll(filepath.Join(f.repo, "infra", ".terraform"), 0o755)
	os.WriteFile(filepath.Join(f.repo, ".terraform", "environment"), []byte(workspace), 0o644)
	os.WriteFile(filepath.Join(f.repo, "infra", ".terraform", "environment"), []byte("dev"), 0o644)
	os.MkdirAll(filepath.Join(f.home, ".azure"), 0o755)
	os.WriteFile(filepath.Join(f.home, ".azure", "azureProfile.json"), []byte(`{"subscriptions":[{"id":"`+devSub+`","isDefault":true}]}`), 0o644)
	return f
}

// C1: a tool verb earlier on the line that selects the workspace, stack or
// subscription is read as the later leaves' target (and identity), and a
// selection verb tq does not model makes the rest of the line unknown.
func TestToolSelectionVerbs(t *testing.T) {
	f := selectFixture(t, "dev")
	// controls: the local files select dev
	for _, cmd := range []string{
		"terraform apply -auto-approve",
		"az deployment group create -g rg-app --template-file main.bicep",
		"pulumi up --yes --stack dev",
		// another folder's selection does not move this one
		"terraform -chdir=infra workspace select prod; terraform apply -auto-approve",
		"terraform workspace list && terraform workspace show && terraform apply -auto-approve",
		"az account show && az deployment group create -g rg-app --template-file main.bicep",
	} {
		if a, why, h, _ := f.run(ApplyEnv, cmd); a != policy.Allow || !h {
			t.Errorf("control %q: %v %v %q", cmd, a, h, why)
		}
	}
	notAllowed := func(kind string, a policy.Action, h bool, why, cmd string) {
		t.Helper()
		if a == policy.Allow || !h {
			t.Errorf("%s %q: %v %v %q", kind, cmd, a, h, why)
		}
	}
	bash := []string{
		"terraform workspace select prod && terraform apply -auto-approve",
		"terraform workspace select -or-create prod && terraform apply -auto-approve",
		"terraform workspace new prod; terraform apply -auto-approve",
		"tofu workspace select prod && tofu apply -auto-approve",
		"terraform -chdir=infra workspace select prod && terraform -chdir=infra apply -auto-approve",
		"cd infra && terraform workspace select prod && terraform apply -auto-approve",
		`bash -c "terraform workspace select prod" && terraform apply -auto-approve`,
		"terraform workspace select $W && terraform apply -auto-approve",
		"terraform workspace delete old; terraform apply -auto-approve",
		"terraform workspace select; terraform apply -auto-approve",
		"pulumi stack select prod && pulumi up --yes",
		"pulumi stack select --stack prod && pulumi up --yes",
		"pulumi stack init prod && pulumi up --yes",
		"pulumi login && terraform apply -auto-approve",
		"pulumi logout; terraform apply -auto-approve",
		"az account set --subscription " + prodSub + " && az deployment group create -g rg-app --template-file main.bicep",
		"az account set -s " + prodSub + " && az deployment group create -g rg-app --template-file main.bicep",
		"az account set -n " + prodSub + " && az deployment group create -g rg-app --template-file main.bicep",
		"az login && terraform apply -auto-approve",
		"az logout; terraform apply -auto-approve",
		"az account clear; terraform apply -auto-approve",
		"az config set defaults.group=rg-prod && terraform apply -auto-approve",
		"aws configure set profile.acme.region eu-west-1 && terraform apply -auto-approve",
		"aws configure import --csv file://c.csv && terraform apply -auto-approve",
		"aws sso login --profile acme-prod && terraform apply -auto-approve",
	}
	for _, cmd := range bash {
		a, why, h, _ := f.run(ApplyEnv, cmd)
		notAllowed("bash", a, h, why, cmd)
	}
	ps := []string{
		"terraform workspace select prod; terraform apply -auto-approve",
		"tofu workspace new prod; tofu apply -auto-approve",
		"pulumi stack select prod; pulumi up --yes",
		"az account set --subscription " + prodSub + "; az deployment group create -g rg-app --template-file main.bicep",
		"az login; terraform apply -auto-approve",
		`pwsh -Command "terraform workspace select prod"; terraform apply -auto-approve`,
	}
	for _, cmd := range ps {
		a, why, h := runPS(f, ApplyEnv, cmd)
		notAllowed("ps", a, h, why, cmd)
	}
	// identity drift reads the line's subscription, not azureProfile.json
	for _, cmd := range []string{
		"az account set -s " + prodSub + "; terraform apply -auto-approve",
		"az account set --subscription " + prodSub + " && terraform apply -auto-approve",
	} {
		if d, why, _, _ := f.run(IdentityDrift, cmd); d != policy.Deny || !strings.Contains(why, prodSub) {
			t.Errorf("drift %q: %v %q", cmd, d, why)
		}
		if d, why, _ := runPS(f, IdentityDrift, strings.ReplaceAll(cmd, "&&", ";")); d != policy.Deny {
			t.Errorf("ps drift %q: %v %q", cmd, d, why)
		}
	}
	if d, why, _, _ := f.run(IdentityDrift, "az account set -s "+devSub+"; terraform apply -auto-approve"); d != policy.Allow {
		t.Errorf("drift dev: %v %q", d, why)
	}
	iv := gated("az account set -s " + prodSub + " && az deployment group create -g rg-app --template-file main.bicep")[1]
	if id := ResolveIdentity(iv, hostFor(f.ctx()), true); id.Subscription != prodSub {
		t.Errorf("az identity: %+v", id)
	}
}

// C1: a modelled selection is a target signal of its own: selecting dev
// resolves dev over a prod file, and TF_WORKSPACE (which wins over the
// selection in terraform) still counts.
func TestSelectedWorkspaceResolves(t *testing.T) {
	g := selectFixture(t, "prod")
	for _, cmd := range []string{
		"terraform apply -auto-approve",
	} {
		if a, _, _, _ := g.run(ApplyEnv, cmd); a == policy.Allow {
			t.Errorf("control prod file %q: allowed", cmd)
		}
	}
	if a, why, h, _ := g.run(ApplyEnv, "terraform workspace select dev && terraform apply -auto-approve"); a != policy.Allow || !h {
		t.Errorf("select dev: %v %v %q", a, h, why)
	}
	// TF_WORKSPACE overrides the selected workspace: the stricter counts
	g.vars["TF_WORKSPACE"] = "prod"
	if a, why, _, _ := g.run(ApplyEnv, "terraform workspace select dev && terraform apply -auto-approve"); a == policy.Allow {
		t.Errorf("TF_WORKSPACE over select: %v %q", a, why)
	}
}

// M1: a stored plan is applied to the environment it was recorded for, so
// the record's environment floors the command's.
func TestStoredPlanRecordedEnvFloors(t *testing.T) {
	f := newFixture(t, selectBlock)
	f.vars["TF_WORKSPACE"] = "dev"
	f.vars["ARM_SUBSCRIPTION_ID"] = devSub
	r := f.record("prod", ".", "subscription="+devSub, false, model.Summary{})
	cmd := "terraform apply -auto-approve " + f.planPath(r)
	a, why, h, _ := f.run(ApplyEnv, cmd)
	if a != policy.Deny || !h || !strings.Contains(why, "recorded for prod") {
		t.Fatalf("prod plan under dev signals: %v %v %q", a, h, why)
	}
	// identity drift checks the plan's environment too
	if d, why, _, _ := f.run(IdentityDrift, cmd); d != policy.Deny || !strings.Contains(why, "environment prod") {
		t.Errorf("drift: %v %q", d, why)
	}
	// a dev plan under dev signals stays auto
	rd := f.record("dev", ".", "subscription="+devSub, false, model.Summary{})
	if a, why, h, _ := f.run(ApplyEnv, "terraform apply -auto-approve "+f.planPath(rd)); a != policy.Allow || !h {
		t.Errorf("dev plan: %v %v %q", a, h, why)
	}
	// the approved prod plan applies under dev signals once the human
	// answered, bound to prod
	f.vars["ARM_SUBSCRIPTION_ID"] = prodSub
	rp := f.record("prod", ".", "subscription="+prodSub, false, model.Summary{})
	id := f.ask(rp, "apply", "")
	f.answer(id, "apply")
	if a, why, _, _ := f.run(ApplyEnv, "terraform apply -auto-approve "+f.planPath(rp)); a != policy.Allow {
		t.Errorf("approved prod plan: %v %q", a, why)
	}
}
