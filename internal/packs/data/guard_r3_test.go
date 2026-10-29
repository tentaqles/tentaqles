package data

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 3: a global flag with a value before the subcommand
// (az --subscription x synapse pipeline create-run) must not hide a run.
func TestPipelineRunGuardFlagValueBeforeSubcommand(t *testing.T) {
	g := packs.NewGuardCtx(pipelineGuardEnv(t, "data: { enabled: true }\n"), "data")
	for _, c := range []struct {
		cmd  string
		want policy.Action
	}{
		{"az --subscription acme synapse pipeline create-run --workspace-name w --name p", policy.Ask},
		{"az --foo x datafactory pipeline create-run -g rg --factory-name f --name p", policy.Ask},
		{"airflow --foo x dags trigger orders_daily", policy.Ask},
		{"dagster --foo x job launch -j orders", policy.Ask},
		{"prefect --profile acme deployment run orders/daily", policy.Ask},
		{"dg --foo x launch --job orders", policy.Ask},
		// a flag between the subcommand words still counts
		{"az synapse --only-show-errors pipeline create-run --name p", policy.Ask},
		// controls
		{"az --subscription acme synapse pipeline show --name p", policy.Allow},
		{"airflow --foo x dags list", policy.Allow},
		{"az --subscription acme account show", policy.Allow},
	} {
		if a, reason, _ := pipelineRunGuard(policy.ToolCall{Tool: "Bash", Command: c.cmd}, g); a != c.want {
			t.Errorf("%q: action %q (%s), want %q", c.cmd, a, reason, c.want)
		}
	}
}
