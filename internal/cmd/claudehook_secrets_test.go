package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The hook applies the identity guard and the secret rules and nothing
// else. The same table runs in a trusted workspace and in a neutral folder:
// the secret rules fire in both, a force push and a terraform apply are not
// tq's business (in a neutral folder the force push is the identity guard's
// neutral-remote block, never a rule of its own), and a committed .env
// template is readable. A last case drifts the repository's git email away
// from the manifest's and pushes: the identity guard blocks.
func TestPreToolUse_SecretRules(t *testing.T) {
	creds := filepath.Join(t.TempDir(), ".aws", "credentials")
	envFile := filepath.Join(t.TempDir(), ".env")
	rows := []struct {
		name  string
		tool  string
		input string // tool_input JSON
		rule  string // the rule that blocks; "" allowed
		// neutral is the identity guard's rule in a neutral folder when it
		// differs from rule ("" means the same as rule).
		neutral string
	}{
		{"cat .env", "Bash", `{"command":"cat .env"}`, "tq/env-file-shell", ""},
		{"printenv", "Bash", `{"command":"printenv"}`, "tq/env-dump", ""},
		{"Read ~/.aws/credentials", "Read", `{"file_path":` + jsonPath(t, creds) + `}`, "tq/secret-store-read", ""},
		{"cat ~/.aws/credentials", "Bash", `{"command":"cat ~/.aws/credentials"}`, "tq/secret-store-shell", ""},
		{"Get-Content ~/.aws/credentials", "PowerShell", `{"command":"Get-Content ~/.aws/credentials"}`, "tq/secret-store-shell", ""},
		{"Grep .env", "Grep", `{"pattern":"KEY","path":` + jsonPath(t, envFile) + `}`, "tq/env-file-read", ""},
		{"Read .env.example", "Read", `{"file_path":".env.example"}`, "", ""},
		{"git push --force", "Bash", `{"command":"git push --force origin feat/x"}`, "", "neutral-remote"},
		{"terraform apply", "Bash", `{"command":"terraform apply"}`, "", ""},
	}
	run := func(t *testing.T, cwd, name, tool, input, rule string) {
		t.Helper()
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, toolPayload(t, cwd, tool, input))
		var d struct {
			Block bool   `json:"block"`
			Rule  string `json:"rule"`
		}
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Fatalf("%s: not a decision: %q (%v)", name, out, err)
		}
		switch {
		case rule == "" && (code != 0 || d.Block || d.Rule != "" || errOut != ""):
			t.Errorf("%s: want allow, got code=%d %+v stderr=%q", name, code, d, errOut)
		case rule != "" && (code != 2 || !d.Block || d.Rule != rule):
			t.Errorf("%s: want block by %s, got code=%d %+v", name, rule, code, d)
		}
	}
	t.Run("trusted workspace", func(t *testing.T) {
		// aws is in scope: a credential read outside the scope is the identity rule's, which
		// TestScope_OutOfScopePath covers; here the secret rules are what is judged.
		ws := setupTrustedWorkspaceWithManifest(t, "acme", "scope: [aws]\n")
		for _, r := range rows {
			run(t, ws, r.name, r.tool, r.input, r.rule)
		}
	})
	t.Run("neutral folder", func(t *testing.T) {
		isolateHome(t)
		dir := testutil.TempDir(t)
		for _, r := range rows {
			rule := r.rule
			if r.neutral != "" {
				rule = r.neutral
			}
			run(t, dir, r.name, r.tool, r.input, rule)
		}
	})
	t.Run("git email drift", func(t *testing.T) {
		// The manifest pins dev@acme.com; the repository's user.email is
		// t@example.com (gitRepo). The doctor reads it for the cwd, reports
		// git-email-drift, and the identity guard blocks the push with both
		// emails in the reason.
		ws := setupTrustedWorkspaceWithManifest(t, "acme", "git:\n  email: dev@acme.com\n")
		gitRepo(t, ws)
		run(t, ws, "git push with a drifted email", "Bash", `{"command":"git push origin main"}`, "git-email-drift")
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, ws, "git push origin main"))
		if !strings.Contains(out, "Git email mismatch") || !strings.Contains(out, "dev@acme.com") || !strings.Contains(out, "t@example.com") {
			t.Fatalf("the reason must name both emails: %q", out)
		}
	})
}
