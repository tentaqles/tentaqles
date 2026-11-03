package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// runGuardCmd runs a tq command and returns its error and output (runHook fails
// the test on a command error, which these tests need to see).
func runGuardCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := NewRoot()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func TestGuardOffAllThenOn(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	out, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--all")
	if err != nil || !strings.Contains(out, "tq/cloud-delete: ask -> off") {
		t.Fatalf("off: %v %q", err, out)
	}
	if out, err := runGuardCmd(t, "guard", "on", "tq/cloud-delete", "--all"); err != nil || !strings.Contains(out, "off -> ask") {
		t.Fatalf("on: %v %q", err, out)
	}
}

func TestGuardSetDenyOnWorkspaceRetrustsManifest(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "# keep this comment\nguard:\n  rules:\n    - id: acme/no-prod\n      action: deny\n      command: 'prod-db'\n      reason: no prod\n")
	out, err := runGuardCmd(t, "guard", "set", "tq/cloud-delete", "deny", "--ws", "acme")
	if err != nil || !strings.Contains(out, "tq/cloud-delete: ask -> deny") {
		t.Fatalf("set: %v %q", err, out)
	}
	mp := filepath.Join(ws, manifest.FileName)
	h, _ := trust.HashFile(mp)
	if !trust.IsTrusted(h) {
		t.Fatal("edited manifest was not re-trusted")
	}
	raw, _ := os.ReadFile(mp)
	if !strings.Contains(string(raw), "keep this comment") || !strings.Contains(string(raw), "acme/no-prod") {
		t.Fatalf("manifest lost content:\n%s", raw)
	}
	if _, err := runGuardCmd(t, "guard", "set", "tq/cloud-delete", "default", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
}

func TestGuardWorkspaceEnableBeatsGlobalOff(t *testing.T) {
	setupTrustedWorkspaceWithManifest(t, "acme", "")
	if _, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGuardCmd(t, "guard", "on", "tq/cloud-delete", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
	out, err := runGuardCmd(t, "guard", "list", "--ws", "acme")
	if err != nil || !strings.Contains(out, "tq/cloud-delete") {
		t.Fatalf("list: %v %q", err, out)
	}
}

func TestGuardRefusesUntrustedManifestAndUnknownRule(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if _, err := runGuardCmd(t, "guard", "off", "tq/not-a-rule", "--all"); err == nil || !strings.Contains(err.Error(), "unknown rule") {
		t.Fatalf("unknown rule: %v", err)
	}
	mp := filepath.Join(ws, manifest.FileName)
	f, _ := os.OpenFile(mp, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("\n# edited by someone\n")
	f.Close()
	_, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--ws", "acme")
	if err == nil || !strings.Contains(err.Error(), "tq allow") {
		t.Fatalf("an untrusted manifest must not be edited and re-trusted: %v", err)
	}
	if _, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--ws", "acme", "--all"); err == nil {
		t.Fatal("--ws and --all together must be refused")
	}
}
