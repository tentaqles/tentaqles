package gate

import (
	"encoding/base64"
	"testing"
	"unicode/utf16"
)

func gated(cmd string) []Invocation {
	var out []Invocation
	for _, iv := range Analyze(cmd) {
		if iv.Tool != "" {
			out = append(out, iv)
		}
	}
	return out
}

func encPS(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestDetectForms(t *testing.T) {
	cases := []struct {
		cmd  string
		tool string
		op   Op
	}{
		{"terraform apply plan.tfplan", "terraform", OpApply},
		{"/usr/local/bin/terraform -chdir=infra/envs/prod apply -auto-approve", "terraform", OpApply},
		{`"C:\Program Files\tf\terraform.exe" apply`, "terraform", OpApply},
		{"tofu apply -destroy", "tofu", OpDestroy},
		{"terraform destroy", "terraform", OpDestroy},
		{"terraform plan -out=-", "terraform", OpDry},
		{"terraform show -json x", "terraform", OpOther},
		{"pulumi up --yes -s acme/dev", "pulumi", OpApply},
		{"pulumi --color always up", "pulumi", OpApply},
		{"pulumi update", "pulumi", OpApply},
		{"pulumi stack rm acme/dev", "pulumi", OpDestroy},
		{"pulumi preview --json", "pulumi", OpDry},
		{"az deployment group create -g rg-acme-dev -f main.bicep -p a.json b=c", "az", OpApply},
		{"az --subscription 2222 deployment sub create -l westeurope -f main.bicep", "az", OpApply},
		{"az deployment group create -g rg --mode Complete -f main.bicep", "az", OpApply},
		{"az deployment group create -g rg -f main.bicep --what-if", "az", OpDry},
		{"az deployment group what-if -g rg -f main.bicep", "az", OpDry},
		{"az deployment group create -g rg -f main.bicep -w", "az", OpDry},
		{"az deployment mg delete -m acme -n d1", "az", OpDestroy},
		{"az group delete -n rg-acme-dev --yes", "az", OpDestroy},
		{"az stack group create -g rg -n s1", "az", OpApply},
		{"az storage account delete -n x", "az", OpOther},
		{"cdk deploy AcmeStack", "cdk", OpApply},
		{"npx cdk deploy --app cdk.out AcmeStack", "cdk", OpApply},
		{"npx -y cdk destroy AcmeStack", "cdk", OpDestroy},
		{"pnpm exec cdk deploy --all", "cdk", OpApply},
		{"yarn cdk synth", "cdk", OpDry},
		{"bunx cdktf deploy", "cdktf", OpApply},
		{"aws cloudformation deploy --stack-name s --template-file t.yaml", "aws", OpApply},
		{"aws --profile acme-prod cloudformation execute-change-set --change-set-name arn:x", "aws", OpApply},
		{"aws cloudformation deploy --stack-name s --no-execute-changeset", "aws", OpDry},
		{"aws cloudformation delete-stack --stack-name s", "aws", OpDestroy},
		{"aws cloudformation create-change-set --stack-name s --change-set-name c", "aws", OpDry},
		{"aws s3 rm s3://b/k", "aws", OpOther},
		{"sam deploy --stack-name s", "sam", OpApply},
		{"sam delete --stack-name s", "sam", OpDestroy},
		{"tq dotenv run --file .env.prod -- terraform apply", "terraform", OpApply},
		{"env TF_WORKSPACE=prod terraform apply", "terraform", OpApply},
		{"TF_WORKSPACE=prod terraform apply", "terraform", OpApply},
		{"& terraform apply", "terraform", OpApply},
		{"& 'C:\\tools\\terraform.exe' apply", "terraform", OpApply},
		{`bash -c "cd infra && terraform apply"`, "terraform", OpApply},
		{`pwsh -Command "terraform apply"`, "terraform", OpApply},
		{"pwsh -EncodedCommand " + encPS("terraform destroy"), "terraform", OpDestroy},
		{`cmd /c terraform apply`, "terraform", OpApply},
		{`eval "terraform apply"`, "terraform", OpApply},
		{`echo "$(terraform apply)"`, "terraform", OpApply},
		{"sudo terraform apply", "terraform", OpApply},
		{"timeout 600 terraform apply", "terraform", OpApply},
		{"terraform apply `\n  -auto-approve", "terraform", OpApply},
		{"pulumi up --dry-run", "pulumi", OpApply}, // review r0 m4: --dry-run is not a dry run
	}
	for _, c := range cases {
		got := gated(c.cmd)
		if len(got) != 1 || got[0].Tool != c.tool || got[0].Op != c.op {
			t.Errorf("%q: %+v", c.cmd, got)
		}
	}
}

func TestDetectChainsAndState(t *testing.T) {
	got := gated("cd infra/envs/prod && export TF_WORKSPACE=prod; terraform plan -out x && terraform apply x 2>&1 | tee log")
	if len(got) != 2 || got[1].Op != OpApply || got[1].Dir != "infra/envs/prod" || got[1].Env["TF_WORKSPACE"] != "prod" || len(got[1].Pos) != 1 || got[1].Pos[0] != "x" {
		t.Fatalf("%+v", got)
	}
	got = gated("$env:AWS_PROFILE = 'acme-prod'; cdk deploy AcmeStack")
	if len(got) != 1 || got[0].Env["AWS_PROFILE"] != "acme-prod" {
		t.Fatalf("ps env: %+v", got)
	}
	got = gated("cd /tmp && terraform apply")
	if got[0].Dir != "?" {
		t.Fatalf("absolute cd: %q", got[0].Dir)
	}
	// a heredoc body that is data is not a command
	if g := gated("cat <<'EOF' > notes.md\nterraform apply\nEOF"); len(g) != 0 {
		t.Fatalf("heredoc: %+v", g)
	}
	// quoted text is data
	if g := gated(`git commit -m "run terraform apply later"`); len(g) != 0 {
		t.Fatalf("commit message: %+v", g)
	}
}

func TestDetectOpaque(t *testing.T) {
	for _, cmd := range []string{"bash deploy.sh", "pwsh -File deploy.ps1", "pwsh -EncodedCommand !!!", "npx --weird cdk deploy", "xargs terraform apply"} {
		ivs := Analyze(cmd)
		if len(ivs) != 1 || !ivs[0].Opaque {
			t.Errorf("%q: %+v", cmd, ivs)
		}
	}
}

func TestFlags(t *testing.T) {
	iv := gated("terraform -chdir=infra apply -var-file=envs/prod.tfvars -var-file envs/x.tfvars p.tfplan")[0]
	if v, _ := iv.Flag("chdir"); v != "infra" || len(iv.FlagAll("var-file")) != 2 || iv.Pos[0] != "p.tfplan" {
		t.Fatalf("%+v", iv)
	}
	iv = gated("pulumi up -C infra/app -c a=b -s dev --yes")[0]
	if v, _ := iv.Flag("C"); v != "infra/app" {
		t.Fatalf("pulumi -C: %+v", iv.Flags)
	}
	if v, _ := iv.Flag("stack", "s"); v != "dev" {
		t.Fatal("stack")
	}
	iv = gated("az deployment group create -g rg -f main.bicep -p a.json b=c --subscription S")[0]
	if len(iv.FlagAll("p")) != 2 {
		t.Fatalf("az multi: %+v", iv.Flags)
	}
}
