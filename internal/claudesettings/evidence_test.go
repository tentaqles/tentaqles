package claudesettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// The round-2 critic's case (zz_critic_test.go), as a regression test: an
// older render wrote defaultMode bypassPermissions for permission_mode:
// bypass; the user then removed permission_mode as tq doctor advises. The
// next render still takes the value back: settings.json is unchanged since
// that render wrote it and its state file, so the value is tq's. The skip
// flag and the recorded allow entry go too; the user's nothing else.
func TestRenderRemovesTheModeAfterPermissionModeIsDropped(t *testing.T) {
	for _, pm := range []string{"bypass", ""} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"permissions":{"defaultMode":"bypassPermissions","allow":["Bash(ls:*)"]},"skipDangerousModePermissionPrompt":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"allow":["Bash(ls:*)"]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		m := &manifest.Manifest{OldPermissionMode: pm}
		if _, err := Render(dir, Desired(m, ""), false); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
		for _, k := range []string{"defaultMode", "skipDangerousModePermissionPrompt", "Bash(ls:*)", "permissions"} {
			if strings.Contains(string(b), k) {
				t.Errorf("permission_mode=%q: %s left behind: %s", pm, k, b)
			}
		}
	}
}

// The evidence stays in the state file while settings.json still holds the
// value tq wrote, and only then: a later render (with no old
// permission_mode and a settings.json the user edited since) still takes
// it back, and once it is gone the state no longer claims it.
func TestRenderCarriesTheEvidenceUntilTheValueIsGone(t *testing.T) {
	dir := t.TempDir()
	st := Owned{Schema: stateSchema, LegacyDefaultMode: "bypassPermissions", found: true}
	if err := saveOwned(dir, st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"permissions":{"defaultMode":"bypassPermissions"},"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	editedAfter(t, dir)
	if _, err := Render(dir, Desired(man(), ""), false); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, filepath.Join(dir, "settings.json"))
	if _, ok := got["permissions"]; ok {
		t.Errorf("the value tq wrote is still there: %v", got)
	}
	if o := loadOwned(dir); o.LegacyDefaultMode != "" {
		t.Errorf("the state still claims %q after it was removed", o.LegacyDefaultMode)
	}
	// The user sets it again by hand: it is theirs now.
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"permissions":{"defaultMode":"bypassPermissions"},"model":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dir, Desired(man(), ""), false); err != nil {
		t.Fatal(err)
	}
	if p, _ := readJSON(t, filepath.Join(dir, "settings.json"))["permissions"].(map[string]any); p == nil || p["defaultMode"] != "bypassPermissions" {
		t.Error("a defaultMode the user set after tq removed its own was removed")
	}
}
