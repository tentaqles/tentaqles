package ml

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

const guardConfig = `
ml:
  enabled: true
  models: [{ name: churn, card: models/churn/MODEL_CARD.md }]
  promotion:
    protected: [production]
    command: "uv run python scripts/promote.py {model} {version} {target}"
`

func guardCtx(t testing.TB, cfg string) packs.GuardCtx {
	c := configFrom(t, cfg, "")
	return packs.NewGuardCtx(packs.GuardEnv{Config: c, Manifest: manifestFrom(t, cfg), Workspace: "acme"}, "ml")
}

func TestModelPromoteGuard(t *testing.T) {
	g := guardCtx(t, guardConfig)
	cases := []struct {
		tool, cmd string
		deny      bool
	}{
		// MLflow REST
		{"Bash", `curl -X POST https://mlflow.acme.test/api/2.0/mlflow/model-versions/transition-stage -d '{"name":"churn","version":"7","stage":"Production"}'`, true},
		{"PowerShell", `Invoke-RestMethod -Method Post -Uri https://mlflow.acme.test/api/2.0/mlflow/registered-models/alias -Body $b`, true},
		{"Bash", `curl -s "https://mlflow.acme.test/api/2.0/mlflow/registered-models/alias?name=churn&alias=champion"`, false}, // get by alias
		{"Bash", `curl -X DELETE https://mlflow.acme.test/api/2.0/mlflow/registered-models/alias -d '{}'`, true},
		// Python one-liners and heredocs
		{"Bash", `python -c "from mlflow import MlflowClient; MlflowClient().set_registered_model_alias('churn','champion','7')"`, true},
		{"Bash", "uv run python - <<'EOF'\nimport mlflow\nmlflow.MlflowClient().transition_model_version_stage('churn', 7, 'Production')\nEOF", true},
		{"Bash", `python3 -c "import boto3; boto3.client('sagemaker').update_model_package(ModelPackageArn=a, ModelApprovalStatus='Approved')"`, true},
		{"Bash", `grep -rn set_registered_model_alias src/`, false},
		// Registry CLIs
		{"Bash", `aws sagemaker update-model-package --model-package-arn arn:aws:sagemaker:us-east-1:111111111111:model-package/churn/7 --model-approval-status Approved`, true},
		{"Bash", `aws sagemaker update-model-package --model-package-arn x --model-approval-status=approved`, true},
		{"Bash", `aws sagemaker update-model-package --model-package-arn x --model-approval-status Rejected`, false},
		{"Bash", `aws sagemaker describe-model-package --model-package-name x`, false},
		{"Bash", `gcloud ai models upload --region=us-central1 --display-name=churn --parent-model=projects/p/locations/us-central1/models/1 --version-aliases=champion`, true},
		{"Bash", `curl -X POST "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/models/1@7:mergeVersionAliases" -d '{"versionAliases":["champion"]}'`, true},
		{"Bash", `gcloud ai models list --region=us-central1`, false},
		{"Bash", `databricks registered-models set-alias main.ml.churn champion 7`, true},
		{"Bash", `databricks model-registry transition-stage churn 7 Production`, true},
		{"Bash", `databricks model-registry approve-transition-request churn 7 Production`, true},
		{"Bash", `databricks registered-models get main.ml.churn`, false},
		{"Bash", `az ml model update --name churn --version 7 --stage Production -g rg -w ws`, true},
		{"Bash", `az ml model show --name churn --version 7`, false},
		// The declared promotion command run directly, and apply.
		{"Bash", `uv run python scripts/promote.py churn 7 production`, true},
		{"PowerShell", `uv run python .\scripts\promote.py churn 7 production`, true},
		{"Bash", `tq pack ml promote apply dec_123`, true},
		{"Bash", `tq pack ml promote request churn 7 --to production`, false},
		{"Bash", `tq pack ml promote status churn`, false},
		{"Bash", `cat scripts/promote_test_notes.txt`, false},
		// MCP tools
		{"mcp__mlflow__set_registered_model_alias", ``, true},
		{"mcp__mlflow__search_registered_models", ``, false},
	}
	for _, c := range cases {
		call := policy.ToolCall{Tool: c.tool, Command: c.cmd}
		act, reason, handled := checkModelPromote(call, g)
		if (act == policy.Deny) != c.deny {
			t.Errorf("%s %q: action %q (%s), want deny=%v", c.tool, c.cmd, act, reason, c.deny)
		}
		if c.deny && (!handled || reason == "") {
			t.Errorf("%q: a deny is handled and has a reason", c.cmd)
		}
		if c.deny && strings.Contains(reason, "Production") {
			t.Errorf("reason echoes the command: %s", reason)
		}
	}
}

func TestModelPromoteGuardModuleCommand(t *testing.T) {
	cfg := strings.Replace(guardConfig, "uv run python scripts/promote.py {model} {version} {target}", "uv run python -m acme_ml.promote {model} {version} {target}", 1)
	g := guardCtx(t, cfg)
	if act, _, _ := checkModelPromote(policy.ToolCall{Tool: "Bash", Command: "uv run python -m acme_ml.promote churn 7 production"}, g); act != policy.Deny {
		t.Fatal("the -m form of the declared command is denied")
	}
	if act, _, _ := checkModelPromote(policy.ToolCall{Tool: "Bash", Command: "uv run python -m acme_ml.train"}, g); act != policy.Allow {
		t.Fatal("another module is allowed")
	}
}

func TestModelPromoteGuardRegistered(t *testing.T) {
	var found bool
	for _, g := range packs.Guards() {
		if g.ID == "tq/model-promote" && g.Pack == "ml" {
			found = true
		}
	}
	if !found {
		t.Fatal("tq/model-promote is registered for the ml pack")
	}
}

func TestModelPromoteGuardBudget(t *testing.T) {
	g := guardCtx(t, guardConfig)
	cmd := strings.Repeat("echo set_registered_model_alias-ish words /api/2.0/mlflow/ ", 64<<10/60)
	start := time.Now()
	for i := 0; i < 5; i++ {
		checkModelPromote(policy.ToolCall{Tool: "Bash", Command: cmd}, g)
	}
	if d := time.Since(start) / 5; d > packs.GuardBudget {
		t.Fatalf("guard took %s on 64 KiB", d)
	}
}

func BenchmarkModelPromoteGuard64KiB(b *testing.B) {
	g := guardCtx(b, guardConfig)
	cmd := strings.Repeat("python -c 'print(1)' ; curl https://acme.test/api/2.0/mlflow/x ", 64<<10/64)
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	for i := 0; i < b.N; i++ {
		checkModelPromote(call, g)
	}
}
