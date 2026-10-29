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

func guardEnv(t testing.TB, body string) packs.GuardEnv {
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
	env := guardEnv(t, "data: { enabled: true }\n")
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
	if o := pipelineOutcome(packs.EvaluateGuards(cmd, guardEnv(t, "data: { enabled: false }\n"), nil, nil)); o != nil {
		t.Fatalf("disabled pack: %+v", o)
	}
	env := guardEnv(t, "data: { enabled: true }\n")
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
	g := packs.NewGuardCtx(guardEnv(t, "data: { enabled: true }\n"), "data")
	a, reason, handled := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: "prefect deployment run orders/daily"}, g)
	if a != policy.Ask || !handled || !strings.Contains(reason, "prefect deployment run") {
		t.Fatalf("%q %q %v", a, reason, handled)
	}
}

func BenchmarkPipelineRunGuard64KiB(b *testing.B) {
	g := packs.NewGuardCtx(guardEnv(b, "data: { enabled: true }\n"), "data")
	cmd := strings.Repeat("echo airflow dags list; ", 64<<10/24) + "airflow dags trigger orders_daily"
	call := policy.ToolCall{Tool: "Bash", Command: cmd}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipelineRunGuard(call, g)
	}
}

func TestPipelineRunGuardUnder50ms(t *testing.T) {
	g := packs.NewGuardCtx(guardEnv(t, "data: { enabled: true }\n"), "data")
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
