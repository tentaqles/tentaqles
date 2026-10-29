package data

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
	"gopkg.in/yaml.v3"
)

func pipelineGuardEnv(t testing.TB, body string) packs.GuardEnv {
	t.Helper()
	var m map[string]manifest.PackConfig
	if err := yaml.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	return packs.GuardEnv{Config: packs.Merge(m, nil), Workspace: "acme"}
}

func pipelineOutcome(outs []packs.GuardOutcome) *packs.GuardOutcome {
	for i := range outs {
		if outs[i].ID == "tq/pipeline-run" {
			return &outs[i]
		}
	}
	return nil
}

func TestPipelineRunGuard(t *testing.T) {
	isolatePipeline(t)
	env := pipelineGuardEnv(t, "data: { enabled: true }\n")
	cases := []struct {
		cmd      string
		want     policy.Action
		backfill bool
	}{
		{"airflow dags trigger orders_daily", policy.Ask, false},
		{"airflow dags trigger -c '{\"x\":1}' orders_daily", policy.Ask, false},
		{"airflow dags backfill -s 2026-01-01 -e 2026-01-31 orders_daily", policy.Ask, true},
		{"airflow backfill create --dag-id orders_daily --from-date 2026-01-01 --to-date 2026-01-31", policy.Ask, true},
		{"cd dags && airflow.exe dags trigger orders_daily", policy.Ask, false},
		{"dagster job launch -j orders_job", policy.Ask, false},
		{"dagster job execute -j orders_job", policy.Ask, false},
		{"dagster asset materialize --select orders", policy.Ask, false},
		{"dg launch --assets orders", policy.Ask, false},
		{"prefect deployment run 'orders/daily'", policy.Ask, false},
		{"az datafactory pipeline create-run -g rg --factory-name acme-adf -n orders", policy.Ask, false},
		{"az synapse pipeline create-run --workspace-name acme-syn -n orders", policy.Ask, false},
		{"airflow dags list", policy.Allow, false},
		{"airflow dags trigger --help", policy.Allow, false},
		{"dagster job list", policy.Allow, false},
		{"prefect deployment ls", policy.Allow, false},
		{"az synapse pipeline show --workspace-name acme-syn -n orders", policy.Allow, false},
		{"echo airflow-dags-trigger", policy.Allow, false},
		{"grep -r 'airflow dags trigger' docs/", policy.Allow, false},
		// Wrapped invocations (review r0 M1).
		{"tq dotenv run -- airflow dags trigger orders_daily", policy.Ask, false},
		{"tq dotenv run --file .env.local -- airflow dags backfill -s 2026-01-01 orders_daily", policy.Ask, true},
		{"bash -c 'airflow dags trigger orders_daily'", policy.Ask, false},
		{"bash -lc 'cd /opt/dags && airflow dags trigger orders_daily'", policy.Ask, false},
		{"sh -c \"dagster job launch -j orders_job\"", policy.Ask, false},
		{"pwsh -Command \"airflow dags trigger orders_daily\"", policy.Ask, false},
		{"powershell.exe -NoProfile -c \"prefect deployment run orders/daily\"", policy.Ask, false},
		{"cmd /c airflow dags trigger orders_daily", policy.Ask, false},
		{"docker compose exec airflow-scheduler airflow dags trigger orders_daily", policy.Ask, false},
		{"docker exec -it -u airflow acme-scheduler airflow dags trigger orders_daily", policy.Ask, false},
		{"docker-compose run --rm airflow-cli airflow dags backfill -s 2026-01-01 orders_daily", policy.Ask, true},
		{"kubectl exec -it airflow-0 -n acme -- airflow dags backfill -s 2026-01-01 orders_daily", policy.Ask, true},
		{"kubectl exec airflow-0 -- bash -c 'airflow dags trigger orders_daily'", policy.Ask, false},
		{"ssh tester@scheduler 'airflow dags trigger orders_daily'", policy.Ask, false},
		{"astro dev run dags trigger orders_daily", policy.Ask, false},
		{"astro dev run dags backfill -s 2026-01-01 orders_daily", policy.Ask, true},
		{"python3.12 -m airflow dags trigger orders_daily", policy.Ask, false},
		{"/usr/bin/python3.11 -m dagster job launch -j orders_job", policy.Ask, false},
		{"tq dotenv run -- tq dotenv run -- bash -c 'airflow dags trigger orders_daily'", policy.Ask, false},
		// Line continuations (review r0 m3).
		{"airflow dags \\\n  trigger orders_daily", policy.Ask, false},
		{"airflow dags `\n  trigger orders_daily", policy.Ask, false},
		{"airflow dags \\\r\n  trigger orders_daily", policy.Ask, false},
		// Wrapped read-only commands stay allowed.
		{"tq dotenv run -- airflow dags list", policy.Allow, false},
		{"bash -c 'airflow dags list'", policy.Allow, false},
		{"docker compose exec airflow-scheduler airflow dags list", policy.Allow, false},
		{"kubectl exec airflow-0 -- airflow dags trigger --help", policy.Allow, false},
		{"astro dev run dags list", policy.Allow, false},
		{"docker compose logs airflow-scheduler", policy.Allow, false},
		{"bash -c 'grep -r \"airflow dags trigger\" docs/'", policy.Allow, false},
	}
	for _, c := range cases {
		for _, tool := range []string{"Bash", "PowerShell"} {
			o := pipelineOutcome(packs.EvaluateGuards(policy.ToolCall{Tool: tool, Command: c.cmd}, env, nil, nil))
			if o == nil {
				t.Fatalf("%s: guard did not run", c.cmd)
			}
			if o.Action != c.want {
				t.Errorf("%s %q: action %q, want %q (%s)", tool, c.cmd, o.Action, c.want, o.Reason)
			}
			if c.want == policy.Ask && !o.Handled {
				t.Errorf("%q: an ask is handled", c.cmd)
			}
			if c.backfill && !strings.Contains(o.Reason, "D1 and D2") {
				t.Errorf("%q: backfill reason %q", c.cmd, o.Reason)
			}
			if c.want == policy.Ask && !c.backfill && strings.Contains(o.Reason, "D1 and D2") {
				t.Errorf("%q: not a backfill: %q", c.cmd, o.Reason)
			}
		}
	}
	// Other tools never reach it.
	if o := pipelineOutcome(packs.EvaluateGuards(policy.ToolCall{Tool: "Edit", Path: "dags/x.py", Content: "airflow dags trigger x"}, env, nil, nil)); o != nil {
		t.Fatalf("Edit ran the guard: %+v", o)
	}
}

func TestPipelineRunGuardLayers(t *testing.T) {
	isolatePipeline(t)
	cmd := policy.ToolCall{Tool: "Bash", Command: "airflow dags trigger orders_daily"}
	// A disabled pack never runs its guard.
	if o := pipelineOutcome(packs.EvaluateGuards(cmd, pipelineGuardEnv(t, "data: { enabled: false }\n"), nil, nil)); o != nil {
		t.Fatalf("disabled pack: %+v", o)
	}
	env := pipelineGuardEnv(t, "data: { enabled: true }\n")
	// guard.disable
	if o := pipelineOutcome(packs.EvaluateGuards(cmd, env, func(id string) bool { return id == "tq/pipeline-run" }, nil)); o != nil {
		t.Fatalf("disabled guard ran: %+v", o)
	}
	// guard.actions can raise ask to deny.
	o := pipelineOutcome(packs.EvaluateGuards(cmd, env, nil, func(id string, a policy.Action) policy.Action {
		if id == "tq/pipeline-run" {
			return policy.Deny
		}
		return a
	}))
	if o == nil || o.Action != policy.Deny {
		t.Fatalf("actions: %+v", o)
	}
}

func TestPipelineRunGuardDirect(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	a, reason, handled := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: "prefect deployment run orders/daily"}, g)
	if a != policy.Ask || !handled || !strings.Contains(reason, "prefect deployment run") {
		t.Fatalf("%q %q %v", a, reason, handled)
	}
}

func BenchmarkPipelineRunGuard64KiB(b *testing.B) {
	g := packs.NewGuardCtx(pipelineGuardEnv(b, "data: { enabled: true }\n"), "data")
	cmd := strings.Repeat("echo airflow dags list; ", 64<<10/24) + "airflow dags trigger orders_daily"
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipelineRunGuard(call, g)
	}
}

// Nested wrappers on a 64 KiB command stay inside the guard budget; a
// command too deeply wrapped to see through fails closed (ask).
func TestPipelineRunGuardWrappersBounded(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	for _, c := range []struct {
		cmd  string
		want policy.Action
	}{
		{strings.Repeat("docker exec ", 64<<10/12) + "true", policy.Ask},
		{strings.Repeat("ssh h ", 64<<10/6) + "true", policy.Ask},
		{"docker compose exec acme " + strings.Repeat("x ", 64<<10/2), policy.Allow},
		{strings.Repeat("bash -c '", 1) + strings.Repeat("echo x; ", 64<<10/8) + "'", policy.Allow},
	} {
		start := time.Now()
		a, reason, _ := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: c.cmd}, g)
		if d := time.Since(start); d > packs.GuardBudget {
			t.Fatalf("%.40q: guard took %v", c.cmd, d)
		}
		if a != c.want {
			t.Errorf("%.40q: action %q (%s), want %q", c.cmd, a, reason, c.want)
		}
	}
}

func TestPipelineRunGuardUnder50ms(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	cmd := strings.Repeat("echo airflow dags list; dagster job list; ", 64<<10/42) + "dagster asset materialize --select x"
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	start := time.Now()
	for i := 0; i < 10; i++ {
		if a, _, _ := pipelineRunGuard(call, g); a != policy.Ask {
			t.Fatalf("action %q", a)
		}
	}
	if d := time.Since(start) / 10; d > packs.GuardBudget {
		t.Fatalf("guard took %v on a 64 KiB command", d)
	}
}
