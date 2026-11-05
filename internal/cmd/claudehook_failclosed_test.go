package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// PRD v2 §6.3 and §14 (Phase 2 gate): no ask verdict exists and the guard
// fails closed. These tests pin both.

// wantGuardErrorDeny runs payload through the hook for both agents and
// requires a deny of tq/guard-error whose reason contains sub.
func wantGuardErrorDeny(t *testing.T, name, payload, sub string) {
	t.Helper()
	code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, payload)
	if code != 2 || out != "" || !strings.Contains(errOut, "BLOCKED [tq/guard-error]: ") || !strings.Contains(errOut, sub) {
		t.Errorf("%s (claude): code=%d out=%q err=%q, want a tq/guard-error deny naming %q", name, code, out, errOut, sub)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, payload)
	if dec, reason := codexAnswerJSON(t, out); code != 2 || dec != "deny" || !strings.Contains(reason, sub) {
		t.Errorf("%s (codex): code=%d out=%q", name, code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, payload)
	if code != 2 || !strings.Contains(out, `"block":true`) || !strings.Contains(out, `"rule":"tq/guard-error"`) {
		t.Errorf("%s (--json): code=%d out=%q", name, code, out)
	}
}

// A payload tq cannot read is denied, whatever is wrong with it.
func TestPreToolUse_MalformedPayloadDenies(t *testing.T) {
	isolateHome(t)
	for name, payload := range map[string]string{
		"not json":        `not json`,
		"truncated":       `{"tool_name":"Bash","tool_input":{"command":"git push"`,
		"array":           `[{"tool_name":"Bash"}]`,
		"string":          `"Bash"`,
		"wrong type":      `{"tool_name":7}`,
		"empty":           ``,
		"null":            `null`,
		"no tool":         `{"cwd":"/tmp","tool_input":{"command":"git push"}}`,
		"empty tool name": `{"tool_name":"","tool_input":{"command":"git push"}}`,
	} {
		sub := "the hook payload"
		wantGuardErrorDeny(t, name, payload, sub)
	}
}

// An oversized payload is denied (it was before; still is).
func TestPreToolUse_OversizedPayloadDenies(t *testing.T) {
	isolateHome(t)
	big := `{"tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("a", maxHookPayload) + `"}}`
	wantGuardErrorDeny(t, "oversized", big, "exceeds 1 MiB")
}

// A decision that misses the hook deadline is denied before the harness's
// own hook timeout would let the call run.
func TestPreToolUse_TimeoutDenies(t *testing.T) {
	isolateHome(t)
	prevD, prevP := hookDeadline, hookPolicyDecision
	hookDeadline = 100 * time.Millisecond
	release := make(chan struct{})
	hookPolicyDecision = func(string, *resolve.Workspace, *registry.Config, policy.ToolCall, time.Time) policy.Decision {
		<-release
		return policy.Decision{}
	}
	t.Cleanup(func() { close(release); hookDeadline, hookPolicyDecision = prevD, prevP })
	dir := testutil.TempDir(t)
	start := time.Now()
	wantGuardErrorDeny(t, "stall", hookPayloadFor(t, dir, "ls"), "did not decide within")
	if el := time.Since(start); el > 5*time.Second {
		t.Errorf("the deadline did not cut the stall: %s", el)
	}
}

// A registry tq cannot read denies every call: tq cannot tell which
// identity the folder belongs to.
func TestPreToolUse_UnreadableRegistryDenies(t *testing.T) {
	isolateHome(t)
	if err := os.MkdirAll(paths.Home(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config(), []byte("bases: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := testutil.TempDir(t)
	wantGuardErrorDeny(t, "shell", hookPayloadFor(t, dir, "ls"), "cannot read its registry")
	wantGuardErrorDeny(t, "read", toolPayload(t, dir, "Read", `{"file_path":`+jsonPath(t, filepath.Join(dir, "a.txt"))+`}`), "cannot read its registry")
}

// A folder that carries tq's marker but whose protected manifest is gone,
// or does not parse, denies every call there (PRD v2 §6.3, "missing
// manifest"); a folder with no marker under the same base is neutral and
// its calls run.
func TestPreToolUse_MissingOrBrokenManifestDenies(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	file := toolPayload(t, root, "Read", `{"file_path":`+jsonPath(t, filepath.Join(root, "main.go"))+`}`)
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "ls")); code != 0 {
		t.Fatalf("trusted ls: code=%d err=%q", code, errOut)
	}

	mp, _, why := resolve.ManifestFile(root)
	if why != "" {
		t.Fatal(why)
	}
	if err := os.WriteFile(mp, []byte("version: [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantGuardErrorDeny(t, "broken manifest, shell", hookPayloadFor(t, root, "ls"), "manifest of identity acme cannot be read")
	wantGuardErrorDeny(t, "broken manifest, read", file, "manifest of identity acme cannot be read")

	// The protected layout with its manifest gone: a name-only marker at the
	// root and no protected copy.
	if err := os.WriteFile(filepath.Join(root, ".tentaqles.yaml"), []byte("schema: tentaqles-marker-v1\nclient: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.ManifestPath("acme")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	wantGuardErrorDeny(t, "missing manifest, shell", hookPayloadFor(t, root, "ls"), "marker without a protected manifest")
	wantGuardErrorDeny(t, "missing manifest, read", file, "marker without a protected manifest")

	// An unmarked sibling folder under the same base stays neutral.
	sib := filepath.Join(filepath.Dir(root), "plain")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, sib, "ls")); code != 0 {
		t.Fatalf("unmarked folder: code=%d err=%q", code, errOut)
	}
}

// No code path emits an ask: the source of the hook and the rules never
// writes a permissionDecision of "ask" and has no ask verdict (the Phase 2
// gate). The behaviour tests above and the incomplete-scan test cover every
// former ask case: the commit scan, a guard panic, and a Codex call.
func TestNoAskCodePath(t *testing.T) {
	askRe := regexp.MustCompile(`"permissionDecision"\s*:\s*"ask"|"ask"|policy\.Ask\b|\bAsk\s+Action\b|CONFIRM \[`)
	for _, dir := range []string{".", "../policy", "../guard", "../commitscan"} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			n := e.Name()
			if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, n))
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(b), "\n") {
				if askRe.MatchString(line) {
					t.Errorf("%s/%s:%d: %s", dir, n, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
	// Every decision the policy package can return is allow or deny.
	for _, r := range policy.Builtin() {
		if r.Action != policy.Deny {
			t.Errorf("%s: %q", r.ID, r.Action)
		}
	}
}

// Every deny is one line with identity, calling process, harness, tool, the
// redacted command, rule, category and layer (PRD v2 §7.3, ADR-0006).
func TestDenialLogSchema(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "blocked_commands:\n  - npm publish\n")
	tok := "ghp_abcdefghijklmnopqrstuvwxyz0123456789" // tq:allow-secret (fake fixture)

	// An identity deny from the main session, with a token in the line.
	cmd := "npm publish --token=" + tok + ` -m "secret note"`
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, cmd))
	if code != 2 || !strings.Contains(errOut, "BLOCKED [identity/") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	// The deny names the identity's scope (PRD v2 §7.3).
	if !strings.Contains(errOut, "Scope of identity acme:") {
		t.Errorf("identity deny without the scope: %q", errOut)
	}
	// A secrets deny from a subagent.
	sub := `{"tool_name":"Bash","agent_id":"a1","agent_type":"Explore","cwd":` + jsonPath(t, root) + `,"tool_input":{"command":"printenv"}}`
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--agent", "codex"}, sub); code != 2 {
		t.Fatalf("printenv: code=%d", code)
	}
	// A protected-path deny (layer filesystem).
	w := toolPayload(t, root, "Write", `{"file_path":`+jsonPath(t, paths.Config())+`,"content":"x"}`)
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, w); code != 2 {
		t.Fatalf("write config: code=%d", code)
	}

	ds := readDenials(t)
	if len(ds) != 3 {
		t.Fatalf("%d denials: %+v", len(ds), ds)
	}
	id, sec, fs := ds[0], ds[1], ds[2]
	if id.Category != "identity" || id.Layer != audit.LayerHook || id.Caller != audit.CallerHarness || id.Harness != "claude" ||
		id.Tool != "Bash" || id.Identity != "acme" || id.Rule == "" || id.PID == 0 || id.Cwd == "" || id.Time.IsZero() {
		t.Errorf("identity denial: %+v", id)
	}
	if !strings.Contains(id.Command, "npm publish --token=") || !strings.Contains(id.Command, `-m "…"`) {
		t.Errorf("the command lost its shape: %q", id.Command)
	}
	for _, leak := range []string{tok, "secret note"} {
		b, _ := json.Marshal(ds)
		if strings.Contains(string(b), leak) {
			t.Errorf("the log carries %q: %s", leak, b)
		}
	}
	if sec.Category != "secrets" || sec.Rule != "tq/env-dump" || sec.Caller != audit.CallerSubagent || sec.Harness != "codex" || sec.Command != "printenv" {
		t.Errorf("secrets denial: %+v", sec)
	}
	if fs.Category != "self-protection" || fs.Rule != policy.ProtectedPathID || fs.Layer != audit.LayerFilesystem || fs.Tool != "Write" || fs.Subject == "" {
		t.Errorf("protected-path denial: %+v", fs)
	}
}

// A fail-closed deny is logged too, naming what was known.
func TestDenialLogFailClosed(t *testing.T) {
	isolateHome(t)
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, `{"tool_name":`); code != 2 {
		t.Fatalf("code=%d", code)
	}
	ds := readDenials(t)
	if len(ds) != 1 || ds[0].Category != guardErrorID || ds[0].Rule != guardErrorID || ds[0].Layer != audit.LayerHook {
		t.Fatalf("%+v", ds)
	}
}
