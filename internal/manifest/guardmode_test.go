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
		t.Fatalf("a valid block: %v", err)
	}
}

func TestGuardModeValidation(t *testing.T) {
	bad := map[string]string{
		"off is never stored":   "guard:\n  mode: off\n",
		"unknown mode":          "guard:\n  mode: loose\n",
		"base without custom":   "guard:\n  mode: relaxed\n  base: strict\n",
		"bad base":              "guard:\n  mode: custom\n  base: custom\n",
		"categories no custom":  "guard:\n  mode: relaxed\n  categories: {system: skip}\n",
		"locked category":       "guard:\n  mode: custom\n  categories: {privacy: skip}\n",
		"locked identity":       "guard:\n  mode: custom\n  categories: {identity: rule}\n",
		"unknown category":      "guard:\n  mode: custom\n  categories: {nope: skip}\n",
		"bad category value":    "guard:\n  mode: custom\n  categories: {system: allow}\n",
		"bad override value":    "guard:\n  rules_override: {tq/iac-apply: allow}\n",
		"override bad id":       "guard:\n  rules_override: {'a b': ask}\n",
		"project abs":           "guard:\n  projects:\n    /abs: {mode: relaxed}\n",
		"project dotdot":        "guard:\n  projects:\n    ../x: {mode: relaxed}\n",
		"project root":          "guard:\n  projects:\n    .: {mode: relaxed}\n",
		"project off":           "guard:\n  projects:\n    app: {mode: off}\n",
		"project locked":        "guard:\n  projects:\n    app: {mode: custom, categories: {secrets: skip}}\n",
		"headless off":          "guard:\n  headless_mode: off\n",
		"codex sandbox":         "guard:\n  codex: {sandbox: yolo}\n",
		"mixed with unattended": "guard:\n  mode: relaxed\n  unattended:\n    workspace: true\n",
		"mixed projects":        "guard:\n  projects:\n    app: {mode: relaxed}\n  unattended:\n    projects: [app]\n",
		"rule bad category":     "guard:\n  rules:\n    - {id: acme/x, action: deny, command: x, reason: r, category: nope}\n",
	}
	for name, body := range bad {
		if _, err := loadBody(t, body); err == nil {
			t.Errorf("%s: want a load error", name)
		}
	}
	good := []string{
		"guard:\n  mode: strict\n",
		"guard:\n  mode: custom\n  base: relaxed\n  categories: {db-destructive: ask, system: skip}\n",
		"guard:\n  rules_override: {tq/iac-apply: skip, acme/x: deny}\n",
		"guard:\n  projects:\n    app/web: {mode: custom, categories: {hygiene: skip}}\n  headless_mode: relaxed\n",
		"guard:\n  rules:\n    - {id: acme/x, action: deny, command: x, reason: r, category: secrets}\n",
		"guard:\n  unattended:\n    workspace: true\n    keep_asking: [tq/commit-pii]\n  headless_mode: default\n",
	}
	for _, body := range good {
		if _, err := loadBody(t, body); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}

func TestGuardModeViewReadsLegacyUnattended(t *testing.T) {
	m, err := loadBody(t, "guard:\n  unattended:\n    workspace: true\n    projects: [demo-todo/]\n    codex_sandbox: read-only\n")
	if err != nil {
		t.Fatal(err)
	}
	v := m.Guard.ModeView()
	if v.Unattended != nil || v.Mode != "relaxed" || v.Projects["demo-todo"].Mode != "relaxed" || v.CodexSandbox() != "read-only" {
		t.Fatalf("view: %+v", v)
	}
	if v.RulesOverride["tq/iac-apply"] != "rule" || v.RulesOverride["tq/remote-migration"] != "rule" || len(v.RulesOverride) != 2 {
		t.Fatalf("absent keep_asking keeps the old defaults asking: %v", v.RulesOverride)
	}
	if m.Guard.Unattended == nil || m.Guard.Mode != "" {
		t.Fatal("the view is in memory only")
	}
	m, _ = loadBody(t, "guard:\n  unattended:\n    projects: [app]\n    keep_asking: []\n")
	if v := m.Guard.ModeView(); v.Mode != "" || v.Projects["app"].Mode != "relaxed" || len(v.RulesOverride) != 0 {
		t.Fatalf("projects only, nothing kept: %+v", v)
	}
	m, _ = loadBody(t, "guard:\n  unattended:\n    workspace: false\n  disable: [tq/force-push]\n")
	if v := m.Guard.ModeView(); v.Mode != "" || len(v.Projects) != 0 || v.Unattended == nil {
		t.Fatalf("a block that turns nothing on adds nothing: %+v", v)
	}
	old := LockedRuleID
	LockedRuleID = func(id string) bool { return id == "tq/commit-pii" }
	defer func() { LockedRuleID = old }()
	m, err = loadBody(t, "guard:\n  unattended:\n    workspace: true\n    keep_asking: [tq/commit-pii, tq/iac-destroy]\n")
	if err != nil {
		t.Fatalf("a locked keep_asking entry still loads: %v", err)
	}
	if v := m.Guard.ModeView(); len(v.RulesOverride) != 1 || v.RulesOverride["tq/iac-destroy"] != "rule" {
		t.Fatalf("locked keep_asking entries are dropped: %v", v.RulesOverride)
	}
	if _, err := loadBody(t, "guard:\n  rules_override: {tq/commit-pii: skip}\n"); err == nil {
		t.Fatal("a locked id in rules_override is an error")
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
