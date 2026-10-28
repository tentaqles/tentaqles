package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hubsync "github.com/tentaqles/tentaqles/internal/hub/sync"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// servicesWorkspace writes and trusts a workspace with the given extra manifest lines.
func servicesWorkspace(t *testing.T, extra string) (*registry.Config, string) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	body := "schema: tentaqles-client-v2\nclient: acme\ngit: { email: tester@acme.test }\nidentities: { claude: {} }\n" + extra
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := trust.HashFile(mp)
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.IdentityDir("acme", "claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

func writeClaudeJSON(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func finding(fs []Finding, code string) (Finding, bool) {
	for _, f := range fs {
		if f.Code == code {
			return f, true
		}
	}
	return Finding{}, false
}

const trackerBlock = "tracker:\n  provider: jira\n  scope: { org: acme, project: PLAT }\n"

func TestTracker_SkippedWithoutBlockOrChecker(t *testing.T) {
	cfg, root := servicesWorkspace(t, "")
	called := false
	d := deps(nil, root, nil)
	d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) { called = true; return "", nil }
	fs := Run(cfg, d)
	if called || has(fs, "tracker-ok") {
		t.Fatalf("no tracker block: no check (%+v)", fs)
	}
	cfg, root = servicesWorkspace(t, trackerBlock)
	d = deps(nil, root, nil)
	d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) { called = true; return "", nil }
	d.VerifyMode = VerifyOff
	if Run(cfg, d); called {
		t.Fatal("--verify off runs no tracker check")
	}
	fs = Run(cfg, deps(nil, root, nil))
	for _, f := range fs {
		if strings.HasPrefix(f.Code, "tracker-") {
			t.Fatalf("no checker wired (the hook path): no tracker finding, got %+v", f)
		}
	}
}

// Under the default --verify auto the tracker call (network or CLI) is spent only on the
// workspace the user is in; --verify all checks every tracker.
func TestTracker_AutoChecksOnlyTheCwdWorkspace(t *testing.T) {
	cfg, root := servicesWorkspace(t, trackerBlock)
	calls := 0
	check := func(context.Context, *resolve.Workspace, []string) (string, error) { calls++; return "tester", nil }
	outside := testutil.TempDir(t)
	d := deps(nil, outside, nil)
	d.TrackerCheck = check
	if fs := Run(cfg, d); calls != 0 || has(fs, "tracker-ok") {
		t.Fatalf("auto, cwd outside the workspace: no tracker call (calls %d)", calls)
	}
	d.VerifyMode = VerifyAll
	if fs := Run(cfg, d); calls != 1 || !has(fs, "tracker-ok") {
		t.Fatalf("--verify all checks it from anywhere (calls %d)", calls)
	}
	sub := filepath.Join(root, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	d = deps(nil, sub, nil)
	d.TrackerCheck = check
	if fs := Run(cfg, d); calls != 2 || !has(fs, "tracker-ok") {
		t.Fatalf("auto, cwd inside the workspace: checked (calls %d)", calls)
	}
}

func TestTracker_OKNamesAccountAndScope(t *testing.T) {
	cfg, root := servicesWorkspace(t, trackerBlock)
	d := deps(nil, root, nil)
	d.TrackerCheck = func(_ context.Context, w *resolve.Workspace, env []string) (string, error) {
		if w.Name != "acme" || w.Manifest.Tracker.Provider != "jira" {
			t.Errorf("checked %s", w.Name)
		}
		return "tester@acme.test", nil
	}
	fs := Run(cfg, d)
	f, ok := finding(fs, "tracker-ok")
	if !ok || f.Level != "ok" || f.Workspace != "acme" {
		t.Fatalf("%+v", fs)
	}
	for _, want := range []string{"jira", "tester@acme.test", "org=acme;project=PLAT"} {
		if !strings.Contains(f.Msg, want) {
			t.Errorf("msg %q misses %q", f.Msg, want)
		}
	}
	if Exit([]Finding{f}) != 0 {
		t.Fatal("an ok finding never fails doctor")
	}
}

func TestTracker_Failures(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		code  string
		level string
	}{
		{"needs session", &cred.SourceError{Source: cred.SourceMCP, Provider: "jira", Err: cred.ErrNeedsSession}, "tracker-needs-session", "info"},
		{"no adapter", fmt.Errorf("%w for %q", hubsync.ErrNoAdapter, "jira"), "tracker-no-adapter", "info"},
		{"auth failed", errors.New("jira credential from cli: no credential"), "tracker-auth-failed", "warn"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, root := servicesWorkspace(t, trackerBlock)
			d := deps(nil, root, nil)
			d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) { return "", c.err }
			fs := Run(cfg, d)
			f, ok := finding(fs, c.code)
			if !ok || f.Level != c.level || f.Fix == "" {
				t.Fatalf("%+v", fs)
			}
		})
	}
}

// The tracker error comes from other workstreams' cred chain and adapters (CLI output, HTTP
// bodies) and lands in the terminal, --json and the onboard skill's prompt: it is cut to one
// line, bounded and redacted.
func TestTracker_AuthFailedErrorIsRedactedOneLine(t *testing.T) {
	cfg, root := servicesWorkspace(t, trackerBlock)
	tok := "ghp_" + strings.Repeat("A1b2C3d4", 5)
	bearer := "Authorization: Bearer " + strings.Repeat("z9Y8x7W6", 5)
	d := deps(nil, root, nil)
	d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) {
		return "", fmt.Errorf("gh api user: exit 1: token %s rejected; %s\nstderr line two %s\nline three", tok, bearer, tok)
	}
	f, ok := finding(Run(cfg, d), "tracker-auth-failed")
	if !ok {
		t.Fatal("no tracker-auth-failed finding")
	}
	if strings.ContainsAny(f.Msg, "\r\n") {
		t.Fatalf("multi-line message: %q", f.Msg)
	}
	if strings.Contains(f.Msg, tok) || strings.Contains(f.Msg, "A1b2C3d4") || strings.Contains(f.Msg, "z9Y8x7W6") {
		t.Fatalf("token leaked: %q", f.Msg)
	}
	if !strings.Contains(f.Msg, "gh api user") {
		t.Fatalf("the useful part of the error is kept: %q", f.Msg)
	}
	long := strings.Repeat("x", 5000)
	d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) { return "", errors.New(long) }
	f, _ = finding(Run(cfg, d), "tracker-auth-failed")
	if len(f.Msg) > 400 {
		t.Fatalf("unbounded message: %d bytes", len(f.Msg))
	}
}

// Hooks and doctor must never stall: a tracker that hangs is cut off by the budget.
func TestTracker_Bounded(t *testing.T) {
	cfg, root := servicesWorkspace(t, trackerBlock)
	prev := trackerBudget
	trackerBudget = 50 * time.Millisecond
	t.Cleanup(func() { trackerBudget = prev })
	d := deps(nil, root, nil)
	d.TrackerCheck = func(ctx context.Context, _ *resolve.Workspace, _ []string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	fs := Run(cfg, d)
	if time.Since(start) > 5*time.Second {
		t.Fatal("tracker check not bounded")
	}
	if f, ok := finding(fs, "tracker-auth-failed"); !ok || !strings.Contains(f.Msg, "timed out") {
		t.Fatalf("%+v", fs)
	}
}

// A checker that ignores ctx (a keychain prompt, a CLI run without CommandContext) must not stall
// doctor either: the budget is enforced around the call, not only through ctx.
func TestTracker_BoundedWhenCheckIgnoresCtx(t *testing.T) {
	cfg, root := servicesWorkspace(t, trackerBlock)
	prev := trackerBudget
	trackerBudget = 50 * time.Millisecond
	t.Cleanup(func() { trackerBudget = prev })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	d := deps(nil, root, nil)
	d.TrackerCheck = func(context.Context, *resolve.Workspace, []string) (string, error) {
		<-release
		return "late@acme.test", nil
	}
	start := time.Now()
	fs := Run(cfg, d)
	if time.Since(start) > 5*time.Second {
		t.Fatal("tracker check not bounded")
	}
	if f, ok := finding(fs, "tracker-auth-failed"); !ok || !strings.Contains(f.Msg, "timed out") {
		t.Fatalf("%+v", fs)
	}
}

// An azure-devops tracker logs in through whichever az identity the manifest has.
func TestTrackerFix_AzureDevOpsUsesManifestIdentity(t *testing.T) {
	tr := &manifest.Tracker{Provider: "azure-devops", Auth: "cli"}
	only := &manifest.Manifest{Identities: map[string]manifest.Identity{"az": {}}, Tracker: tr}
	if got := trackerFix("acme", only, "s"); got != "tq login acme az" {
		t.Fatalf("az only: %q", got)
	}
	both := &manifest.Manifest{Identities: map[string]manifest.Identity{"az": {}, "azure-devops": {}}, Tracker: tr}
	if got := trackerFix("acme", both, "s"); got != "tq login acme azure-devops" {
		t.Fatalf("both: %q", got)
	}
}

func TestChatEmail_MCPEntryPresent(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: slack }\nemail: { provider: gmail }\n")
	writeClaudeJSON(t, `{"mcpServers": {"slack": {"type": "http", "url": "https://mcp.example.test"}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if f, ok := finding(fs, "chat-ok"); !ok || f.Level != "ok" || !strings.Contains(f.Msg, "slack") {
		t.Fatalf("%+v", fs)
	}
	f, ok := finding(fs, "email-mcp-missing")
	if !ok || f.Level != "warn" || !strings.Contains(f.Msg, "gmail") || !strings.Contains(f.Fix, "claude.bundle.mcp") {
		t.Fatalf("%+v", fs)
	}
}

// Connectors also live in the workspace's project .mcp.json and in .claude.json under
// projects[<folder>].mcpServers; neither is a missing entry. Another folder's project entry is.
func TestChatEmail_ProjectScopedSources(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: slack }\nemail: { provider: gmail }\n")
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers": {"slack": {"command": "x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.ToSlash(filepath.Join(filepath.Dir(root), "elsewhere"))
	writeClaudeJSON(t, `{"projects": {`+fmt.Sprintf("%q", other)+`: {"mcpServers": {"gmail": {"command": "y"}}}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if !has(fs, "chat-ok") {
		t.Fatalf("<root>/.mcp.json counts: %+v", fs)
	}
	if !has(fs, "email-mcp-missing") {
		t.Fatalf("another folder's project entry does not count: %+v", fs)
	}
	writeClaudeJSON(t, `{"projects": {`+fmt.Sprintf("%q", filepath.ToSlash(root))+`: {"mcpServers": {"gmail": {"command": "y"}}}}}`)
	if fs = Run(cfg, deps(nil, root, nil)); !has(fs, "email-ok") {
		t.Fatalf("projects[<root>].mcpServers counts: %+v", fs)
	}
}

func TestChatEmail_NoneAndNoFile(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: none }\nemail: { provider: outlook }\n")
	fs := Run(cfg, deps(nil, root, nil))
	if has(fs, "chat-ok") || has(fs, "chat-mcp-missing") {
		t.Fatalf("chat none is not checked: %+v", fs)
	}
	if !has(fs, "email-mcp-missing") {
		t.Fatalf("no .claude.json means the entry is missing: %+v", fs)
	}
}

// A user may name the server after the connector ("slack-mcp"); the provider id inside the key
// is enough.
func TestChatEmail_LooseServerName(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: teams }\n")
	writeClaudeJSON(t, `{"mcpServers": {"ms-Teams-mcp": {"command": "x"}}}`)
	if fs := Run(cfg, deps(nil, root, nil)); !has(fs, "chat-ok") {
		t.Fatalf("%+v", fs)
	}
}

// The provider id must be a whole token of the key: "steamship" is not a Teams server.
func TestChatEmail_LooseMatchNeedsTokenBoundary(t *testing.T) {
	cfg, root := servicesWorkspace(t, "chat: { provider: teams }\n")
	writeClaudeJSON(t, `{"mcpServers": {"steamship": {"command": "x"}, "teamsy": {"command": "y"}}}`)
	fs := Run(cfg, deps(nil, root, nil))
	if has(fs, "chat-ok") || !has(fs, "chat-mcp-missing") {
		t.Fatalf("%+v", fs)
	}
}

func TestAllChecksPassedWithOnlyOKFindings(t *testing.T) {
	fs := finish([]Finding{{Level: "ok", Code: "chat-ok", Msg: "x"}})
	if !has(fs, "ok") {
		t.Fatalf("%+v", fs)
	}
	fs = finish([]Finding{{Level: "warn", Code: "w", Msg: "x"}})
	if has(fs, "ok") {
		t.Fatalf("%+v", fs)
	}
}
