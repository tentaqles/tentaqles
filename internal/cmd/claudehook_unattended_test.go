package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

const unattendedProject = "guard:\n  unattended:\n    projects: [demo-todo]\n"

func unattendedWS(t *testing.T, body string, bypass bool) (root, proj string) {
	t.Helper()
	root = setupTrustedWorkspaceWithManifest(t, "acme", body)
	proj = filepath.Join(root, "demo-todo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if bypass {
		h, err := trust.HashFile(filepath.Join(root, manifest.FileName))
		if err != nil {
			t.Fatal(err)
		}
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

func TestUnattendedSkipsConfirmInBypassAndAuto(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	for _, mode := range []string{"bypassPermissions", "auto"} {
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, mode))
		if code != 0 || strings.TrimSpace(out) != "" {
			t.Fatalf("%s: want silent allow, got code %d out %q", mode, code, out)
		}
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != 2 || recs[0].Scope != "demo-todo" || recs[0].Rules[0] != "tq/sql-destructive-shell" {
		t.Fatalf("audit records = %+v", recs)
	}
}

func TestUnattendedAsksInOtherModes(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	for _, mode := range []string{"", "default", "acceptEdits", "plan"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, mode))
		if !strings.Contains(askReason(t, out), "tq/sql-destructive-shell") {
			t.Fatalf("mode %q: want ask, got %q", mode, out)
		}
	}
}

func TestUnattendedNeedsBypassTrustAndCoverage(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, false)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out) // fails the test if it is not an ask
}

func TestUnattendedOutsideProjectAsks(t *testing.T) {
	root, _ := unattendedWS(t, unattendedProject, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, root, `psql -c "drop table t"`, "bypassPermissions"))
	askReason(t, out)
}

func TestUnattendedKeepAskingAndGuardChangeStillAsk(t *testing.T) {
	_, proj := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	for _, cmd := range []string{"terraform apply", "tq guard off tq/force-push"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, "bypassPermissions"))
		askReason(t, out)
	}
}

func TestUnattendedGuardChangeStillAsks(t *testing.T) {
	empty := "guard:\n  unattended:\n    workspace: true\n    keep_asking: []\n"
	_, proj := unattendedWS(t, empty, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "tq guard off tq/force-push", "bypassPermissions"))
	if !strings.Contains(askReason(t, out), "tq/guard-change") {
		t.Fatalf("want tq/guard-change ask, got %q", out)
	}
}

const unattendedAll = "guard:\n  unattended:\n    workspace: true\n    keep_asking: []\n"

// Secret, identity and self-protection denies stay enforced in unattended
// sessions.
func TestUnattendedEnforcedDeniesStillBlock(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
	for _, cmd := range []string{"cat .env", "printenv", "cat ~/.aws/credentials", "echo x > ~/.tentaqles/guard.yaml", "echo {} > .claude/settings.json"} {
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
		t.Fatalf("an enforced deny is not a skip: %+v", recs)
	}
}

// Every other deny is skipped in an unattended session, and recorded.
func TestUnattendedSkipsOtherDenies(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
	for _, c := range []struct{ cmd, rule string }{
		{"gh repo delete acme/app --yes", "tq/gh-repo-delete"},
		{"git push --force origin main", "tq/force-push-main"},
	} {
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, c.cmd, "bypassPermissions"))
		if code != 0 || strings.TrimSpace(out) != "" {
			t.Fatalf("%q: want silent allow, got code %d out %q err %q", c.cmd, code, out, errOut)
		}
		recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
		if len(recs) == 0 || recs[0].Action != "deny" || !slices.Contains(recs[0].Rules, c.rule) {
			t.Fatalf("%q: audit records = %+v", c.cmd, recs)
		}
	}
	wf := `{"file_path":` + jsonPath(t, filepath.Join(proj, ".github", "workflows", "ci.yml")) + `,"content":"steps:\n  - uses: actions/checkout@main\n"}`
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modeToolPayload(t, proj, "Write", wf, "auto"))
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("tq/gha-branch-ref: want silent allow, got code %d out %q", code, out)
	}
}

// Skipping a deny uncovers an enforced ask it hid: the call is asked.
func TestUnattendedSkippedDenyKeepsEnforcedAsk(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
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

// Outside unattended mode, denies block as before.
func TestUnattendedOffStillDenies(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
	for _, mode := range []string{"", "default", "acceptEdits"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "gh repo delete acme/app --yes", mode))
		if code != 2 || !strings.Contains(errOut, "tq/gh-repo-delete") {
			t.Fatalf("mode %q: want block, got code %d stderr %q", mode, code, errOut)
		}
	}
}

// The identity guard (blocked_commands and the rest) runs before the rules
// and is never skipped.
func TestUnattendedKeepsBlockedCommands(t *testing.T) {
	_, proj := unattendedWS(t, "blocked_commands: [\"npm publish\"]\n"+unattendedAll, true)
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "npm publish", "bypassPermissions"))
	if code != 2 || !strings.Contains(errOut, "blocked by client manifest") {
		t.Fatalf("blocked_commands must still block: code %d stderr %q", code, errOut)
	}
}

// Manifest rules are skipped unless they set unattended: enforce.
func TestUnattendedManifestRuleOptIn(t *testing.T) {
	body := unattendedAll + "  rules:\n" +
		"    - id: acme/no-foo\n      action: deny\n      command: '\\bfoo\\b'\n      reason: no foo\n" +
		"    - id: acme/no-bar\n      action: deny\n      command: '\\bbar\\b'\n      reason: no bar\n      unattended: enforce\n"
	_, proj := unattendedWS(t, body, true)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "foo", "auto"))
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("acme/no-foo: want silent allow, got code %d out %q", code, out)
	}
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "bar", "auto"))
	if code != 2 || !strings.Contains(errOut, "acme/no-bar") {
		t.Fatalf("acme/no-bar: want block, got code %d stderr %q", code, errOut)
	}
	code, _, errOut = runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "foo", "default"))
	if code != 2 || !strings.Contains(errOut, "acme/no-foo") {
		t.Fatalf("acme/no-foo outside unattended: want block, got code %d stderr %q", code, errOut)
	}
}

func TestUnattendedJSONReportsSkippedDeny(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, "gh repo delete acme/app --yes", "auto"))
	var v struct {
		Block  bool   `json:"block"`
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != 0 || v.Block || v.Rule != "tq/gh-repo-delete" || !strings.HasPrefix(v.Reason, "unattended: BLOCKED") {
		t.Fatalf("code %d json = %q (%v)", code, out, err)
	}
}

func TestUnattendedJSONReportsSkip(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, modePayload(t, proj, `psql -c "drop table t"`, "auto"))
	var v struct {
		Block  bool   `json:"block"`
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.Block || v.Rule != "tq/sql-destructive-shell" || !strings.HasPrefix(v.Reason, "unattended: ") {
		t.Fatalf("json = %q (%v)", out, err)
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

func TestUnattendedLine(t *testing.T) {
	root, proj := unattendedWS(t, unattendedProject, true)
	ws := wsFor(t, root)
	now := time.Now()
	guardlog.Append(guardlog.Record{WS: "acme", Scope: "demo-todo", Mode: "auto", Action: "ask", Rules: []string{"x"}, Tool: "Bash", Command: "c"}, nil)
	if got, want := unattendedLine(ws, proj, now), "Unattended: on (demo-todo) · 1 confirm and 0 blocks skipped in 24h · applies in bypass and auto sessions"; got != want {
		t.Fatalf("got %q", got)
	}
	// A record from before skipped denies existed has no action: a confirm.
	guardlog.Append(guardlog.Record{WS: "acme", Scope: "demo-todo", Mode: "auto", Rules: []string{"x"}, Tool: "Bash", Command: "c"}, nil)
	guardlog.Append(guardlog.Record{WS: "acme", Scope: "demo-todo", Mode: "auto", Action: "deny", Rules: []string{"y"}, Tool: "Bash", Command: "c"}, nil)
	if got, want := unattendedLine(ws, proj, now), "Unattended: on (demo-todo) · 2 confirms and 1 block skipped in 24h · applies in bypass and auto sessions"; got != want {
		t.Fatalf("got %q", got)
	}
	if got, want := unattendedLine(ws, root, now), "Unattended: on for demo-todo (not this folder)"; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestUnattendedLineNotEffectiveAndUnset(t *testing.T) {
	root, proj := unattendedWS(t, unattendedProject, false)
	if got, want := unattendedLine(wsFor(t, root), proj, time.Now()), "Unattended: configured, not effective (needs tq allow --bypass acme)"; got != want {
		t.Fatalf("got %q", got)
	}
	root2, _ := unattendedWS(t, "", false)
	if got := unattendedLine(wsFor(t, root2), root2, time.Now()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestSessionStartShowsUnattended(t *testing.T) {
	_, proj := unattendedWS(t, unattendedProject, true)
	code, out, _ := runHook(t, []string{"claude-hook", "session-start"}, hookPayloadFor(t, proj, ""))
	if code != 0 || !strings.Contains(out, "Unattended: on") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func modeToolPayload(t *testing.T, cwd, tool, input, mode string) string {
	t.Helper()
	return strings.TrimSuffix(toolPayload(t, cwd, tool, input), "}") + `,"permission_mode":` + jsonPath(t, mode) + `}`
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

func TestUnattendedFileTargetMustBeCovered(t *testing.T) {
	root, proj := unattendedWS(t, unattendedProject, true)
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
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("inside demo-todo: want silent allow, got code %d out %q", code, out)
	}
}

func askDecision(id string) policy.Decision {
	return policy.Decision{Action: policy.Ask, Matched: []*policy.Rule{{ID: id, Action: policy.Ask}}}
}

func TestUnattendedSkipNeedsTrustedWorkspace(t *testing.T) {
	root, proj := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	ws := wsFor(t, root)
	call := policy.ToolCall{Tool: "Bash", Command: "git push --force origin feat/x"}
	g := unattendedGateFor(ws, proj, "auto", call)
	if g == nil || g.filter(askDecision("tq/force-push")).Action != policy.Allow {
		t.Fatal("trusted + bypass must skip")
	}
	// Bypass marker left behind but the manifest is no longer trusted.
	if err := trust.Deny(ws.Hash); err != nil {
		t.Fatal(err)
	}
	if err := trust.AllowBypass(ws.Hash); err != nil {
		t.Fatal(err)
	}
	if g := unattendedGateFor(ws, proj, "auto", call); g != nil {
		t.Fatal("an untrusted workspace must never skip")
	}
	var none *unattendedGate
	if d := none.filter(askDecision("tq/force-push")); d.Action != policy.Ask {
		t.Fatal("a nil gate keeps every rule")
	}
}

func TestUnattendedSkipAuditsMCPContent(t *testing.T) {
	root, proj := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	ws := wsFor(t, root)
	call := policy.ToolCall{Tool: "mcp__postgres__query", Content: `{"sql":"DELETE FROM orders WHERE id=1"}`}
	g := unattendedGateFor(ws, proj, "auto", call)
	d := g.filter(askDecision("tq/mcp-sql-write"))
	if d.Action != policy.Allow {
		t.Fatal("want skip")
	}
	if got := g.audit(ws, "auto", call, d); len(got) != 1 {
		t.Fatalf("skipped = %v", got)
	}
	recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0)
	if len(recs) != 1 || !strings.Contains(recs[0].Command, "DELETE FROM orders") {
		t.Fatalf("audit records = %+v", recs)
	}
}

func TestUnattendedLineWorkspaceNotThisFolder(t *testing.T) {
	root, _ := unattendedWS(t, "guard:\n  unattended:\n    workspace: true\n", true)
	if got, want := unattendedLine(wsFor(t, root), t.TempDir(), time.Now()), "Unattended: on for the workspace (not this folder)"; got != want {
		t.Fatalf("got %q", got)
	}
}

// tq's trust store and registry stay protected in unattended sessions: a
// write (which could add a bypass marker) blocks, a read is allowed.
func TestUnattendedTrustStoreWriteBlocks(t *testing.T) {
	_, proj := unattendedWS(t, unattendedAll, true)
	for _, cmd := range []string{"echo ok > ~/.tentaqles/trust/abc.bypass", "rm $TQ_HOME/config.yaml"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, cmd, "bypassPermissions"))
		if code != 2 || !strings.Contains(errOut, "tq/trust-store-write") {
			t.Fatalf("%q: want block, got code %d stderr %q", cmd, code, errOut)
		}
	}
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, modePayload(t, proj, "cat ~/.tentaqles/config.yaml", "bypassPermissions"))
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("read: want silent allow, got code %d out %q", code, out)
	}
	if recs := guardlog.Read("acme", time.Now().Add(-time.Minute), 0); len(recs) != 0 {
		t.Fatalf("nothing was skipped: %+v", recs)
	}
}
