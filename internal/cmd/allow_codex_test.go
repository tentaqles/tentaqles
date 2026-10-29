package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// codexTrustWS writes an acme manifest with a codex identity and
// guard.mode: relaxed, trusted without a grant. The mode resolver is
// replaced by one that grants relaxed only with bypass trust (WS1 reads a
// .bypass file as a relaxed grant; the engine is a stub on this branch).
// Returns the manifest path and the codex config.toml path.
func codexTrustWS(t *testing.T) (mp, cfg string) {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "elsewhere"))
	mp = filepath.Join(root, manifest.FileName)
	raw := "schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {}, codex: {} }\nguard:\n  mode: relaxed\n"
	if err := os.WriteFile(mp, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tab, _ := guardcfg.Preset(guardcfg.ModeRelaxed)
	prev := codexModeAt
	codexModeAt = func(ws *resolve.Workspace, cwd string) (string, func(*policy.Rule) policy.Action) {
		if !trust.IsBypassAllowed(ws.Hash) {
			return prev(ws, cwd)
		}
		return guardcfg.ModeRelaxed, func(r *policy.Rule) policy.Action {
			c := guardcfg.CategoryOf(r)
			if c == "" || guardcfg.Locked(c) {
				return r.Action
			}
			return tab.Get(c).Apply(r.Action)
		}
	}
	t.Cleanup(func() { codexModeAt = prev })
	return mp, filepath.Join(paths.IdentityDir("acme", "codex"), "config.toml")
}

func approvalIn(t *testing.T, cfg string) string {
	t.Helper()
	b, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, a := range []string{"never", "on-request"} {
		if strings.Contains(string(b), `approval_policy = "`+a+`"`) {
			return a
		}
	}
	return ""
}

func TestDenyDropsCodexToDefault(t *testing.T) {
	_, cfg := codexTrustWS(t)
	if out, err := runGuardCmd(t, "allow", "--bypass", "acme"); err != nil {
		t.Fatalf("allow --bypass: %v\n%s", err, out)
	}
	if a := approvalIn(t, cfg); a != "never" {
		t.Fatalf("a granted relaxed workspace writes approval never, got %q", a)
	}
	if out, err := runGuardCmd(t, "deny", "acme"); err != nil {
		t.Fatalf("deny: %v\n%s", err, out)
	}
	if a := approvalIn(t, cfg); a != "on-request" {
		t.Fatalf("deny must drop codex to default, got %q", a)
	}
	// The hook stays: an untrusted folder is still guarded (identity guard).
	if _, _, ok := codexcfg.FindHook(filepath.Dir(cfg), "PreToolUse"); !ok {
		t.Fatal("deny keeps tq's hook")
	}
}

func TestPlainAllowAfterEditDropsCodexToDefault(t *testing.T) {
	mp, cfg := codexTrustWS(t)
	if out, err := runGuardCmd(t, "allow", "--bypass", "acme"); err != nil {
		t.Fatalf("allow --bypass: %v\n%s", err, out)
	}
	if a := approvalIn(t, cfg); a != "never" {
		t.Fatalf("allow --bypass: %q", a)
	}
	b, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, append(b, "language: en\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runGuardCmd(t, "allow", "acme"); err != nil {
		t.Fatalf("allow: %v\n%s", err, out)
	}
	if a := approvalIn(t, cfg); a != "on-request" {
		t.Fatalf("plain allow of an edited manifest drops the grant, so codex goes back to default: %q", a)
	}
}
