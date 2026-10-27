package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// codexTrustWS writes an acme manifest with a codex identity and
// guard.unattended.workspace:true, trusted but without bypass. Returns the
// manifest path and the codex config.toml path.
func codexTrustWS(t *testing.T) (mp, cfg string) {
	t.Helper()
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "elsewhere"))
	mp = filepath.Join(root, manifest.FileName)
	raw := "schema: tentaqles-client-v2\nclient: acme\nidentities: { gh: {}, codex: {} }\nguard:\n  unattended:\n    workspace: true\n"
	if err := os.WriteFile(mp, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return mp, filepath.Join(paths.IdentityDir("acme", "codex"), "config.toml")
}

func hasCodexBlock(t *testing.T, cfg string) bool {
	t.Helper()
	b, err := os.ReadFile(cfg)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Contains(string(b), `approval_policy = "never"`)
}

func TestDenyRemovesCodexBlock(t *testing.T) {
	_, cfg := codexTrustWS(t)
	if out, err := runGuardCmd(t, "allow", "--bypass", "acme"); err != nil {
		t.Fatalf("allow --bypass: %v\n%s", err, out)
	}
	if !hasCodexBlock(t, cfg) {
		t.Fatal("allow --bypass must write the codex block")
	}
	if out, err := runGuardCmd(t, "deny", "acme"); err != nil {
		t.Fatalf("deny: %v\n%s", err, out)
	}
	if hasCodexBlock(t, cfg) {
		t.Fatal("deny must remove the codex block")
	}
}

func TestPlainAllowAfterEditRemovesCodexBlock(t *testing.T) {
	mp, cfg := codexTrustWS(t)
	if out, err := runGuardCmd(t, "allow", "--bypass", "acme"); err != nil {
		t.Fatalf("allow --bypass: %v\n%s", err, out)
	}
	if !hasCodexBlock(t, cfg) {
		t.Fatal("allow --bypass must write the codex block")
	}
	b, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, append(b, "language: en\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runGuardCmd(t, "allow", "acme"); err != nil {
		t.Fatalf("allow: %v\n%s", err, out)
	}
	if hasCodexBlock(t, cfg) {
		t.Fatal("plain allow of an edited manifest drops bypass, so the codex block must go")
	}
}
