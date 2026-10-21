package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

func TestHubSettingsSurviveSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(paths.Home(), 0o700)
	os.WriteFile(paths.Config(), []byte("bases: []\nhub:\n  mode: all\n  port: 7471\n"), 0o600)
	cfg, err := Load()
	if err != nil || cfg.Hub.Mode != "all" || cfg.Hub.Port != 7471 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if _, err := cfg.AddBase(home); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(paths.Config())
	if !strings.Contains(string(raw), "mode: all") || !strings.Contains(string(raw), "port: 7471") {
		t.Fatalf("Save dropped the hub block:\n%s", raw)
	}
	empty := &Config{}
	empty.Save()
	raw, _ = os.ReadFile(paths.Config())
	if strings.Contains(string(raw), "hub:") {
		t.Fatalf("an empty hub block is omitted:\n%s", raw)
	}
}
