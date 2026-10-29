package gate

// Review round 0 fixes: each test reproduces a finding.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// The C4 probe: dev is auto and selected by the hook's own AWS_PROFILE and
// TF_WORKSPACE; prod (the strictest, the default) approves.
const driftBlock = `
environments:
  dev:
    match: {profiles: [acme-dev], workspaces: [dev]}
    apply: auto
    destroy: auto
  prod:
    match: {profiles: [acme-prod], workspaces: [prod]}
`

func runPS(f *fixture, guard func(policy.ToolCall, packs.GuardCtx) (policy.Action, string, bool), cmd string) (policy.Action, string, bool) {
	return guard(policy.ToolCall{Tool: "PowerShell", Command: cmd}, f.ctx())
}

// C4: an environment change the parser cannot read never falls back to the
// hook's stale variables.
func TestUnmodelledEnvChangesFailClosed(t *testing.T) {
	f := newFixture(t, driftBlock)
	f.vars["AWS_PROFILE"] = "acme-dev"
	f.vars["TF_WORKSPACE"] = "dev"
	// controls: the hook's environment selects dev; a readable export prod
	for cmd, want := range map[string]policy.Action{
		"cdk deploy --all":                                 policy.Allow,
		"terraform apply -auto-approve":                    policy.Allow,
		"export AWS_PROFILE=acme-prod && cdk deploy --all": policy.Deny,
	} {
		if a, why, h, _ := f.run(ApplyEnv, cmd); a != want || !h {
			t.Errorf("control %q: %v %v %q", cmd, a, h, why)
		}
	}
	bash := []string{
		"source ./envs/prod.sh && cdk deploy --all",
		". ./envs/prod.env; terraform apply -auto-approve",
		"set -a; . .env.prod; set +a; cdk deploy --all",
		"set -o allexport; cdk deploy --all",
		"declare -x AWS_PROFILE=acme-prod; cdk deploy --all",
		"typeset -x AWS_PROFILE=acme-prod; cdk deploy --all",
		"unset AWS_PROFILE; cdk deploy --all",
		"direnv allow && cdk deploy --all",
		"direnv exec . cdk deploy --all",
		`eval "$(aws configure export-credentials --profile acme-prod --format env)"; cdk deploy --all`,
		"eval 'export AWS_PROFILE=acme-prod'; cdk deploy --all",
		"export AWS_PROFILE=$(cat profile.txt); cdk deploy --all",
		"AWS_PROFILE=$P cdk deploy --all",
		"P=acme-prod; export AWS_PROFILE=$P; cdk deploy --all",
		"env -u AWS_PROFILE cdk deploy --all",
		"env -i PATH=/usr/bin cdk deploy --all",
		"export -n AWS_PROFILE; cdk deploy --all",
	}
	for _, cmd := range bash {
		if a, why, h, _ := f.run(ApplyEnv, cmd); a == policy.Allow || !h {
			t.Errorf("bash %q: %v %v %q", cmd, a, h, why)
		}
	}
	ps := []string{
		"Set-Item Env:AWS_PROFILE acme-prod; cdk deploy --all",
		"Set-Item -Path Env:AWS_PROFILE -Value acme-prod; cdk deploy --all",
		"New-Item -Path Env:\\AWS_PROFILE -Value acme-prod; cdk deploy --all",
		"Remove-Item Env:AWS_PROFILE; cdk deploy --all",
		"[Environment]::SetEnvironmentVariable('AWS_PROFILE','acme-prod'); cdk deploy --all",
		"[System.Environment]::SetEnvironmentVariable('TF_WORKSPACE','prod', 'Process'); terraform apply",
		". ./prod.ps1; cdk deploy --all",
		"${env:AWS_PROFILE} = $p; cdk deploy --all",
		"$env:AWS_PROFILE = Get-Content p.txt; cdk deploy --all",
	}
	for _, cmd := range ps {
		if a, why, h := runPS(f, ApplyEnv, cmd); a == policy.Allow || !h {
			t.Errorf("ps %q: %v %v %q", cmd, a, h, why)
		}
	}
	// a modelled PowerShell assignment still resolves
	if a, why, _ := runPS(f, ApplyEnv, "$env:AWS_PROFILE = 'acme-dev'; cdk deploy --all"); a != policy.Allow {
		t.Errorf("ps modelled: %v %q", a, why)
	}
	// the floor names the cause
	if _, why, _, _ := f.run(ApplyEnv, "source ./envs/prod.sh && cdk deploy --all"); !strings.Contains(why, "cannot read") {
		t.Errorf("reason: %q", why)
	}
	// an unknown identity never matches a recorded one: an approve
	// environment denies even with an explicit target
	if _, why, _, _ := f.run(ApplyEnv, "source x.sh; terraform -chdir=infra/envs/prod apply /x/plans/p.tfplan"); !strings.Contains(why, "prod") {
		t.Errorf("unknown identity: %q", why)
	}
	iv := gated("source x.sh; cdk deploy AcmeProd")[0]
	if id := ResolveIdentity(iv, hostFor(f.ctx()), false); !id.Unknown || id.String() != "unknown" {
		t.Errorf("identity: %+v %s", id, id.String())
	}
	iv = gated("source x.sh; cdk deploy AcmeProd --profile acme-prod")[0]
	if id := ResolveIdentity(iv, hostFor(f.ctx()), false); id.Unknown || id.Profile != "acme-prod" {
		t.Errorf("flag identity: %+v", id)
	}
	// set -euo pipefail is not set -a
	if a, why, _, _ := f.run(ApplyEnv, "set -euo pipefail; cdk deploy --all"); a != policy.Allow {
		t.Errorf("set -euo: %v %q", a, why)
	}
}

// M1: the npm package spellings of cdk and cdktf are the tools.
func TestRunnerPackageSpellings(t *testing.T) {
	for _, cmd := range []string{
		"npx aws-cdk deploy --all",
		"npx aws-cdk@2.150.0 deploy --all",
		"npx cdk@2 deploy AcmeProd",
		"npx --yes aws-cdk@latest deploy --all",
		"pnpm dlx aws-cdk deploy --all",
		"yarn dlx aws-cdk deploy --all",
		"bunx aws-cdk deploy --all",
		"npm exec aws-cdk -- deploy --all",
	} {
		got := gated(cmd)
		if len(got) != 1 || got[0].Tool != "cdk" || got[0].Op != OpApply {
			t.Errorf("%q: %+v", cmd, got)
		}
	}
	if g := gated("npx cdktf-cli@0.20 deploy"); len(g) != 1 || g[0].Tool != "cdktf" {
		t.Errorf("cdktf-cli: %+v", g)
	}
	for w, want := range map[string]string{"aws-cdk": "cdk", "aws-cdk@2.1.0": "cdk", "cdk@2": "cdk", "@acme/cdk@1": "cdk", "left-pad": "", "terraform@1": "terraform"} {
		if got := PackageTool(w); got != want {
			t.Errorf("PackageTool(%q) = %q, want %q", w, got, want)
		}
	}
	for w, want := range map[string]bool{"aws-cdk": true, "x-terraform": true, "cdk@2": true, "aws-vault": false, "terraform-docs": false} {
		if looseTool(w) != want {
			t.Errorf("looseTool(%q) != %v", w, want)
		}
	}
	f := newFixture(t, cloudBlock)
	// in prod, through the published package name: handled and denied
	if a, why, h, _ := f.run(ApplyEnv, "npx aws-cdk deploy --all --profile acme-prod"); a != policy.Deny || !h {
		t.Errorf("aws-cdk prod: %v %v %q", a, h, why)
	}
	// an unknown runner with a package-spelled tool word is unreadable, not foreign
	if a, why, h, _ := f.run(ApplyEnv, "mystery-runner aws-cdk deploy --all"); a == policy.Allow || !h {
		t.Errorf("unknown runner: %v %v %q", a, h, why)
	}
}

// M2: flags that change what is deployed are not bound.
func TestUnboundFlagsDenied(t *testing.T) {
	f := newFixture(t, cloudBlock)
	for _, cmd := range []string{
		"cdk deploy --app cdk.out AcmeProd --profile acme-prod --parameters AcmeProd:Size=large",
		"cdk deploy --app cdk.out AcmeProd --profile acme-prod --previous-parameters=false",
		"cdk deploy --app cdk.out AcmeProd --profile acme-prod --hotswap",
		"pulumi up --stack acme-prod --yes --config size=large",
		"pulumi up --stack acme-prod --yes --config-file other.yaml",
		"pulumi up --stack acme-prod --yes --replace urn:pulumi:acme-prod::acme::aws:rds/instance:Instance::db",
		"pulumi up --stack acme-prod --yes --target-replace urn:x",
		"pulumi up --stack acme-prod --yes --target urn:x",
		"pulumi up --stack acme-prod --yes --exclude urn:x",
		"pulumi up --stack acme-prod --yes --refresh",
	} {
		a, why, h, _ := f.run(ApplyEnv, cmd)
		if a != policy.Deny || !h || !strings.Contains(why, "cannot be bound") {
			t.Errorf("%q: %v %v %q", cmd, a, h, why)
		}
	}
	if s := unboundFlags(gated("pulumi up --cwd infra/pulumi --stack acme-prod --yes --skip-preview -m 'go'")[0]); s != "" {
		t.Errorf("allowed flags refused: %s", s)
	}
}

// M2: Pulumi's weak binding re-checks the recorded program tree.
func TestPulumiWeakTreeBinding(t *testing.T) {
	f := newFixture(t, cloudBlock)
	dir := "infra/pulumi"
	abs := filepath.Join(f.repo, filepath.FromSlash(dir))
	os.MkdirAll(abs, 0o755)
	files := map[string]string{"Pulumi.yaml": "name: acme\nruntime: go\n", "Pulumi.acme-prod.yaml": "config: {}\n", "main.go": "package main\n"}
	var tree []string
	for n, b := range files {
		os.WriteFile(filepath.Join(abs, n), []byte(b), 0o644)
		tree = append(tree, dir+"/"+n)
	}
	h := hostFor(f.ctx())
	proj, cfg, err := PulumiBinding(h, dir, "acme-prod")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := TreeHash(h, tree)
	if err != nil {
		t.Fatal(err)
	}
	id := plans.NewID(time.Now())
	name := plans.PlanName(id, "preview.json")
	body := []byte(`{"steps":[]}`)
	if err := plans.WriteFile(f.state, name, body); err != nil {
		t.Fatal(err)
	}
	r := plans.Record{ID: id, Env: "prod", Tool: "pulumi", Dir: dir, Subject: "acme-prod", Stack: "acme-prod", Weak: true,
		Project: proj, StackCfg: cfg, Tree: tree, TreeSHA: ts, PlanFile: name, PlanSHA256: packs.HashBytes(body),
		InputsHash: "sha256:inputs", Identity: "none"}
	if err := plans.Save(f.state, r); err != nil {
		t.Fatal(err)
	}
	f.answer(f.ask(r, "apply", ""), "apply")
	cmd := "pulumi up --cwd infra/pulumi --stack acme-prod --yes"
	// a changed program file
	os.WriteFile(filepath.Join(abs, "main.go"), []byte("package main // changed\n"), 0o644)
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "evidence changed") {
		t.Fatalf("changed tree: %v %q", a, why)
	}
	// a removed program file
	os.Remove(filepath.Join(abs, "main.go"))
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny {
		t.Fatalf("removed file: %v %q", a, why)
	}
	os.WriteFile(filepath.Join(abs, "main.go"), []byte(files["main.go"]), 0o644)
	if a, why, h, _ := f.run(ApplyEnv, cmd); a != policy.Allow || !h {
		t.Fatalf("unchanged tree: %v %q", a, why)
	}
	// a record without a tree is refused
	r.Tree, r.TreeSHA = nil, ""
	plans.Save(f.state, r)
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Deny || !strings.Contains(why, "program tree") {
		t.Fatalf("no tree: %v %q", a, why)
	}
}

// M3: a destroying named plan is a destroy binding, in staging (destroy
// approve) and in an environment with apply approve and destroy ask.
func TestDestroyingNamedPlanBinding(t *testing.T) {
	f := newFixture(t, cloudBlock+`  qa:
    match: {dirs: [infra/envs/qa]}
    apply: approve
    destroy: ask
`)
	sum := model.Summary{StatefulDeletes: []model.Ref{{Addr: "aws_db_instance.main", Type: "aws_db_instance"}}}
	for _, env := range []string{"staging", "qa"} {
		dir := "infra/envs/" + env
		// asked as an apply: the binding does not match
		r := f.record(env, dir, "none", false, sum)
		f.answer(f.ask(r, "apply", ""), "apply")
		if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir="+dir+" apply "+f.planPath(r)); a != policy.Deny {
			t.Errorf("%s asked as apply: %v %q", env, a, why)
		}
		// asked as a destroy (approve --destroy): allowed
		r = f.record(env, dir, "none", false, sum)
		f.answer(f.ask(r, "destroy", ""), "apply")
		if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir="+dir+" apply "+f.planPath(r)); a != policy.Allow {
			t.Errorf("%s asked as destroy: %v %q", env, a, why)
		}
	}
}

// C2: only answers signed with the keychain key count.
func TestUnsignedAnswerDoesNotCount(t *testing.T) {
	f := newFixture(t, cloudBlock)
	r := f.record("prod", "infra/envs/prod", "none", false, model.Summary{})
	id := f.ask(r, "apply", "")
	// an answered line written straight into the ledger, without a MAC
	if err := ledger.Append("acme", ledger.Entry{Event: ledger.Answered, Decision: id, Pack: "cloud", Choice: "apply", ActorKind: "hub", TS: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r)); a != policy.Deny {
		t.Fatalf("forged answer: %v %q", a, why)
	}
	// no keychain key at all: nothing counts
	f.answer(id, "apply")
	prev := LoadKey
	LoadKey = func(string) ([]byte, error) { return nil, ledger.ErrNoKey }
	defer func() { LoadKey = prev }()
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r)); a != policy.Deny {
		t.Fatalf("no key: %v %q", a, why)
	}
	LoadKey = prev
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=infra/envs/prod apply "+f.planPath(r)); a != policy.Allow {
		t.Fatalf("signed: %v %q", a, why)
	}
}

// m2: relative paths resolve from the hook's working directory.
func TestRelativePathsFromTheHookCwd(t *testing.T) {
	f := newFixture(t, cloudBlock)
	Getwd = func() (string, error) { return filepath.Join(f.repo, "infra", "envs"), nil }
	if a, why, _, _ := f.run(ApplyEnv, "cd dev && terraform apply"); a != policy.Allow {
		t.Errorf("sub-folder cd: %v %q", a, why)
	}
	if a, why, _, _ := f.run(ApplyEnv, "terraform -chdir=dev apply"); a != policy.Allow {
		t.Errorf("sub-folder chdir: %v %q", a, why)
	}
	if a, _, _, _ := f.run(ApplyEnv, "cd prod && terraform apply"); a != policy.Deny {
		t.Error("sub-folder prod")
	}
	// a hook started outside the repository resolves from the repo root
	Getwd = func() (string, error) { return filepath.Dir(f.repo), nil }
	if a, _, _, _ := f.run(ApplyEnv, "cd infra/envs/dev && terraform apply"); a != policy.Allow {
		t.Error("outside the repo")
	}
	if a, _, _, _ := f.run(ApplyEnv, "cd dev && terraform apply"); a != policy.Deny {
		t.Error("outside the repo, sub-folder path")
	}
}

// m3: an approved apply next to a command the core rules still ask about
// is denied, so the approval is not used up by a declined ask.
func TestApprovedApplyWithForeignLeaf(t *testing.T) {
	f := newFixture(t, cloudBlock)
	r := f.record("prod", "infra/envs/prod", "none", false, model.Summary{})
	f.answer(f.ask(r, "apply", ""), "apply")
	cmd := "terraform -chdir=infra/envs/prod apply " + f.planPath(r)
	a, why, h, g := f.run(ApplyEnv, cmd+" && kubectl apply -f x.yaml")
	if a != policy.Deny || !h || !strings.Contains(why, "command of its own") {
		t.Fatalf("foreign leaf: %v %v %q", a, h, why)
	}
	_ = g
	if a, why, _, _ := f.run(ApplyEnv, cmd); a != policy.Allow {
		t.Fatalf("alone: %v %q", a, why)
	}
}

// m4: --dry-run does not make an apply a dry run.
func TestDryRunFlagIsNotADryRun(t *testing.T) {
	f := newFixture(t, cloudBlock)
	if a, why, h, _ := f.run(ApplyEnv, "cdk deploy --all --dry-run --profile acme-prod"); a != policy.Deny || !h {
		t.Fatalf("%v %v %q", a, h, why)
	}
}
