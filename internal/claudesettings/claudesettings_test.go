package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func man() *manifest.Manifest {
	return &manifest.Manifest{Client: "acme"}
}

func TestDesiredEnvAndStatusLine(t *testing.T) {
	m := man()
	m.Claude.Env = map[string]string{"FOO": "1"}
	d := Desired(m, filepath.Join("opt", "tq", "tq.exe"))
	if d.Env["FOO"] != "1" || d.Env["ENABLE_STOP_REVIEW"] != "0" {
		t.Errorf("env = %v", d.Env)
	}
	if cmd := d.StatusLine["command"].(string); cmd != `"opt/tq/tq.exe" statusline` {
		t.Errorf("status line command = %s", cmd)
	}
}

func TestApplyKeepsUserEntries(t *testing.T) {
	current := map[string]any{
		"model":      "opus",
		"env":        map[string]any{"USER_VAR": "x", "OLD_TQ": "1"},
		"statusLine": map[string]any{"type": "command", "command": "npx -y ccstatusline@latest"},
	}
	prev := Owned{EnvKeys: []string{"OLD_TQ"}}
	want := Managed{Env: map[string]string{"ENABLE_STOP_REVIEW": "0"}, StatusLine: map[string]any{"type": "command", "command": "tentaqles statusline"}}

	next, owned := Apply(current, want, prev)
	env := next["env"].(map[string]any)
	if env["USER_VAR"] != "x" || env["ENABLE_STOP_REVIEW"] != "0" {
		t.Errorf("env = %v", env)
	}
	if _, ok := env["OLD_TQ"]; ok {
		t.Error("previously owned env key not removed")
	}
	if next["statusLine"].(map[string]any)["command"] != "tentaqles statusline" || !owned.StatusLine {
		t.Error("unpinned ccstatusline not replaced")
	}
	if next["model"] != "opus" {
		t.Error("unrelated key lost")
	}
	if _, ok := next["permissions"]; ok {
		t.Error("a permissions key was written")
	}
	// The input map is not mutated.
	if _, ok := current["env"].(map[string]any)["ENABLE_STOP_REVIEW"]; ok {
		t.Error("input mutated")
	}
}

func TestApplyLeavesCustomStatusLine(t *testing.T) {
	current := map[string]any{"statusLine": map[string]any{"type": "command", "command": "bash ~/.claude/statusline.sh"}}
	next, owned := Apply(current, Managed{StatusLine: map[string]any{"command": "tentaqles statusline"}}, Owned{})
	if next["statusLine"].(map[string]any)["command"] != "bash ~/.claude/statusline.sh" || owned.StatusLine {
		t.Error("custom status line was replaced")
	}
}

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestRenderLeavesPermissionSettings: every permission setting is the user's: a render
// changes env and the status line and nothing else.
func TestRenderLeavesPermissionSettings(t *testing.T) {
	dir := t.TempDir()
	settings := `{"permissions":{"allow":["Bash(my-tool:*)"],"deny":["Read(~/secret)"],"defaultMode":"bypassPermissions"},"skipDangerousModePermissionPrompt":true}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dir, Desired(man(), ""), false); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	p := got["permissions"].(map[string]any)
	if p["defaultMode"] != "bypassPermissions" || len(p["allow"].([]any)) != 1 || len(p["deny"].([]any)) != 1 || got["skipDangerousModePermissionPrompt"] != true {
		t.Errorf("settings = %v", got)
	}
}

// TestRenderFreshWritesNoPermissionKey: a fresh render writes env and the
// status line and no permission key; a second render is a no-op.
func TestRenderFreshWritesNoPermissionKey(t *testing.T) {
	dir := t.TempDir()
	want := Desired(man(), filepath.Join("opt", "tq", "tq.exe"))
	if res, err := Render(dir, want, false); err != nil || !res.Changed {
		t.Fatalf("render: %+v %v", res, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	for _, k := range []string{"permissions", "defaultMode", "skipDangerousModePermissionPrompt"} {
		if strings.Contains(string(raw), k) {
			t.Errorf("fresh render wrote %s: %s", k, raw)
		}
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	if got["env"].(map[string]any)["ENABLE_STOP_REVIEW"] != "0" || got["statusLine"] == nil {
		t.Errorf("settings = %v", got)
	}
	if res, err := Render(dir, want, false); err != nil || res.Changed {
		t.Fatalf("second render should be a no-op: %+v %v", res, err)
	}
}

// TestBaselineEnvKeepsReviewsOffOpus pins the security-guidance knobs: the
// commit/push review reads SG_AGENTIC_MODEL, the other paths
// SECURITY_REVIEW_MODEL, and the per-turn Stop review stays off.
func TestBaselineEnvKeepsReviewsOffOpus(t *testing.T) {
	for k, want := range map[string]string{
		"ENABLE_STOP_REVIEW":    "0",
		"SECURITY_REVIEW_MODEL": "claude-sonnet-5-5",
		"SG_AGENTIC_MODEL":      "claude-sonnet-5-5",
	} {
		if got := baselineEnv[k]; got != want {
			t.Errorf("baselineEnv[%s] = %q, want %q", k, got, want)
		}
	}
}
