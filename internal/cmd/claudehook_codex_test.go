package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
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

// renderedCodexHome renders tq's files for a trusted acme workspace that
// isolates codex and returns the workspace and its CODEX_HOME.
func renderedCodexHome(t *testing.T) (*resolve.Workspace, string) {
	t.Helper()
	w := codexWS(t)
	dir := paths.IdentityDir(w.Name, "codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	renderOut(t, w, false)
	if _, err := codexcfg.ReadRules(dir); err != nil {
		t.Fatalf("render wrote no rules file: %v", err)
	}
	return w, dir
}

func askOn(id string) policy.Decision {
	return policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: id, Action: policy.Ask, Reason: "because", Source: "builtin"}}}
}

func checkCodexAnswer(t *testing.T, call policy.ToolCall, d policy.Decision, ctx codexHookCtx, want string) {
	t.Helper()
	var b bytes.Buffer
	if code := codexAnswer(&b, call, d, ctx); code != 0 {
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
}

func TestCodexAnswerNeverAsks(t *testing.T) {
	w, home := renderedCodexHome(t)
	ok := codexHookCtx{CodexHome: home, GOOS: "linux", PermissionMode: "default", WS: w, Cwd: w.Root}
	with := func(f func(*codexHookCtx)) codexHookCtx { c := ok; f(&c); return c }
	bash := policy.ToolCall{Tool: "Bash", Command: "terraform apply"}
	cases := []struct {
		name string
		call policy.ToolCall
		d    policy.Decision
		ctx  codexHookCtx
		want string // "" = no output (Codex continues and its own rule prompts)
	}{
		{"deny stays deny", bash, policy.Decision{Action: policy.Deny, Matched: []*policy.Rule{{ID: "tq/env-file-shell", Reason: "x"}}}, ok, "deny"},
		{"covered shell ask is left to Codex's prompt rule", bash, askOn("tq/iac-apply"), ok, ""},
		{"acceptEdits prompts too", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.PermissionMode = "acceptEdits" }), ""},
		{"unknown permission mode", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.PermissionMode = "" }), "deny"},
		{"unlisted permission mode", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.PermissionMode = "dontAsk" }), "deny"},
		{"bypass session", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.PermissionMode = "bypassPermissions" }), "deny"},
		{"no workspace", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.WS = nil }), "deny"},
		{"CODEX_HOME is not the identity dir", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.CodexHome = filepath.Join(t.TempDir(), "none") }), "deny"},
		{"ask without a prefix rule", bash, askOn("tq/cloud-delete"), ok, "deny"},
		{"one of two asks uncovered", bash, policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: "tq/iac-apply"}, {ID: "tq/outside-path-write"}}}, ok, "deny"},
		{"complex script", policy.ToolCall{Tool: "Bash", Command: "terraform apply > log"}, askOn("tq/iac-apply"), ok, "deny"},
		{"a chain is never left to Codex (A11)", policy.ToolCall{Tool: "Bash", Command: "cd infra && terraform apply"}, askOn("tq/iac-apply"), ok, "deny"},
		{"apply_patch ask", policy.ToolCall{Tool: "apply_patch", Command: "terraform apply"}, askOn("tq/iac-apply"), ok, "deny"},
		{"mcp ask", policy.ToolCall{Tool: "mcp__github__create_issue"}, askOn("tq/mcp-github-write"), ok, "deny"},
		{"windows shell", bash, askOn("tq/iac-apply"), with(func(c *codexHookCtx) { c.GOOS = "windows" }), "deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { checkCodexAnswer(t, tc.call, tc.d, tc.ctx, tc.want) })
	}
}

// codexHookOutput is the call site WS1's hook uses today: the permission
// mode and workspace are unknown there, so every ask is a deny even when
// tq's rules file would cover it (review r0, Major 1).
func TestCodexHookOutputDeniesEveryAsk(t *testing.T) {
	_, home := renderedCodexHome(t)
	t.Setenv("CODEX_HOME", home)
	prev := codexPromptOS
	codexPromptOS = "linux"
	t.Cleanup(func() { codexPromptOS = prev })
	var b bytes.Buffer
	if code := codexHookOutput(&b, policy.ToolCall{Tool: "Bash", Command: "terraform apply"}, askOn("tq/iac-apply")); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if dec, _ := codexAnswerJSON(t, b.String()); dec != "deny" {
		t.Fatalf("unknown permission mode must deny: %q", b.String())
	}
}

// An agent that plants one of tq's curated lines in rules/tq.rules cannot
// make the hook let the call through: the file no longer equals what tq
// renders (review r0, Major 2).
func TestCodexAnswerRulesFileTampered(t *testing.T) {
	w, home := renderedCodexHome(t)
	ctx := codexHookCtx{CodexHome: home, GOOS: "linux", PermissionMode: "default", WS: w, Cwd: w.Root}
	rp := filepath.Join(home, filepath.FromSlash(codexcfg.RulesFile))
	orig, err := os.ReadFile(rp)
	if err != nil {
		t.Fatal(err)
	}
	planted := codexcfg.RenderRules("default", []codexcfg.PrefixRule{{ID: "tq/x", Decision: codexcfg.DecisionPrompt, Pattern: codexcfg.Prefixes("tq/force-push")[0]}})
	line := strings.Split(planted, "\n")[2]
	tampered := strings.Replace(string(orig), "# <<< managed by tq", line+"\n# <<< managed by tq", 1)
	if err := os.WriteFile(rp, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	checkCodexAnswer(t, policy.ToolCall{Tool: "Bash", Command: "terraform apply"}, askOn("tq/iac-apply"), ctx, "deny")
	if err := os.WriteFile(rp, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkCodexAnswer(t, policy.ToolCall{Tool: "Bash", Command: "terraform apply"}, askOn("tq/iac-apply"), ctx, "deny")
}

// relaxed with a danger-full-access sandbox writes no prompt lines. A file
// that holds one anyway (an agent outside the sandbox wrote it) is a
// mismatch, and the plan alone never covers the ask either.
func TestCodexAnswerFullAccessSandboxDenies(t *testing.T) {
	w, home := renderedCodexHome(t)
	w.Manifest.Guard.Codex = &manifest.GuardCodex{Sandbox: codexcfg.SandboxFull}
	withCodexMode(t, "relaxed", "")
	renderOut(t, w, false)
	plan := codexFilePlan(w)
	if !plan.Bypass {
		t.Fatalf("relaxed + danger-full-access must be bypass: %+v", plan)
	}
	ctx := codexHookCtx{CodexHome: home, GOOS: "linux", PermissionMode: "default", WS: w, Cwd: w.Root}
	push := policy.ToolCall{Tool: "Bash", Command: "git push --force"}
	checkCodexAnswer(t, push, askOn("tq/force-push"), ctx, "deny")

	rules := append([]codexcfg.PrefixRule{}, plan.Rules...)
	rules = append(rules, codexcfg.PrefixRule{ID: "tq/force-push", Decision: codexcfg.DecisionPrompt, Pattern: codexcfg.Prefixes("tq/force-push")[0]})
	rp := filepath.Join(home, filepath.FromSlash(codexcfg.RulesFile))
	if err := os.WriteFile(rp, []byte(codexcfg.RenderRules(plan.Mode, rules)), 0o600); err != nil {
		t.Fatal(err)
	}
	checkCodexAnswer(t, push, askOn("tq/force-push"), ctx, "deny")
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

// End to end through the hook: the live call site does not know the
// permission mode, so even an ask a prompt rule in CODEX_HOME would cover is
// a deny; never an ask, never empty.
func TestPreToolUseAgentCodexAsksDeny(t *testing.T) {
	// A valid manifest that isolates codex (the setup helper already writes
	// an identities key, so the body must not add a second one).
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	mp := filepath.Join(root, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {}, codex: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(manifestHash(t, root)); err != nil {
		t.Fatal(err)
	}
	if w, _ := resolveTrusted(root); w == nil || !codexIsolated(w) {
		t.Fatal("the fixture must resolve to a trusted workspace that isolates codex")
	}
	dir := paths.IdentityDir("acme", "codex")
	t.Setenv("CODEX_HOME", dir)
	prev := codexPromptOS
	codexPromptOS = "linux"
	t.Cleanup(func() { codexPromptOS = prev })
	for _, cmd := range []string{"terraform apply", "terraform -chdir=infra apply"} {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, hookPayloadFor(t, root, cmd))
		if dec, _ := codexAnswerJSON(t, out); code != 0 || dec != "deny" {
			t.Fatalf("%s: %d %q", cmd, code, out)
		}
	}
}
