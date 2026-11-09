package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// renderNoStatus runs tentaqles settings render with the status line left alone, so the test
// does not touch a settings file outside its own folders.
func renderNoStatus(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runTq(t, append([]string{"settings", "render", "--no-statusline"}, args...)...)
}

func TestSettingsRender_RecordsAClientWithNoRecord(t *testing.T) {
	dir := existingClient(t)
	unbindClient(t, "acme")
	if out, err := renderNoStatus(t, "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want, err := registry.Normalize(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := cfg.Folder("acme")
	if !ok || !registry.SamePath(rec, want) {
		t.Fatalf("acme is recorded as %q (%v), want %s", rec, ok, want)
	}
}

func TestSettingsRender_DryRunRecordsNothing(t *testing.T) {
	existingClient(t)
	unbindClient(t, "acme")
	before := registryBytes(t)
	out, err := renderNoStatus(t, "--dry-run", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "would record") {
		t.Fatalf("a dry run does not say it would record the folder:\n%s", out)
	}
	if !bytes.Equal(before, registryBytes(t)) {
		t.Fatal("a dry run changed the registry")
	}
}

func TestSettingsRender_NeverRecordsAClaimedTwiceName(t *testing.T) {
	dir := existingClient(t)
	unbindClient(t, "acme")
	copiedClient(t, filepath.Dir(dir), "acme")
	// No client resolves here (two folders claim acme), so the command has nothing to render and says so;
	// the warning proves the two claims were seen.
	out, _ := renderNoStatus(t, "--all")
	if !strings.Contains(out, "two folders claim client") {
		t.Fatalf("the two claims were not seen, so the test proves nothing:\n%s", out)
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok := cfg.Folder("acme"); ok {
		t.Fatalf("a name two folders claim was recorded as %q", rec)
	}
}

func TestSettingsRender_RecordsBeforeRendering(t *testing.T) {
	existingClient(t)
	unbindClient(t, "acme")
	// Broken JSON makes claudesettings.Render fail after the folder was recorded: the record must already be saved.
	settings := filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")
	if err := os.WriteFile(settings, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := renderNoStatus(t, "acme"); err == nil {
		t.Fatalf("render of broken settings succeeded, so the failure was not exercised:\n%s", out)
	}
	cfg, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok := cfg.Folder("acme"); !ok {
		t.Fatalf("acme's folder was announced or bound but not saved before its render failed (recorded as %q)", rec)
	}
}
