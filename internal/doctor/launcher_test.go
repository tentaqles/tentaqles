package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

type launcherFinding struct{ level, code, ws, msg, fix string }

func collectLauncher() (*[]launcherFinding, AddFunc) {
	var fs []launcherFinding
	return &fs, func(level, code, ws, msg, fix string) { fs = append(fs, launcherFinding{level, code, ws, msg, fix}) }
}

func writeLauncherFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func launcherHomeDeps(home string) Deps {
	return Deps{Env: func(k string) (string, bool) {
		if k == "HOME" || k == "USERPROFILE" {
			return home, true
		}
		return "", false
	}}
}

func TestLauncherChecksAreRegistered(t *testing.T) {
	for _, c := range Registered() {
		if c.Name == "launcher" && c.Global != nil && c.Workspace != nil {
			return
		}
	}
	t.Fatal("the launcher check is not registered")
}

func TestClaudeAliasShadow(t *testing.T) {
	cases := []struct {
		file, body string
		want       bool
	}{
		{".bashrc", "alias claude='claude --dangerously-skip-permissions'\neval \"$(tq activate bash)\"\n", true},
		{".zshrc", "  alias codex=\"codex --yolo\"\n", true},
		{".bash_aliases", "alias claude=claude\n", true},
		{filepath.Join(".config", "fish", "config.fish"), "alias claude 'claude --x'\n", true},
		{filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"), "Set-Alias -Name claude -Value C:\\x\\claude.exe\n", true},
		{filepath.Join("Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"), "sal codex C:\\x\\codex.exe\n", true},
		{".bashrc", "# alias claude='old'\nalias cl=claude\nalias claudex=foo\neval \"$(tq activate bash)\"\n", false},
	}
	for _, c := range cases {
		t.Run(c.file+"/"+c.body[:8], func(t *testing.T) {
			home := t.TempDir()
			writeLauncherFile(t, filepath.Join(home, c.file), c.body)
			fs, add := collectLauncher()
			claudeAliasShadow(launcherHomeDeps(home), add)
			got := len(*fs) > 0
			if got != c.want {
				t.Fatalf("finding = %v, want %v (%+v)", got, c.want, *fs)
			}
			if got {
				f := (*fs)[0]
				if f.level != "warn" || f.code != "claude-alias-shadow" || !strings.Contains(f.msg, "bypasses tq's launcher") {
					t.Fatalf("%+v", f)
				}
			}
		})
	}
	// No home, no profiles: nothing, and no panic.
	fs, add := collectLauncher()
	claudeAliasShadow(Deps{Env: func(string) (string, bool) { return "", false }}, add)
	claudeAliasShadow(launcherHomeDeps(t.TempDir()), add)
	if len(*fs) != 0 {
		t.Fatalf("%+v", *fs)
	}
}

func TestPermissionModeOverridden(t *testing.T) {
	for _, c := range []struct {
		mode string
		want bool
	}{{"", false}, {"bypass", false}, {"bypassPermissions", false}, {"default", true}, {"acceptEdits", true}, {"plan", true}, {"auto", true}, {"dontAsk", true}} {
		w := &resolve.Workspace{Name: "acme", Manifest: &manifest.Manifest{Client: "acme"}}
		w.Manifest.Claude.PermissionMode = c.mode
		fs, add := collectLauncher()
		permissionModeOverridden(w, Deps{}, add)
		if got := len(*fs) > 0; got != c.want {
			t.Fatalf("%q: finding = %v, want %v", c.mode, got, c.want)
		}
		if c.want {
			f := (*fs)[0]
			if f.level != "info" || f.code != "permission-mode-overridden" || f.ws != "acme" ||
				!strings.Contains(f.msg, "claude.permission_mode is "+c.mode+"; the claude launcher starts in bypass anyway") {
				t.Fatalf("%+v", f)
			}
		}
	}
}
