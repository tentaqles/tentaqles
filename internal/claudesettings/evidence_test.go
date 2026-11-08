package claudesettings

import (
	"os"
	"path/filepath"
	"testing"
)

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
