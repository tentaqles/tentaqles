package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

const (
	relaxedWorkspace = "guard:\n  mode: relaxed\n"
	relaxedProject   = "guard:\n  projects:\n    demo-todo: { mode: relaxed }\n"
	legacyProject    = "guard:\n  unattended:\n    projects: [demo-todo]\n"
)

// modeWS sets up the trusted workspace acme with body and a demo-todo
// folder. grant is "" (none), "relaxed" (a .mode grant) or "bypass" (a
// legacy .bypass file).
func modeWS(t *testing.T, body, grant string) (root, proj string) {
	t.Helper()
	t.Setenv(guardcfg.EnvMode, "")
	t.Setenv("TENTAQLES_HEADLESS", "")
	root = setupTrustedWorkspaceWithManifest(t, "acme", body)
	proj = filepath.Join(root, "demo-todo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	switch grant {
	case "relaxed":
		if err := trust.SaveGrant(h, trust.Grant{Mode: "relaxed", Workspace: "acme"}); err != nil {
			t.Fatal(err)
		}
	case "bypass":
		if err := trust.AllowBypass(h); err != nil {
			t.Fatal(err)
		}
	}
	return root, proj
}

func modePayload(t *testing.T, cwd, command, mode string) string {
	t.Helper()
	p := hookPayloadFor(t, cwd, command)
	if mode == "" {
		return p
	}
	return strings.TrimSuffix(p, "}") + `,"permission_mode":` + jsonPath(t, mode) + `}`
}

func modeToolPayload(t *testing.T, cwd, tool, input, mode string) string {
	t.Helper()
	return strings.TrimSuffix(toolPayload(t, cwd, tool, input), "}") + `,"permission_mode":` + jsonPath(t, mode) + `}`
}

func silentAllow(t *testing.T, what string, code int, out string) {
	t.Helper()
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("%s: want silent allow, got code %d out %q", what, code, out)
	}
}

// Relaxed applies in every Claude permission mode (the bypass/auto coupling
// is gone, spec E §3), and the skip is recorded.
func TestRelaxedSkipsInEveryClaudeMode(t *testing.T) {
	_, proj := modeWS(t, relaxedProject, "relaxed")
	modes := []string{"bypassPermissions", "auto", "", "default", "acceptEdits", "plan"}
	for _, mode := range modes {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, mode))
		silentAllow(t, mode, code, out)
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != len(modes) || recs[0].Scope != "project:demo-todo" || recs[0].Rules[0] != "tq/sql-destructive-shell" ||
		recs[0].Mode != "relaxed" || recs[0].To != "allow" || recs[0].Action != "ask" {
		t.Fatalf("audit records = %+v", recs)
	}
}

func TestRelaxedNeedsAGrantAndCoverage(t *testing.T) {
	_, proj := modeWS(t, relaxedProject, "")
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out) // no grant: default, so it asks

	root, _ := modeWS(t, relaxedProject, "relaxed")
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, root, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out) // outside the project: the workspace layer (default)
}

// The legacy guard.unattended block and a legacy .bypass file still work:
// they read as a relaxed project and a relaxed grant.
func TestLegacyUnattendedAndBypassReadAsRelaxed(t *testing.T) {
	_, proj := modeWS(t, legacyProject, "bypass")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, ""))
	silentAllow(t, "legacy", code, out)
	// keep_asking absent: tq/iac-apply and tq/remote-migration still ask.
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "terraform apply", ""))
	if !strings.Contains(askReason(t, out), "tq/iac-apply") {
		t.Fatalf("keep_asking default: %q", out)
	}
}

func TestRelaxedGuardChangeStillAsks(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "tq guard off tq/force-push", "bypassPermissions"))
	if !strings.Contains(askReason(t, out), "tq/guard-change") {
		t.Fatalf("want tq/guard-change ask, got %q", out)
	}
}

// The locked groups (identity, secrets, self-protection, privacy) hold in
// relaxed: their denies still block and nothing is recorded as skipped.
func TestRelaxedLockedDeniesStillBlock(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	for _, cmd := range []string{"cat .env", "printenv", "cat ~/.aws/credentials", "echo x > ~/.tentaqles/guard.yaml", "echo {} > .claude/settings.json",
		"tq mode set relaxed", "tq allow --mode acme relaxed", "tq unattended on", "echo ok > ~/.tentaqles/trust/abc.mode"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, "bypassPermissions"))
		if code != 2 || !strings.Contains(errOut, "BLOCKED") {
			t.Fatalf("%q must still block: code %d stderr %q", cmd, code, errOut)
		}
	}
	for _, c := range []struct{ tool, input string }{
		{"Read", `{"file_path":` + jsonPath(t, filepath.Join(proj, ".env")) + `}`},
		{"Write", `{"file_path":` + jsonPath(t, filepath.Join(proj, ".claude", "settings.json")) + `,"content":"{\"defaultMode\":\"bypassPermissions\"}"}`},
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modeToolPayload(t, proj, c.tool, c.input, "auto"))
		if code != 2 || !strings.Contains(errOut, "BLOCKED") {
			t.Fatalf("%s %s must still block: code %d stderr %q", c.tool, c.input, code, errOut)
		}
	}
	if recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0); len(recs) != 0 {
		t.Fatalf("a locked deny is not a skip: %+v", recs)
	}
}

// Relaxed asks (instead of blocking) on git-history and system denies, and
// records the lowering; other denies are skipped.
func TestRelaxedLowersGitHistoryAndSystemDenies(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	for _, c := range []struct{ cmd, rule string }{
		{"gh repo delete acme/app --yes", "tq/gh-repo-delete"},
		{"git push --force origin main", "tq/force-push-main"},
		{"echo x > /etc/hosts", "tq/system-dir-write"},
	} {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, c.cmd, "bypassPermissions"))
		if code != 0 || !strings.Contains(askReason(t, out), c.rule) {
			t.Fatalf("%q: want an ask naming %s, got code %d out %q", c.cmd, c.rule, code, out)
		}
		recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
		if len(recs) == 0 || recs[0].Action != "deny" || recs[0].To != "ask" || !slices.Contains(recs[0].Rules, c.rule) {
			t.Fatalf("%q: audit records = %+v", c.cmd, recs)
		}
	}
	for _, cmd := range []string{"sudo id", "rm -rf /opt/tq-outside/x"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, ""))
		askReason(t, out)
	}
	wf := `{"file_path":` + jsonPath(t, filepath.Join(proj, ".github", "workflows", "ci.yml")) + `,"content":"steps:\n  - uses: actions/checkout@main\n"}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modeToolPayload(t, proj, "Write", wf, "auto"))
	silentAllow(t, "tq/gha-branch-ref", code, out)
}

// A PII rule stays enforced in relaxed, and a guard.disable of it is ignored.
func TestRelaxedKeepsPrivacy(t *testing.T) {
	_, proj := modeWS(t, piiData+"guard:\n  mode: relaxed\n  disable: [tq/text-pii]\n", "relaxed")
	cmd := `gh pr create --title "x" --body "contact tester@example.com"`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, ""))
	if code != 2 || !strings.Contains(errOut, "tq/text-pii") {
		t.Fatalf("text-pii must still block in relaxed: code %d stderr %q", code, errOut)
	}
}

// Skipping a deny uncovers an enforced ask it hid: the call is asked.
func TestModeSkippedDenyKeepsEnforcedAsk(t *testing.T) {
	_, proj := modeWS(t, "guard:\n  mode: custom\n  base: relaxed\n  categories: {git-history: skip}\n", "")
	h, _ := trust.HashFile(filepath.Join(filepath.Dir(proj), manifest.FileName))
	_ = trust.SaveGrant(h, trust.Grant{Mode: "custom", Table: map[string]string{"git-history": "skip"}})
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "git push --force origin main; tq guard off tq/force-push", "bypassPermissions"))
	if code != 0 {
		t.Fatalf("want an ask, got code %d", code)
	}
	reason := askReason(t, out)
	if !strings.Contains(reason, "tq/guard-change") || strings.Contains(reason, "tq/force-push-main") {
		t.Fatalf("reason = %q", reason)
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != 1 || recs[0].Action != "deny" || !slices.Contains(recs[0].Rules, "tq/force-push-main") {
		t.Fatalf("audit records = %+v", recs)
	}
}

// Default (nothing configured) denies as before, in every Claude mode.
func TestDefaultModeStillDenies(t *testing.T) {
	_, proj := modeWS(t, "", "relaxed")
	for _, mode := range []string{"", "default", "bypassPermissions"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "gh repo delete acme/app --yes", mode))
		if code != 2 || !strings.Contains(errOut, "tq/gh-repo-delete") {
			t.Fatalf("mode %q: want block, got code %d stderr %q", mode, code, errOut)
		}
	}
}

// Strict denies prod changes and force pushes that default only asks.
func TestStrictDenies(t *testing.T) {
	_, proj := modeWS(t, "guard:\n  mode: strict\n", "")
	for _, c := range []struct{ cmd, rule string }{
		{"terraform apply", "tq/iac-apply"},
		{"git push --force origin feat/x", "tq/force-push"},
		{"az group delete -n rg", "tq/cloud-delete"},
	} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, c.cmd, ""))
		if code != 2 || !strings.Contains(errOut, c.rule) {
			t.Fatalf("%q: want block by %s, got code %d stderr %q", c.cmd, c.rule, code, errOut)
		}
	}
	if recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0); len(recs) != 0 {
		t.Fatalf("tightening is not recorded as a skip: %+v", recs)
	}
}

// The launch override (TQ_MODE in the hook's own env) beats the manifest,
// and is clamped by the grant like every layer; a TQ_MODE in the command
// line changes nothing.
func TestLaunchOverride(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	t.Setenv(guardcfg.EnvMode, "strict")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "terraform apply", ""))
	if code != 2 || !strings.Contains(errOut, "tq/iac-apply") {
		t.Fatalf("launch strict: code %d stderr %q", code, errOut)
	}
	_, proj2 := modeWS(t, "", "")
	t.Setenv(guardcfg.EnvMode, "relaxed")
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj2, `psql -c "drop table t"`, ""))
	askReason(t, out) // relaxed at launch without a grant runs at default
	t.Setenv(guardcfg.EnvMode, "")
	code, _, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj2, "TQ_MODE=relaxed gh repo delete acme/app --yes", ""))
	if code != 2 {
		t.Fatal("TQ_MODE in a command line never loosens the hook")
	}
}

// A headless run with no TQ_MODE runs at headless_mode (default), not the
// workspace's interactive mode.
func TestHeadlessUsesHeadlessMode(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	t.Setenv("TENTAQLES_HEADLESS", "1")
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, ""))
	askReason(t, out)
}

// The identity guard (blocked_commands and the rest) runs before the rules
// and is never skipped.
func TestRelaxedKeepsBlockedCommands(t *testing.T) {
	_, proj := modeWS(t, "blocked_commands: [\"npm publish\"]\n"+relaxedWorkspace, "relaxed")
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "npm publish", "bypassPermissions"))
	if code != 2 || !strings.Contains(errOut, "blocked by client manifest") {
		t.Fatalf("blocked_commands must still block: code %d stderr %q", code, errOut)
	}
}

// A custom rule follows its declared category; without one (or with the
// deprecated unattended key) it is always applied.
func TestManifestRuleCategory(t *testing.T) {
	body := relaxedWorkspace + "  rules:\n" +
		"    - id: acme/no-foo\n      action: deny\n      command: '\\bfoo\\b'\n      reason: no foo\n      category: hygiene\n" +
		"    - id: acme/no-bar\n      action: deny\n      command: '\\bbar\\b'\n      reason: no bar\n" +
		"    - id: acme/no-baz\n      action: deny\n      command: '\\bbaz\\b'\n      reason: no baz\n      unattended: enforce\n      category: hygiene\n"
	_, proj := modeWS(t, body, "relaxed")
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "foo", ""))
	silentAllow(t, "acme/no-foo (hygiene)", code, out)
	for _, id := range []string{"bar", "baz"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, id, ""))
		if code != 2 || !strings.Contains(errOut, "acme/no-"+id) {
			t.Fatalf("acme/no-%s: want block, got code %d stderr %q", id, code, errOut)
		}
	}
}

func TestModeJSONReportsSkipAndLowering(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	var v struct {
		Block  bool   `json:"block"`
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, `psql -c "drop table t"`, "auto"))
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != 0 || v.Block || v.Rule != "tq/sql-destructive-shell" || !strings.HasPrefix(v.Reason, "mode relaxed: SKIPPED [tq/sql-destructive-shell]") {
		t.Fatalf("code %d json = %q (%v)", code, out, err)
	}
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, "gh repo delete acme/app --yes", "auto"))
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Block || v.Rule != "tq/gh-repo-delete" || !strings.HasPrefix(v.Reason, "CONFIRM") {
		t.Fatalf("lowered deny is an ask: %q (%v)", out, err)
	}
}

func wsFor(t *testing.T, root string) *resolve.Workspace {
	t.Helper()
	mp := filepath.Join(root, manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func TestSkippedReasonAndCounts(t *testing.T) {
	isolateHome(t)
	ms := []policy.Mapped{
		{Rule: &policy.Rule{ID: "tq/a", Action: policy.Deny, Reason: "r1", Source: "builtin"}, To: policy.Ask},
		{Rule: &policy.Rule{ID: "tq/b", Action: policy.Ask, Reason: "r2", Source: "builtin"}, To: policy.Allow},
	}
	want := "LOWERED [tq/a]: r1 (was a block, now a confirm; source: builtin)\nSKIPPED [tq/b]: r2 (was a confirm; source: builtin)"
	if got := skippedReason(ms); got != want {
		t.Fatalf("got %q", got)
	}
	now := time.Now()
	guardlog.Append(guardlog.Record{WS: "acme", Action: "ask", Rules: []string{"x"}}, nil)
	guardlog.Append(guardlog.Record{WS: "acme", Rules: []string{"x"}}, nil) // pre-deny records: a confirm
	guardlog.Append(guardlog.Record{WS: "acme", Action: "deny", To: "ask", Rules: []string{"y"}}, nil)
	if got := skippedCounts("acme", now); got != "2 confirms and 1 block" {
		t.Fatalf("got %q", got)
	}
}

func writeMigration(t *testing.T, dir string) string {
	t.Helper()
	d := filepath.Join(dir, "supabase", "migrations")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, "0001.sql")
	if err := os.WriteFile(p, []byte("create table t (id int);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A file tool's target must be inside the project for the project's mode to
// apply; an edit elsewhere falls back to the workspace layer.
func TestModeFileTargetMustBeCovered(t *testing.T) {
	root, proj := modeWS(t, relaxedProject, "relaxed")
	outside := writeMigration(t, filepath.Join(root, "app"))
	inside := writeMigration(t, proj)
	edit := func(p string) string {
		return `{"file_path":` + jsonPath(t, p) + `,"old_string":"int","new_string":"bigint"}`
	}
	rel, err := filepath.Rel(proj, outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, rel} {
		for _, tool := range []string{"Edit", "Write"} {
			_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modeToolPayload(t, proj, tool, edit(p), "bypassPermissions"))
			if !strings.Contains(askReason(t, out), "tq/migration-edit") {
				t.Fatalf("%s %s outside demo-todo: want ask, got %q", tool, p, out)
			}
		}
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modeToolPayload(t, proj, "Edit", edit(inside), "bypassPermissions"))
	silentAllow(t, "inside demo-todo", code, out)
}

func askDecision(id string) policy.Decision {
	return policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: id, Action: policy.Ask}}}
}

func TestModeGateNeedsTrustedWorkspace(t *testing.T) {
	root, proj := modeWS(t, relaxedWorkspace, "relaxed")
	ws := wsFor(t, root)
	call := policy.ToolCall{Tool: "Bash", Command: "terraform destroy"}
	g := modeGateFor(ws, proj, call, "")
	if g == nil || g.apply(askDecision("tq/iac-destroy")).Action != policy.Allow {
		t.Fatal("trusted + granted must skip")
	}
	// The grant is left behind but the manifest is no longer trusted.
	if err := trust.Deny(ws.Hash); err != nil {
		t.Fatal(err)
	}
	_ = trust.SaveGrant(ws.Hash, trust.Grant{Mode: "relaxed"})
	if g := modeGateFor(ws, proj, call, ""); g != nil {
		t.Fatalf("an untrusted workspace never loosens: %+v", g.res)
	}
	var none *modeGate
	if d := none.apply(askDecision("tq/iac-destroy")); d.Action != policy.Ask || none.audit("", call, d) != nil || none.mapFunc() != nil {
		t.Fatal("a nil gate keeps every rule")
	}
	if none.resolved().Mode != guardcfg.ModeDefault {
		t.Fatal("a nil gate is default")
	}
}

// An untrusted workspace (an edited manifest, a new client folder) never
// takes the global layer's grant: a global relaxed with its grant only
// loosens neutral folders and workspaces that hold a grant of their own
// (spec E §4.2, §6.3).
func TestUntrustedWorkspaceIgnoresGlobalGrant(t *testing.T) {
	root, proj := modeWS(t, relaxedWorkspace, "relaxed")
	ws := wsFor(t, root)
	g := guardcfg.File{Mode: guardcfg.ModeRelaxed}
	if err := guardcfg.Save(g); err != nil {
		t.Fatal(err)
	}
	gr, ok := guardcfg.GlobalGrantFor(g, guardcfg.ModeRelaxed)
	if !ok {
		t.Fatal("no global grant")
	}
	if err := trust.SaveGrant(trust.GlobalGrantKey, gr); err != nil {
		t.Fatal(err)
	}
	call := policy.ToolCall{Tool: "Bash", Command: "terraform destroy"}
	if g := modeGateFor(nil, t.TempDir(), call, ""); g == nil || g.apply(askDecision("tq/iac-destroy")).Action != policy.Allow {
		t.Fatal("a neutral folder takes the granted global relaxed")
	}
	// The manifest is edited: its hash moves and it is no longer trusted.
	if err := trust.Deny(ws.Hash); err != nil {
		t.Fatal(err)
	}
	if g := modeGateFor(ws, proj, call, ""); g != nil && g.apply(askDecision("tq/iac-destroy")).Action != policy.Ask {
		t.Fatalf("an untrusted workspace must not run at the global relaxed: %+v", g.res)
	}
	if r := resolveModeFor(ws, proj, "", guardcfg.ModeRelaxed); r.Ceiling != nil || r.Mode != guardcfg.ModeDefault {
		t.Fatalf("an untrusted workspace has the default ceiling: %+v", r)
	}
	// Strict still applies there: it needs no grant.
	if err := guardcfg.Save(guardcfg.File{Mode: guardcfg.ModeStrict}); err != nil {
		t.Fatal(err)
	}
	if r := resolveModeFor(ws, proj, "", ""); r.Mode != guardcfg.ModeStrict {
		t.Fatalf("a global strict still tightens an untrusted workspace: %+v", r)
	}
}

func TestModeAuditsMCPContent(t *testing.T) {
	root, proj := modeWS(t, relaxedWorkspace, "relaxed")
	ws := wsFor(t, root)
	call := policy.ToolCall{Tool: "mcp__postgres__query", Content: `{"sql":"DELETE FROM orders WHERE id=1"}`}
	g := modeGateFor(ws, proj, call, "")
	d := g.apply(askDecision("tq/mcp-sql-write"))
	if d.Action != policy.Allow {
		t.Fatal("want skip")
	}
	if got := g.audit("auto", call, d); len(got) != 1 {
		t.Fatalf("skipped = %v", got)
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != 1 || !strings.Contains(recs[0].Command, "DELETE FROM orders") || recs[0].Scope != "workspace" || recs[0].ClaudeMode != "auto" {
		t.Fatalf("audit records = %+v", recs)
	}
}

// A repo .claude/tq-rules.yaml can tighten the mode but never loosen it.
func TestRepoFileTightensOnly(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "tq-rules.yaml"), []byte("categories:\n  db-destructive: ask\n  system: skip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, ""))
	askReason(t, out)
	_, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "sudo id", ""))
	askReason(t, out) // system skip ignored
	res := resolveMode(wsFor(t, filepath.Dir(proj)), proj, "")
	if !slices.Contains(res.Ignored, "system") {
		t.Fatalf("the looser repo value is listed: %+v", res.Ignored)
	}
}

// tq's trust store and registry stay protected in relaxed: a write (which
// could add a grant) blocks, a read is allowed.
func TestRelaxedTrustStoreWriteBlocks(t *testing.T) {
	_, proj := modeWS(t, relaxedWorkspace, "relaxed")
	for _, cmd := range []string{"echo ok > ~/.tentaqles/trust/abc.mode", "rm $TQ_HOME/config.yaml"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, "bypassPermissions"))
		if code != 2 || !strings.Contains(errOut, "tq/trust-store-write") {
			t.Fatalf("%q: want block, got code %d stderr %q", cmd, code, errOut)
		}
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "cat ~/.tentaqles/config.yaml", "bypassPermissions"))
	silentAllow(t, "read", code, out)
}

// End to end through the hook: an edited (untrusted), invalid or deleted
// manifest never runs at the granted global relaxed, on the file-tool and
// MCP path or the shell path (final security review M1). The global grant
// is for folders outside every workspace only.
func TestHookUntrustedOrBrokenManifestIgnoresGlobalGrant(t *testing.T) {
	root, proj := modeWS(t, "", "")
	g := guardcfg.File{Mode: guardcfg.ModeRelaxed}
	if err := guardcfg.Save(g); err != nil {
		t.Fatal(err)
	}
	gr, ok := guardcfg.GlobalGrantFor(g, guardcfg.ModeRelaxed)
	if !ok {
		t.Fatal("no global grant")
	}
	if err := trust.SaveGrant(trust.GlobalGrantKey, gr); err != nil {
		t.Fatal(err)
	}
	mcp := `{"tool_name":"mcp__github__create_issue","cwd":` + jsonPath(t, proj) + `,"tool_input":{"title":"x"}}`
	notAllowed := func(what, payload string) {
		t.Helper()
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
		if code != 2 && !strings.Contains(out, `"permissionDecision":"ask"`) {
			t.Fatalf("%s: allowed at the global relaxed (code %d, out %q)", what, code, out)
		}
	}
	// Sanity: a trusted manifest without a mode stays at default (asks).
	notAllowed("trusted default", mcp)
	mp := filepath.Join(root, manifest.FileName)
	raw, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, append(raw, []byte("# edited\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	notAllowed("untrusted, mcp", mcp)
	mig := filepath.Join(proj, "migrations", "001_init.sql")
	if err := os.MkdirAll(filepath.Dir(mig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mig, []byte("create table x();\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notAllowed("untrusted, edit", `{"tool_name":"Edit","cwd":`+jsonPath(t, proj)+`,"tool_input":{"file_path":`+jsonPath(t, mig)+`,"old_string":"x","new_string":"y"}}`)
	if err := os.WriteFile(mp, []byte("guard: [not: valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notAllowed("invalid, mcp", mcp)
	notAllowed("invalid, shell", modePayload(t, proj, "terraform destroy", ""))
	if err := os.Remove(mp); err != nil {
		t.Fatal(err)
	}
	notAllowed("deleted, mcp", mcp)
	notAllowed("deleted, shell", modePayload(t, proj, "terraform destroy", ""))
	// A folder outside every base still takes the global grant.
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, `{"tool_name":"mcp__github__create_issue","cwd":`+jsonPath(t, t.TempDir())+`,"tool_input":{"title":"x"}}`)
	if code != 0 || strings.Contains(out, `"permissionDecision"`) {
		t.Fatalf("a neutral folder runs at the granted global relaxed: code %d out %q", code, out)
	}
}
