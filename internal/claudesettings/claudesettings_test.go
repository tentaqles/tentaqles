package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	prev := Owned{Schema: stateSchema, EnvKeys: []string{"OLD_TQ"}, found: true}
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
	if owned.Schema != stateSchema {
		t.Errorf("owned.Schema = %d", owned.Schema)
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

// oldRender is a settings.json and state file as the render that wrote
// permission settings left them: the allow/deny baseline entries tq added,
// a defaultMode and the skip flag, plus entries the user added by hand.
const oldSettings = `{
  "model": "opus",
  "permissions": {
    "allow": ["Bash(git status:*)", "Bash(ls:*)", "Bash(my-tool:*)"],
    "deny": ["Bash(printenv)", "Read(~/secret-of-mine)"],
    "ask": ["Bash(az webapp deploy:*)"],
    "defaultMode": "bypassPermissions"
  },
  "skipDangerousModePermissionPrompt": true,
  "env": {"ENABLE_STOP_REVIEW": "0", "USER_VAR": "x"}
}`

const oldState = `{
  "allow": ["Bash(git status:*)", "Bash(ls:*)"],
  "deny": ["Bash(printenv)"],
  "ask": ["Bash(az webapp deploy:*)"],
  "env_keys": ["ENABLE_STOP_REVIEW"]
}`

func writeOld(t *testing.T, settings, state string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if state != "" {
		if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(state), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// editedAfter makes settings.json newer than the state file: the user (or
// the harness) changed it after the older render wrote both.
func editedAfter(t *testing.T, dir string) {
	t.Helper()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "settings.json"), later, later); err != nil {
		t.Fatal(err)
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

// TestRenderRemovesAnEmptiedPermissionsObject: with no user entries left the
// permissions object goes.
func TestRenderRemovesAnEmptiedPermissionsObject(t *testing.T) {
	settings := `{"permissions":{"allow":["Bash(ls:*)"],"defaultMode":"auto"},"skipDangerousModePermissionPrompt":true}`
	dir := writeOld(t, settings, `{"allow":["Bash(ls:*)"]}`)
	res, err := Render(dir, Desired(man(), ""), false)
	if err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	if _, ok := got["permissions"]; ok {
		t.Errorf("emptied permissions kept: %v", got)
	}
	if !strings.Contains(strings.Join(res.Changes, "; "), "permissions: removed (empty)") {
		t.Errorf("summary = %v", res.Changes)
	}
}

// TestRenderWithoutStateLeavesPermissionSettings: with no state file there is
// no evidence tq wrote anything, so a defaultMode, the skip flag and every
// permission entry are the user's and stay.
func TestRenderWithoutStateLeavesPermissionSettings(t *testing.T) {
	dir := writeOld(t, oldSettings, "")
	if _, err := Render(dir, Desired(man(), ""), false); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	p := got["permissions"].(map[string]any)
	if p["defaultMode"] != "bypassPermissions" || len(toStrings(p["allow"])) != 3 || len(toStrings(p["deny"])) != 2 || len(toStrings(p["ask"])) != 1 {
		t.Errorf("permissions = %v", p)
	}
	if got["skipDangerousModePermissionPrompt"] != true {
		t.Error("the user's skip flag was removed")
	}
}

// TestRenderLeavesAModeTqNeverWrote: a defaultMode outside what the old
// render produced, or a skip flag set to false, is the user's.
func TestRenderLeavesAModeTqNeverWrote(t *testing.T) {
	dir := writeOld(t, `{"permissions":{"defaultMode":"someFutureMode"},"skipDangerousModePermissionPrompt":false}`, `{}`)
	editedAfter(t, dir)
	if _, err := Render(dir, Desired(man(), ""), false); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	if got["permissions"].(map[string]any)["defaultMode"] != "someFutureMode" || got["skipDangerousModePermissionPrompt"] != false {
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
