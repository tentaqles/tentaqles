package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func man(mode string) *manifest.Manifest {
	m := &manifest.Manifest{Client: "acme"}
	m.Claude.PermissionMode = mode
	return m
}

func TestDesiredModes(t *testing.T) {
	cases := []struct {
		mode   string
		bypass bool
		want   string
	}{
		{"", false, "auto"},
		{"auto", false, "auto"},
		{"plan", false, "plan"},
		{"bypass", false, "acceptEdits"},
		{"bypass", true, "bypassPermissions"},
	}
	for _, c := range cases {
		if got := Desired(man(c.mode), c.bypass, "").DefaultMode; got != c.want {
			t.Errorf("mode %q bypass=%v: got %q want %q", c.mode, c.bypass, got, c.want)
		}
	}
}

func TestDesiredManifestAdditionsAndDenyWins(t *testing.T) {
	m := man("")
	m.Claude.Permissions = &manifest.Permissions{Allow: []string{"Bash(make:*)", "Bash(cat:*)"}, Deny: []string{"Bash(cat:*)"}, Ask: []string{"Bash(az webapp deploy:*)"}}
	m.Claude.Env = map[string]string{"FOO": "1"}
	d := Desired(m, false, "C:\\tq\\tq.exe")
	if contains(d.Allow, "Bash(cat:*)") {
		t.Error("deny did not win over allow")
	}
	if !contains(d.Allow, "Bash(make:*)") || !contains(d.Ask, "Bash(az webapp deploy:*)") || d.Env["FOO"] != "1" {
		t.Errorf("manifest additions missing: %+v", d)
	}
	if d.Env["ENABLE_STOP_REVIEW"] != "0" {
		t.Error("baseline env missing")
	}
	if cmd := d.StatusLine["command"].(string); cmd != `"C:/tq/tq.exe" statusline` {
		t.Errorf("status line command = %s", cmd)
	}
}

func TestApplyKeepsUserEntriesAndTakesBackOwned(t *testing.T) {
	current := map[string]any{
		"model": "opus",
		"permissions": map[string]any{
			"allow":       []any{"Bash(my-tool:*)", "Bash(old-tq-entry:*)"},
			"defaultMode": "bypassPermissions",
		},
		"skipDangerousModePermissionPrompt": true,
		"env":                               map[string]any{"USER_VAR": "x", "OLD_TQ": "1"},
		"statusLine":                        map[string]any{"type": "command", "command": "npx -y ccstatusline@latest"},
	}
	prev := Owned{Allow: []string{"Bash(old-tq-entry:*)"}, EnvKeys: []string{"OLD_TQ"}}
	want := Managed{DefaultMode: "auto", Allow: []string{"Bash(ls:*)"}, Deny: []string{"Bash(printenv)"},
		Env: map[string]string{"ENABLE_STOP_REVIEW": "0"}, StatusLine: map[string]any{"type": "command", "command": "tq statusline"}}

	next, owned := Apply(current, want, prev)
	p := next["permissions"].(map[string]any)
	allow := toStrings(p["allow"])
	if !contains(allow, "Bash(my-tool:*)") || !contains(allow, "Bash(ls:*)") || contains(allow, "Bash(old-tq-entry:*)") {
		t.Errorf("allow = %v", allow)
	}
	if p["defaultMode"] != "auto" {
		t.Errorf("defaultMode = %v", p["defaultMode"])
	}
	if _, ok := next["skipDangerousModePermissionPrompt"]; ok {
		t.Error("skipDangerousModePermissionPrompt kept")
	}
	env := next["env"].(map[string]any)
	if env["USER_VAR"] != "x" || env["ENABLE_STOP_REVIEW"] != "0" {
		t.Errorf("env = %v", env)
	}
	if _, ok := env["OLD_TQ"]; ok {
		t.Error("previously owned env key not removed")
	}
	if next["statusLine"].(map[string]any)["command"] != "tq statusline" || !owned.StatusLine {
		t.Error("unpinned ccstatusline not replaced")
	}
	if next["model"] != "opus" {
		t.Error("unrelated key lost")
	}
	if strings.Join(owned.Allow, ",") != "Bash(ls:*)" {
		t.Errorf("owned.Allow = %v", owned.Allow)
	}
	// The input map is not mutated.
	if current["permissions"].(map[string]any)["defaultMode"] != "bypassPermissions" {
		t.Error("input mutated")
	}
}

func TestApplyLeavesCustomStatusLine(t *testing.T) {
	current := map[string]any{"statusLine": map[string]any{"type": "command", "command": "bash ~/.claude/statusline.sh"}}
	next, owned := Apply(current, Managed{StatusLine: map[string]any{"command": "tq statusline"}}, Owned{})
	if next["statusLine"].(map[string]any)["command"] != "bash ~/.claude/statusline.sh" || owned.StatusLine {
		t.Error("custom status line was replaced")
	}
}

func TestApplyUserAlreadyHadEntryIsNotOwned(t *testing.T) {
	current := map[string]any{"permissions": map[string]any{"allow": []any{"Bash(ls:*)"}}}
	_, owned := Apply(current, Managed{Allow: []string{"Bash(ls:*)"}}, Owned{})
	if len(owned.Allow) != 0 {
		t.Errorf("user's own entry became tq-owned: %v", owned.Allow)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"permissions":{"allow":["Bash(mine:*)"]}}`), 0o600)
	want := Desired(man(""), false, "")

	res, err := Render(dir, want, true)
	if err != nil || !res.Changed {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.Contains(string(raw), "defaultMode") {
		t.Fatal("dry run wrote the file")
	}

	if res, err = Render(dir, want, false); err != nil || !res.Changed {
		t.Fatalf("render: %+v %v", res, err)
	}
	if res, err = Render(dir, want, false); err != nil || res.Changed {
		t.Fatalf("second render should be a no-op: %+v %v", res, err)
	}
	var got map[string]any
	raw, _ = os.ReadFile(filepath.Join(dir, "settings.json"))
	json.Unmarshal(raw, &got)
	if !contains(toStrings(got["permissions"].(map[string]any)["allow"]), "Bash(mine:*)") {
		t.Error("user entry lost on render")
	}

	// Dropping a baseline entry from the desired set removes it next time.
	want.Allow = want.Allow[1:]
	Render(dir, want, false)
	raw, _ = os.ReadFile(filepath.Join(dir, "settings.json"))
	json.Unmarshal(raw, &got)
	if contains(toStrings(got["permissions"].(map[string]any)["allow"]), baselineAllow[0]) {
		t.Error("previously owned entry not taken back")
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
