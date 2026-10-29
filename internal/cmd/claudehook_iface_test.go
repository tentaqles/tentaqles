package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Interface-commit contract tests (spec E §17). They pin the stubs' seams;
// the owning workstreams replace the stub behaviour.

func TestPreToolUseAgentCodexNeverAsks(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, root, `psql -c "drop table t"`))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		H struct {
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	if got.H.Decision != "deny" || !strings.Contains(got.H.Reason, "tq/sql-destructive-shell") {
		t.Fatalf("codex must get a deny, never an ask: %+v", got.H)
	}
	// The default agent still asks.
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, `psql -c "drop table t"`))
	if !strings.Contains(askReason(t, out), "tq/sql-destructive-shell") {
		t.Fatalf("claude must still get the ask: %q", out)
	}
}

func TestCodexHookOutputAllowWritesNothing(t *testing.T) {
	var b bytes.Buffer
	if code := codexHookOutput(&b, policy.ToolCall{Tool: "Bash"}, policy.Decision{}); code != 0 || b.Len() != 0 {
		t.Fatalf("allow: %d %q", code, b.String())
	}
}

func TestSessionStartAcceptsAgentFlag(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "session-start", "--agent", "codex"}, hookPayloadFor(t, root, "ls"))
	if code != 0 || !strings.Contains(out, "Client:") {
		t.Fatalf("session-start --agent codex: %d %q", code, out)
	}
}

func TestRefuseAgentAndRequireHuman(t *testing.T) {
	for _, k := range []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
		t.Run(k, func(t *testing.T) {
			for _, o := range []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
				t.Setenv(o, "")
			}
			t.Setenv(k, "1")
			if err := refuseAgent("tq run --off"); !errors.Is(err, runner.ErrHumanOnly) {
				t.Fatalf("%s: want ErrHumanOnly, got %v", k, err)
			}
		})
	}
	for _, o := range []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
		t.Setenv(o, "")
	}
	if err := refuseAgent("x"); err != nil {
		t.Fatalf("no marker: %v", err)
	}
	if err := requireHuman(strings.NewReader(""), &bytes.Buffer{}, "tq mode set"); !errors.Is(err, runner.ErrHumanOnly) {
		t.Fatalf("non-TTY must be refused: %v", err)
	}
}

func TestManifestRewriteKeepsModeKeys(t *testing.T) {
	body := "guard:\n  mode: strict\n  rules_override:\n    tq/iac-apply: ask\n  projects:\n    demo-todo: { mode: relaxed }\n  headless_mode: default\n  codex:\n    sandbox: read-only\n"
	root := setupTrustedWorkspaceWithManifest(t, "acme", body)
	w := wsFor(t, root)
	if err := editManifestGuard(w, []string{"tq/force-push"}, "off", ""); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	g := m.Guard
	if g.Mode != "strict" || g.RulesOverride["tq/iac-apply"] != "ask" || g.Projects["demo-todo"].Mode != "relaxed" ||
		g.HeadlessMode != "default" || g.CodexSandbox() != "read-only" || len(g.Disable) != 1 {
		t.Fatalf("mode keys lost on rewrite: %+v", g)
	}
}
