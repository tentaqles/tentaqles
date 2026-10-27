package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardlog"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func runUnattendedCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runGuardCmd(t, append([]string{"unattended"}, args...)...)
}

func TestUnattendedOnProjectThenOff(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if err := os.MkdirAll(filepath.Join(root, "demo-todo"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	out, err := runUnattendedCmd(t, "on", "--project", "demo-todo")
	if err != nil {
		t.Fatalf("on: %v\n%s", err, out)
	}
	if !strings.Contains(out, "tq allow --bypass acme") {
		t.Fatalf("must say bypass trust is missing: %q", out)
	}
	m, err := manifest.Load(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if u := m.Guard.Unattended; u == nil || u.Workspace || len(u.Projects) != 1 || u.Projects[0] != "demo-todo" {
		t.Fatalf("block = %+v", m.Guard.Unattended)
	}
	h, _ := trust.HashFile(filepath.Join(root, manifest.FileName))
	if !trust.IsTrusted(h) {
		t.Fatal("edited manifest must be re-trusted")
	}
	if _, err := runUnattendedCmd(t, "off", "--project", "demo-todo"); err != nil {
		t.Fatal(err)
	}
	m, _ = manifest.Load(filepath.Join(root, manifest.FileName))
	if m.Guard.Unattended.Configured() {
		t.Fatalf("off must clear it: %+v", m.Guard.Unattended)
	}
}

func TestUnattendedOnRejectsBadProject(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Chdir(root)
	outside := t.TempDir()
	for _, p := range []string{"missing", "..", root, outside} {
		if _, err := runUnattendedCmd(t, "on", "--project", p); err == nil {
			t.Errorf("project %q must be refused", p)
		}
	}
}

func TestUnattendedKeepsBypassTrustAndStatus(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	h, _ := trust.HashFile(filepath.Join(root, manifest.FileName))
	_ = trust.AllowBypass(h)
	t.Chdir(root)
	out, err := runUnattendedCmd(t, "on", "--codex-sandbox", "read-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "tq allow --bypass") {
		t.Fatalf("bypass trust was carried, no hint expected: %q", out)
	}
	out, err = runUnattendedCmd(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workspace", "effective", "tq/guard-change", "tq/iac-apply", "read-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
	if _, err := runUnattendedCmd(t, "on", "--codex-sandbox", "nope"); err == nil {
		t.Error("bad sandbox must be refused")
	}
}

func TestUnattendedLog(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Chdir(root)
	now := time.Now().UTC()
	guardlog.Append(guardlog.Record{Time: now.Add(-2 * time.Minute), WS: "acme", Scope: "workspace", Mode: "auto", Rules: []string{"tq/force-push"}, Tool: "Bash", Command: "older"}, nil)
	guardlog.Append(guardlog.Record{Time: now.Add(-1 * time.Minute), WS: "other", Scope: "workspace", Mode: "auto", Rules: []string{"tq/force-push"}, Tool: "Bash", Command: "elsewhere"}, nil)
	guardlog.Append(guardlog.Record{Time: now, WS: "acme", Scope: "demo-todo", Mode: "bypassPermissions", Rules: []string{"tq/a", "tq/b"}, Tool: "Bash", Command: "newer"}, nil)
	out, err := runUnattendedCmd(t, "log", "--ws", "acme")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "newer") || !strings.Contains(lines[0], "tq/a,tq/b") || !strings.Contains(lines[1], "older") {
		t.Fatalf("log = %q", out)
	}
}
