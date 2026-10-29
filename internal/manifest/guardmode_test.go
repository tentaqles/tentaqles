package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardModeKeysDecode(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	body := "schema: tentaqles-client-v2\nclient: acme\nguard:\n  mode: custom\n  base: relaxed\n  categories:\n    db-destructive: ask\n  rules_override:\n    tq/iac-apply: ask\n  projects:\n    demo-todo: { mode: strict }\n  headless_mode: strict\n  codex: { sandbox: read-only }\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	g := m.Guard
	if g.Mode != "custom" || g.Base != "relaxed" || g.Categories["db-destructive"] != "ask" || g.RulesOverride["tq/iac-apply"] != "ask" ||
		g.Projects["demo-todo"].Mode != "strict" || g.HeadlessMode != "strict" || g.CodexSandbox() != "read-only" || !g.HasMode() {
		t.Fatalf("decoded: %+v", g)
	}
	if err := g.ValidateMode(); err != nil {
		t.Fatalf("stub accepts everything: %v", err)
	}
}

func TestGuardCodexSandboxFallback(t *testing.T) {
	var g Guard
	if g.HasMode() || g.CodexSandbox() != "workspace-write" {
		t.Fatal("empty guard")
	}
	g.Unattended = &Unattended{CodexSandbox: "danger-full-access"}
	if g.CodexSandbox() != "danger-full-access" {
		t.Fatal("legacy codex_sandbox")
	}
	g.Codex = &GuardCodex{Sandbox: "read-only"}
	if g.CodexSandbox() != "read-only" {
		t.Fatal("guard.codex wins")
	}
}
