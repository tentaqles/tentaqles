package ml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 2: tq/model-promote reads a shell script the command
// runs (through ReadLocal) and asks when it is a promotion.
func TestModelPromoteGuardScriptFiles(t *testing.T) {
	repo := t.TempDir()
	for n, body := range map[string]string{
		"promote.sh":  "databricks registered-models set-alias main.ml.churn champion 7\n",
		"declared.sh": "uv run python scripts/promote.py churn 7 production\n",
		"train.sh":    "uv run python -m train\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, n), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := configFrom(t, guardConfig, "")
	g := packs.NewGuardCtx(packs.GuardEnv{Config: c, Manifest: manifestFrom(t, guardConfig), Workspace: "acme",
		WorkspaceRoot: repo, RepoRoot: repo, Cwd: repo}, "ml")
	for _, k := range []struct {
		cmd  string
		want policy.Action
	}{
		{"bash promote.sh", policy.Ask},
		{"./declared.sh", policy.Ask},
		{"bash train.sh", policy.Allow},
		{"bash missing.sh", policy.Allow},
	} {
		a, reason, _ := checkModelPromote(policy.ToolCall{Tool: "Bash", Command: k.cmd}, g)
		if a == "" {
			a = policy.Allow
		}
		if a != k.want {
			t.Errorf("%q: %q (%s), want %q", k.cmd, a, reason, k.want)
		}
	}
}
