package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
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

const cloudDelete = `{"command":"az group delete --name rg-demo --yes"}`

func TestGuardOffAllThenOn(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if _, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete)); !strings.Contains(askReason(t, out), "tq/cloud-delete") {
		t.Fatalf("baseline should ask: %q", out)
	}
	out, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--all")
	if err != nil || !strings.Contains(out, "tq/cloud-delete: ask -> off") {
		t.Fatalf("off: %v %q", err, out)
	}
	if code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete)); code != 0 || out != "" {
		t.Fatalf("rule still fired after off --all: code=%d out=%q", code, out)
	}
	if out, err := runGuardCmd(t, "guard", "on", "tq/cloud-delete", "--all"); err != nil || !strings.Contains(out, "off -> ask") {
		t.Fatalf("on: %v %q", err, out)
	}
	if _, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete)); !strings.Contains(askReason(t, out), "tq/cloud-delete") {
		t.Fatalf("rule not back on: %q", out)
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
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete))
	if code != 2 || !strings.Contains(errOut, "tq/cloud-delete") {
		t.Fatalf("set deny not applied: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":"psql -h prod-db"}`)); code != 2 || !strings.Contains(errOut, "acme/no-prod") {
		t.Fatalf("existing manifest rule lost: code=%d err=%q", code, errOut)
	}
	if _, err := runGuardCmd(t, "guard", "set", "tq/cloud-delete", "default", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete)); !strings.Contains(askReason(t, out), "tq/cloud-delete") {
		t.Fatalf("default did not restore ask: %q", out)
	}
}

func TestGuardWorkspaceEnableBeatsGlobalOff(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	if _, err := runGuardCmd(t, "guard", "off", "tq/cloud-delete", "--all"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGuardCmd(t, "guard", "on", "tq/cloud-delete", "--ws", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", cloudDelete)); !strings.Contains(askReason(t, out), "tq/cloud-delete") {
		t.Fatalf("workspace enable must win over global off: %q", out)
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

func TestGuardProtectsItsOwnSettings(t *testing.T) {
	ws := setupTrustedWorkspaceWithManifest(t, "acme", "")
	for _, cmd := range []string{"tq guard off tq/env-dump --all", "tq.exe guard set tq/cloud-delete ask", "tq allow acme", "cd x && tq deny acme"} {
		_, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":`+jsonPath(t, cmd)+`}`))
		if !strings.Contains(askReason(t, out), "tq/guard-change") {
			t.Errorf("%s should ask: %q", cmd, out)
		}
	}
	if code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":"tq guard list"}`)); code != 0 || out != "" {
		t.Errorf("tq guard list must pass: code=%d out=%q", code, out)
	}
	gp := guardcfg.Path()
	code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Write", `{"file_path":`+jsonPath(t, gp)+`,"content":"disable: [tq/env-dump]"}`))
	if code != 2 || !strings.Contains(errOut, "tq/guard-file-write") {
		t.Errorf("Write to guard.yaml: code=%d err=%q", code, errOut)
	}
	for _, cmd := range []string{"echo 'disable: [tq/env-dump]' > ~/.tentaqles/guard.yaml", `Set-Content $HOME\.tentaqles\guard.yaml 'x'`, "rm ~/.tentaqles/guard.yaml"} {
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, ws, "Bash", `{"command":`+jsonPath(t, cmd)+`}`))
		if code != 2 || !strings.Contains(errOut, "tq/guard-file-write") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
}
