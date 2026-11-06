package cmd

import (
	"encoding/json"
	"strings"
	"testing"
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

// End to end through the hook: a secret rule is a Codex deny (the JSON
// answer and exit 2, either blocks), and a command no rule covers gets no
// answer at all: nothing of tq's stands between Codex and a terraform apply.
func TestPreToolUseAgentCodexDeniesSecretsOnly(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	args := []string{"claude-hook", "pre-tool-use", "--agent", "codex"}
	code, out, _ := runHook(t, args, hookPayloadFor(t, root, "cat .env"))
	if dec, reason := codexAnswerJSON(t, out); code != 2 || dec != "deny" || !strings.Contains(reason, "tq/env-file-shell") {
		t.Fatalf("cat .env: %d %q", code, out)
	}
	for _, cmd := range []string{"terraform apply", "terraform -chdir=infra apply", `psql -c "drop table t"`} {
		code, out, errOut := runHook(t, args, hookPayloadFor(t, root, cmd))
		if code != 0 || out != "" || errOut != "" {
			t.Fatalf("%s: code %d out %q stderr %q", cmd, code, out, errOut)
		}
	}
}
