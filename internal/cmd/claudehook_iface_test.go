package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Under --agent codex every non-allow decision is a deny: the one ask left
// (the commit scan's, stubbed here) becomes a deny that says Codex cannot
// ask, while the default agent still gets the ask.
func TestPreToolUseAgentCodexNeverAsks(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	prev := hookPolicyDecision
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		return ruleDecision(policy.Ask, "tq/commit-secret", "the secret "+scanTimedOut)
	}
	t.Cleanup(func() { hookPolicyDecision = prev })
	cmd := hookPayloadFor(t, root, `git commit -m "feat: x"`)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, cmd)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	dec, reason := codexAnswerJSON(t, out)
	if dec != "deny" || !strings.Contains(reason, "tq/commit-secret") || !strings.Contains(reason, "Codex hooks cannot ask") {
		t.Fatalf("codex must get a deny that says why, never an ask: %q %q", dec, reason)
	}
	// The default agent still asks.
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, cmd)
	if !strings.Contains(askReason(t, out), "tq/commit-secret") {
		t.Fatalf("claude must still get the ask: %q", out)
	}
}

func TestCodexAnswerAllowWritesNothing(t *testing.T) {
	var b bytes.Buffer
	if code := codexAnswer(&b, policy.ToolCall{Tool: "Bash"}, policy.Decision{}); code != 0 || b.Len() != 0 {
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
