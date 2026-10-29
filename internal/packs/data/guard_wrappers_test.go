package data

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration hardening: every data guard that reads through shell wrappers
// fails closed when its scan is cut short, and reads (or asks on) pwsh
// -EncodedCommand, -File and the other opaque PowerShell forms.

func encPwsh(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestPipelineRunGuardPowerShellForms(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	enc := encPwsh("airflow dags trigger orders_daily")
	for _, c := range []struct {
		cmd  string
		want policy.Action
	}{
		{"pwsh -EncodedCommand " + enc, policy.Ask},
		{"pwsh -e " + enc, policy.Ask},
		{"pwsh -ec " + enc, policy.Ask},
		{"powershell -NoProfile -enc " + enc, policy.Ask},
		{"pwsh -EncodedCom " + enc, policy.Ask},
		{"pwsh -e " + encPwsh("Get-ChildItem"), policy.Allow},
		{"pwsh -e ###", policy.Ask},
		{"pwsh -File run.ps1", policy.Ask},
		{"pwsh -NoProfile -f run.ps1", policy.Ask},
		{"pwsh run.ps1", policy.Ask},
		{"pwsh -Command -", policy.Ask},
		{"powershell airflow dags trigger orders_daily", policy.Ask},
		{"pwsh -NoProfile -Command Get-Date", policy.Allow},
		{"bash -c 'pwsh -e " + enc + "'", policy.Ask},
	} {
		a, reason, _ := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: c.cmd}, g)
		if a != c.want {
			t.Errorf("%.60q: action %q (%s), want %q", c.cmd, a, reason, c.want)
		}
	}
}

// A wrapper whose words run past the scan cap is not read to the end: ask.
func TestPipelineRunGuardScanCapFailsClosed(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	for _, cmd := range []string{
		"docker compose exec acme " + strings.Repeat("x ", maxWrapScan+5) + "airflow dags trigger orders_daily",
		"docker " + strings.Repeat("--flag ", maxWrapScan+5) + "exec acme airflow dags trigger orders_daily",
		"kubectl " + strings.Repeat("-n acme ", maxWrapScan) + "exec pod -- airflow dags trigger x",
	} {
		if a, reason, _ := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: cmd}, g); a != policy.Ask {
			t.Errorf("%.60q: action %q (%s), want ask", cmd, a, reason)
		}
	}
	// Short wrappers are still read exactly.
	if a, _, _ := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: "docker compose exec acme ls -la"}, g); a != policy.Allow {
		t.Fatalf("short wrapper: %q", a)
	}
}

func TestDBTGuardsPowerShellForms(t *testing.T) {
	isolate(t)
	repo := t.TempDir()
	env := guardEnv(t, repo, "data: {enabled: true}")
	for _, c := range []struct {
		cmd, id string
		want    policy.Action
	}{
		{"pwsh -EncodedCommand " + encPwsh("dbt run --target prod"), guardProdTarget, policy.Deny},
		{"pwsh -e " + encPwsh("dbt build --full-refresh --target dev"), guardFullRefresh, policy.Ask},
		{"powershell -enc " + encPwsh("dbt run --target dev"), guardProdTarget, policy.Allow},
		{"pwsh -e ###", guardProdTarget, policy.Ask},
		{"pwsh -File build.ps1", guardProdTarget, policy.Ask},
		{"pwsh -File build.ps1", guardFullRefresh, policy.Ask},
		{"pwsh build.ps1", guardFullRefresh, policy.Ask},
		{"pwsh -Command Get-Date", guardProdTarget, policy.Allow},
		// Nested past the parse depth: fail closed.
		{`bash -c "sh -c 'bash -c \"sh -c \\\"bash -c dbt\\\"\"'"`, guardProdTarget, policy.Ask},
	} {
		o := outcome(t, env, "Bash", c.cmd, c.id)
		if o.Action != c.want {
			t.Errorf("%s %.60q: %+v, want %q", c.id, c.cmd, o, c.want)
		}
	}
}

func TestCostGuardsPowerShellForms(t *testing.T) {
	costIsolate(t)
	env := costGuardEnv(t, costFullConfig)
	run := func(id, cmd string) policy.Action {
		for _, o := range packs.EvaluateGuards(policy.ToolCall{Tool: "Bash", Command: cmd}, env, nil, nil) {
			if o.ID == id {
				return o.Action
			}
		}
		return policy.Allow
	}
	for _, c := range []struct {
		id, cmd string
		want    policy.Action
	}{
		{"tq/bq-unbounded-query", "pwsh -en " + encPwsh("bq query 'SELECT 1'"), policy.Deny},
		{"tq/bq-unbounded-query", "pwsh -File q.ps1", policy.Ask},
		{"tq/bq-unbounded-query", "pwsh q.ps1", policy.Ask},
		{"tq/bq-unbounded-query", "pwsh -e ###", policy.Ask},
		{"tq/warehouse-resize", "pwsh -File resize.ps1", policy.Ask},
		{"tq/warehouse-query-unbounded", "pwsh -Command -", policy.Ask},
		{"tq/bq-unbounded-query", "pwsh -Command Get-Date", policy.Allow},
		// Past the tokenizer's word cap: ask.
		{"tq/bq-unbounded-query", strings.Repeat("x ", costMaxWords+10) + "bq query 'SELECT 1'", policy.Ask},
	} {
		if got := run(c.id, c.cmd); got != c.want {
			t.Errorf("%s %.60q: %q, want %q", c.id, c.cmd, got, c.want)
		}
	}
}
