package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func codexAnswerJSON(t *testing.T, out string) (decision, reason string) {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return "", ""
	}
	var v struct {
		H struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.H.Event != "PreToolUse" {
		t.Fatalf("not a PreToolUse answer: %q (%v)", out, err)
	}
	return v.H.Decision, v.H.Reason
}

// writeTqRules puts tq's rules file, with rules, in a fresh CODEX_HOME.
func writeTqRules(t *testing.T, rules ...codexcfg.PrefixRule) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "codex-home")
	p := filepath.Join(home, filepath.FromSlash(codexcfg.RulesFile))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(codexcfg.RenderRules("default", rules)), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func iacPromptRule() codexcfg.PrefixRule {
	return codexcfg.PrefixRule{ID: "tq/iac-apply", Decision: codexcfg.DecisionPrompt, Pattern: codexcfg.Prefixes("tq/iac-apply")[0]}
}

func askOn(id string) policy.Decision {
	return policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: id, Action: policy.Ask, Reason: "because", Source: "builtin"}}}
}

func TestCodexAnswerNeverAsks(t *testing.T) {
	home := writeTqRules(t, iacPromptRule())
	bash := policy.ToolCall{Tool: "Bash", Command: "terraform apply"}
	cases := []struct {
		name string
		call policy.ToolCall
		d    policy.Decision
		ctx  codexHookCtx
		want string // "" = no output (Codex continues and its own rule prompts)
	}{
		{"deny stays deny", bash, policy.Decision{Action: policy.Deny, Matched: []*policy.Rule{{ID: "tq/env-file-shell", Reason: "x"}}}, codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"covered shell ask is left to Codex's prompt rule", bash, askOn("tq/iac-apply"), codexHookCtx{CodexHome: home, GOOS: "linux"}, ""},
		{"no rules file", bash, askOn("tq/iac-apply"), codexHookCtx{CodexHome: filepath.Join(t.TempDir(), "none"), GOOS: "linux"}, "deny"},
		{"ask without a prefix rule", bash, askOn("tq/cloud-delete"), codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"one of two asks uncovered", bash, policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: "tq/iac-apply"}, {ID: "tq/outside-path-write"}}}, codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"complex script", policy.ToolCall{Tool: "Bash", Command: "terraform apply > log"}, askOn("tq/iac-apply"), codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"apply_patch ask", policy.ToolCall{Tool: "apply_patch", Command: "terraform apply"}, askOn("tq/iac-apply"), codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"mcp ask", policy.ToolCall{Tool: "mcp__github__create_issue"}, askOn("tq/mcp-github-write"), codexHookCtx{CodexHome: home, GOOS: "linux"}, "deny"},
		{"bypass session", bash, askOn("tq/iac-apply"), codexHookCtx{CodexHome: home, GOOS: "linux", PermissionMode: "bypassPermissions"}, "deny"},
		{"windows shell", bash, askOn("tq/iac-apply"), codexHookCtx{CodexHome: home, GOOS: "windows"}, "deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if code := codexAnswer(&b, tc.call, tc.d, tc.ctx); code != 0 {
				t.Fatalf("exit %d", code)
			}
			dec, reason := codexAnswerJSON(t, b.String())
			if dec != tc.want {
				t.Fatalf("got %q want %q (%s)", dec, tc.want, b.String())
			}
			if dec == "ask" {
				t.Fatal("never ask: Codex fails open on it")
			}
			if tc.d.Action == policy.Ask && dec == "deny" && !strings.Contains(reason, "Codex hooks cannot ask") {
				t.Fatalf("an ask turned deny says why: %q", reason)
			}
		})
	}
}

func TestCodexRulesHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/x/codex")
	if got := codexRulesHome(); got != "/x/codex" {
		t.Fatalf("CODEX_HOME: %q", got)
	}
	home := isolateHome(t)
	t.Setenv("CODEX_HOME", "")
	if got := codexRulesHome(); got != filepath.Join(home, ".codex") {
		t.Fatalf("default: %q", got)
	}
}

// End to end through the hook: an ask that a prompt rule in CODEX_HOME
// covers leaves stdout empty; anything else is a deny, never an ask.
func TestPreToolUseAgentCodexPromptRule(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Setenv("CODEX_HOME", writeTqRules(t, iacPromptRule()))
	prev := codexPromptOS
	codexPromptOS = "linux"
	t.Cleanup(func() { codexPromptOS = prev })

	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, root, "terraform apply"))
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("covered ask: %d %q", code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, root, "terraform -chdir=infra apply"))
	if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "deny" {
		t.Fatalf("uncovered ask: %d %q", code, out)
	}
}
