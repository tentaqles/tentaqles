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
	ps := filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	ps5 := filepath.Join("Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1")
	fish := filepath.Join(".config", "fish", "config.fish")
	block := "# >>> tq >>>\nif ($env:TQ_ENABLED -ne '0') { tq activate pwsh | Out-String | Invoke-Expression }\n# <<< tq <<<\n"
	cases := []struct {
		file, body string
		want       bool
		level, msg string
	}{
		// an alias that calls the same name only adds flags to the launcher
		{".bashrc", "alias claude='claude --dangerously-skip-permissions'\neval \"$(tq activate bash)\"\n", true, "info", "adds flags to tq's claude launcher"},
		{".zshrc", "  alias codex=\"codex --yolo\"\n", true, "info", "adds flags"},
		{".bash_aliases", "alias claude=claude\n", true, "info", "adds flags"},
		{fish, "alias claude 'claude --x'\n", true, "info", "adds flags"},
		// one that runs something else replaces it
		{".bashrc", "alias claude='/opt/claude/bin/claude --x'\n", true, "warn", "instead of tq's claude launcher"},
		{ps, "Set-Alias -Name claude -Value C:\\x\\claude.exe\n", true, "warn", "PowerShell resolves aliases before functions"},
		{ps5, "sal codex C:\\x\\codex.exe\n", true, "warn", "PowerShell resolves aliases before functions"},
		{".bashrc", "# alias claude='old'\nalias cl=claude\nalias claudex=foo\neval \"$(tq activate bash)\"\n", false, "", ""},
		// functions after tq's activation replace the launcher (final review M2)
		{ps, block + "\n# carried over by tq migrate; permission_mode will move into the manifest\nfunction claude { & \"$env:USERPROFILE\\.local\\bin\\claude.exe\" --permission-mode auto @args }\n", true, "warn", "function claude, defined after tq's activation"},
		{ps5, block + "function global:codex { codex.exe @args }\n", true, "warn", "function codex"},
		{".bashrc", "eval \"$(tq activate bash)\"\nclaude() { command claude --x \"$@\"; }\n", true, "warn", "function claude"},
		{".zshrc", "eval \"$(tq activate zsh)\"\nfunction codex { command codex \"$@\"; }\n", true, "warn", "function codex"},
		{fish, "tq activate fish | source\nfunction claude\n  command claude $argv\nend\n", true, "warn", "function claude"},
		// before the activation tq's own definition wins; claude-off is not claude;
		// the preserved tq-legacy block runs only under TQ_ENABLED=0
		{".bashrc", "claude() { :; }\neval \"$(tq activate bash)\"\nfunction claude-off { :; }\n", false, "", ""},
		{ps, "# >>> tq-legacy >>>\nif ($env:TQ_ENABLED -eq '0') {\nfunction claude { x }\n}\n# <<< tq-legacy <<<\n" + block + "function claude-personal { x }\n", false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.file+"/"+c.msg, func(t *testing.T) {
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
				if f.level != c.level || f.code != "claude-alias-shadow" || !strings.Contains(f.msg, c.msg) {
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

// The fix line is accurate (correctness re-review r2-1): a wrapper that tq
// migrate removes (the exact text it carried, with
// --dangerously-skip-permissions) says so; any other function names the
// profile and the lines to delete by hand. The profiles are fixtures under a
// temp home, never real ones.
func TestClaudeAliasShadowFixLine(t *testing.T) {
	ps := filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	block := "# >>> tq >>>\r\nif ($env:TQ_ENABLED -ne '0') { tq activate pwsh | Out-String | Invoke-Expression }\r\n# <<< tq <<<\r\n"
	carried := "\r\n# carried over by tq migrate; permission_mode will move into the manifest\r\nif ($env:TQ_ENABLED -ne '0') {\r\nfunction claude { & \"$env:USERPROFILE\\.local\\bin\\claude.exe\" --dangerously-skip-permissions @args }\r\n}\r\n"
	cases := []struct{ name, body, want string }{
		{"removable carry", block + carried, "tq migrate removes it"},
		{"carry with another flag", block + strings.Replace(carried, "--dangerously-skip-permissions", "--permission-mode auto", 1), "delete lines 7-7 of "},
		{"hand-written, three lines", block + "\r\nfunction claude {\r\n  & claude.exe @args\r\n}\r\n", "delete lines 5-7 of "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			writeLauncherFile(t, filepath.Join(home, ps), c.body)
			fs, add := collectLauncher()
			claudeAliasShadow(launcherHomeDeps(home), add)
			if len(*fs) != 1 {
				t.Fatalf("findings = %+v", *fs)
			}
			if f := (*fs)[0]; !strings.Contains(f.fix, c.want) || (c.want != "tq migrate removes it" && strings.Contains(f.fix, "tq migrate")) {
				t.Fatalf("fix = %q, want %q", f.fix, c.want)
			}
		})
	}
}
