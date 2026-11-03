package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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

func askOn(id string) policy.Decision {
	return policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: id, Action: policy.Ask, Reason: "because", Source: "builtin"}}}
}

func denyOn(id string) policy.Decision {
	return policy.Decision{Action: policy.Deny, Matched: []*policy.Rule{{ID: id, Action: policy.Deny, Reason: "because", Source: "builtin"}}}
}

func checkCodexAnswer(t *testing.T, call policy.ToolCall, d policy.Decision, want string) {
	t.Helper()
	var b bytes.Buffer
	if code := codexAnswer(&b, call, d); code != 0 {
		t.Fatalf("exit %d", code)
	}
	dec, reason := codexAnswerJSON(t, b.String())
	if dec != want {
		t.Fatalf("got %q want %q (%s)", dec, want, b.String())
	}
	if dec == "ask" {
		t.Fatal("never ask: Codex fails open on it")
	}
	if d.Action == policy.Ask && dec == "deny" && !strings.Contains(reason, "Codex hooks cannot ask") {
		t.Fatalf("an ask turned deny says why: %q", reason)
	}
	if d.Action != policy.Allow && !strings.Contains(reason, d.Matched[0].ID) {
		t.Fatalf("the deny names the rule: %q", reason)
	}
}

// Codex never gets an ask: every non-allow decision is a deny, whatever the
// tool, and an allow writes nothing.
func TestCodexAnswerNeverAsks(t *testing.T) {
	bash := policy.ToolCall{Tool: "Bash", Command: `git commit -m "feat: x"`}
	cases := []struct {
		name string
		call policy.ToolCall
		d    policy.Decision
		want string // "" = no output (Codex continues)
	}{
		{"allow writes nothing", bash, policy.Decision{}, ""},
		{"deny stays deny", bash, denyOn("tq/env-file-shell"), "deny"},
		{"the commit scan's ask is a deny", bash, askOn("tq/commit-secret"), "deny"},
		{"the guard-error ask is a deny", bash, askOn(guardErrorID), "deny"},
		{"apply_patch ask", policy.ToolCall{Tool: "apply_patch", Command: "*** Begin Patch"}, askOn(guardErrorID), "deny"},
		{"mcp deny", policy.ToolCall{Tool: "mcp__postgres__query"}, denyOn("tq/cross-client-mcp"), "deny"},
		{"read deny", policy.ToolCall{Tool: "Read", Path: "/x/.env"}, denyOn("tq/env-file-read"), "deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkCodexAnswer(t, tc.call, tc.d, tc.want) })
	}
}

// End to end through the hook: a secret rule is a Codex deny with exit 0
// (Codex reads the answer), and a command no rule covers gets no answer at
// all: nothing of tq's stands between Codex and a terraform apply.
func TestPreToolUseAgentCodexDeniesSecretsOnly(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	args := []string{"claude-hook", "pre-tool-use", "--agent", "codex"}
	code, out, _ := runHook(t, args, hookPayloadFor(t, root, "cat .env"))
	if dec, reason := codexAnswerJSON(t, out); code != 0 || dec != "deny" || !strings.Contains(reason, "tq/env-file-shell") {
		t.Fatalf("cat .env: %d %q", code, out)
	}
	for _, cmd := range []string{"terraform apply", "terraform -chdir=infra apply", `psql -c "drop table t"`} {
		code, out, errOut := runHook(t, args, hookPayloadFor(t, root, cmd))
		if code != 0 || out != "" || errOut != "" {
			t.Fatalf("%s: code %d out %q stderr %q", cmd, code, out, errOut)
		}
	}
}
