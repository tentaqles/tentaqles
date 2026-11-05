package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// A commit scan that could not finish (stubbed here) is a deny for both
// agents: Claude gets exit 2 and the reason on stderr, Codex the deny JSON
// as well. Neither ever gets an ask.
func TestPreToolUseIncompleteScanDeniesBothAgents(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	prev := hookPolicyDecision
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		return ruleDecision(policy.Deny, "tq/commit-secret", "the secret "+scanTimedOut)
	}
	t.Cleanup(func() { hookPolicyDecision = prev })
	cmd := hookPayloadFor(t, root, `git commit -m "feat: x"`)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, cmd)
	dec, reason := codexAnswerJSON(t, out)
	if code != 2 || dec != "deny" || !strings.Contains(reason, "secrets/tq/commit-secret") {
		t.Fatalf("codex: code %d %q %q", code, dec, reason)
	}
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, cmd)
	if code != 2 || out != "" || !strings.Contains(errOut, "BLOCKED [secrets/tq/commit-secret]") {
		t.Fatalf("claude: code %d out %q err %q", code, out, errOut)
	}
}

func TestSessionStartAcceptsAgentFlag(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, out, _ := runHook(t, []string{"claude-hook", "session-start", "--agent", "codex"}, hookPayloadFor(t, root, "ls"))
	if code != 0 || !strings.Contains(out, "Client:") {
		t.Fatalf("session-start --agent codex: %d %q", code, out)
	}
}

func TestRefuseAgent(t *testing.T) {
	for _, k := range []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
		t.Run(k, func(t *testing.T) {
			for _, o := range []string{"CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TENTAQLES_HEADLESS", "TQ_HUB_TASK", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"} {
				t.Setenv(o, "")
			}
			t.Setenv(k, "1")
			if err := refuseAgent("tq run --off"); !errors.Is(err, errHumanOnly) {
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
}
