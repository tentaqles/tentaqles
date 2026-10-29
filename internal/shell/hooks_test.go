package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

const goldenBin = "/opt/tq/bin/tq"

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", p, err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden mismatch for %s:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestHook_Golden(t *testing.T) {
	for _, sh := range Shells {
		t.Run(sh, func(t *testing.T) {
			got, err := HookFor(sh, goldenBin)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "hook-"+sh+".golden", got)
		})
	}
}

// Every shell with functions gets the spec E §8.2 pieces: the paused hook,
// tq off/on, claude/codex through tq run, and the off launchers.
func TestHook_DefinesLaunchers(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish", "pwsh", "powershell"} {
		h, err := HookFor(sh, goldenBin)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"TQ_ENABLED", "env --off --shell " + sh, "run --off -- claude", "run . -- claude",
			"run --off -- codex", "run . -- codex", "claude-off", "codex-off",
			"tq: off in this window", "tq: on - identity re-applied",
		} {
			if !strings.Contains(h, want) {
				t.Errorf("%s hook: missing %q", sh, want)
			}
		}
	}
	h, _ := HookFor("cmd", goldenBin)
	if !strings.Contains(h, "tq run --off -- claude") || !strings.Contains(h, "tq run . -- claude") {
		t.Errorf("cmd hook must document the launchers: %q", h)
	}
}

// The code of `tq off` per shell, from a fixed workspace state.
func TestEmit_OffGolden(t *testing.T) {
	orig := `C:\orig`
	st := envplan.State{WS: "acme", Prev: map[string]*string{"TQ_WS": nil, "TQ_WS_ROOT": nil, "GH_CONFIG_DIR": nil, "TQ_SOMETHING": &orig}}
	cur := map[string]string{envplan.StateVar: st.Encode(), "TQ_WS": "acme", "TQ_WS_ROOT": "/w/acme",
		"GH_CONFIG_DIR": "/id/gh", "CLAUDE_CONFIG_DIR": "/id/claude", "TQ_SOMETHING": "x"}
	ops := envplan.OffOps(func(k string) (string, bool) { v, ok := cur[k]; return v, ok })
	for _, sh := range Shells {
		t.Run(sh, func(t *testing.T) {
			got, err := Emit(sh, ops)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "off-"+sh+".golden", got)
		})
	}
}

// --- the functions, run in a real shell with a fake tq on PATH -----------

// fakeTq writes a fake tq that logs its argv to log and prints env code for
// `env --shell <sh>` (TQ_WS=acme unless off) and `env --off --shell <sh>`.
func fakeTq(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "tq")
	body := `#!/bin/sh
printf '%s\n' "$*" >> "$TQ_FAKE_LOG"
sh=bash
prev=
for a in "$@"; do [ "$prev" = "--shell" ] && sh=$a; prev=$a; done
case "$1 $2" in
  "env --off")
    case $sh in
      fish) printf 'set -e TQ_WS\nset -gx TQ_MODE '"'"'off'"'"'\n' ;;
      *) printf 'unset TQ_WS\nexport TQ_MODE='"'"'off'"'"'\n' ;;
    esac ;;
  "env --shell")
    if [ "${TQ_MODE:-}" != off ]; then
      case $sh in
        fish) printf 'set -gx TQ_WS '"'"'acme'"'"'\n' ;;
        *) printf 'export TQ_WS='"'"'acme'"'"'\n' ;;
      esac
    fi ;;
esac
exit 0
`
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// wantLog is what the scripts below make the fake tq record.
var wantLog = []string{
	"env --shell SH",
	"env --off --shell SH",
	"run --off -- claude x",
	"env --shell SH",
	"run . -- claude y",
	"run --off -- codex z",
	"version",
}

// findBash returns a bash that can run the test, or "" (WSL's bash.exe
// cannot see Windows paths: skipped, like TestDotenvRunLoadsAndMasks).
func findBash() string {
	if runtime.GOOS == "windows" {
		for _, c := range []string{os.Getenv("BASH"), `C:\Program Files\Git\bin\bash.exe`, `C:\Program Files (x86)\Git\bin\bash.exe`} {
			if c != "" && !strings.Contains(strings.ToLower(c), "system32") {
				if _, err := os.Stat(c); err == nil {
					return c
				}
			}
		}
		return ""
	}
	p, _ := exec.LookPath("bash")
	return p
}

func slash(p string) string { return filepath.ToSlash(p) }

func checkLog(t *testing.T, log, sh string) {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n")), "\n")
	for i := range got {
		got[i] = strings.TrimSpace(got[i])
	}
	want := make([]string, len(wantLog))
	for i, w := range wantLog {
		want[i] = strings.ReplaceAll(w, "SH", sh)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: tq calls\n got  %q\n want %q", sh, got, want)
	}
}

// poisonProfiles points HOME, USERPROFILE, ZDOTDIR, XDG_CONFIG_HOME and
// BASH_ENV of this test process at profile files that print PROFILE-LEAK, so a
// shell test that forgets to isolate them fails (spec E §16: no test touches
// a real shell profile).
func poisonProfiles(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	leak := "echo PROFILE-LEAK\n"
	ps := "Write-Output PROFILE-LEAK\n"
	for rel, body := range map[string]string{
		".zshenv":                  leak,
		".bashrc":                  leak,
		".bash_env":                leak,
		".config/fish/config.fish": leak,
		"Documents/PowerShell/Microsoft.PowerShell_profile.ps1":        ps,
		"Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1": ps,
	} {
		f := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", slash(home))
	t.Setenv("USERPROFILE", home)
	t.Setenv("ZDOTDIR", slash(home))
	t.Setenv("XDG_CONFIG_HOME", slash(filepath.Join(home, ".config")))
	t.Setenv("BASH_ENV", slash(filepath.Join(home, ".bash_env")))
}

// shellEnv is the environment of a live shell test: the test's own, with the
// profile locations moved to an empty folder and BASH_ENV/ENV cleared.
func shellEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	home := t.TempDir()
	env := append(os.Environ(),
		"HOME="+slash(home), "USERPROFILE="+home, "ZDOTDIR="+slash(home),
		"XDG_CONFIG_HOME="+slash(filepath.Join(home, ".config")), "BASH_ENV=", "ENV=",
		"TQ_MODE=", "TQ_ENABLED=", "TQ_WS=")
	return append(env, extra...)
}

func checkNoLeak(t *testing.T, sh, out string) {
	t.Helper()
	if strings.Contains(out, "PROFILE-LEAK") {
		t.Fatalf("%s read a shell profile from the real home: %s", sh, out)
	}
}

func runPosixShell(t *testing.T, shellBin, sh, script string) {
	t.Helper()
	poisonProfiles(t)
	dir := t.TempDir()
	bin := fakeTq(t, dir)
	hook, err := HookFor(sh, slash(bin))
	if err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(dir, "hook."+sh)
	if err := os.WriteFile(hookPath, []byte(hook), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	args := []string{"-c", strings.ReplaceAll(script, "HOOK", slash(hookPath))}
	if sh == "fish" {
		args = append([]string{"--no-config"}, args...)
	}
	cmd := exec.Command(shellBin, args...)
	cmd.Env = shellEnv(t, "TQ_FAKE_LOG="+slash(log))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", sh, err, out)
	}
	s := string(out)
	checkNoLeak(t, sh, s)
	if !strings.Contains(s, "A:off:") || !strings.Contains(s, "B::acme") || !strings.Contains(s, "tq: off in this window") {
		t.Fatalf("%s: env after off/on: %s", sh, s)
	}
	checkLog(t, log, sh)
}

const posixScript = `. 'HOOK'
tq off
echo "A:${TQ_MODE:-}:${TQ_WS:-}"
__tq_hook
claude x
tq on
echo "B:${TQ_MODE:-}:${TQ_WS:-}"
claude y
codex-off z
tq version
`

func TestHook_BashFunctions(t *testing.T) {
	b := findBash()
	if b == "" {
		t.Skip("no usable bash")
	}
	runPosixShell(t, b, "bash", posixScript)
}

func TestHook_ZshFunctions(t *testing.T) {
	z, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	runPosixShell(t, z, "zsh", posixScript)
}

func TestHook_FishFunctions(t *testing.T) {
	f, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("no fish")
	}
	runPosixShell(t, f, "fish", `source 'HOOK'
tq off
echo "A:$TQ_MODE:$TQ_WS"
__tq_hook
claude x
tq on
echo "B:$TQ_MODE:$TQ_WS"
claude y
codex-off z
tq version
`)
}

// PowerShell: pwsh when installed, else Windows PowerShell 5.1, with a fake
// tq.cmd that logs its argv.
func TestHook_PowerShellFunctions(t *testing.T) {
	poisonProfiles(t)
	sh, exe := "pwsh", ""
	if p, err := exec.LookPath("pwsh"); err == nil {
		exe = p
	} else if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell"); err == nil {
			sh, exe = "powershell", p
		}
	}
	if exe == "" || runtime.GOOS != "windows" {
		t.Skip("needs PowerShell on Windows (the fake tq is a .cmd)")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "tq.cmd")
	fake := "@echo off\r\n" +
		">>\"%TQ_FAKE_LOG%\" echo %*\r\n" +
		"if \"%1 %2\"==\"env --off\" goto off\r\n" +
		"if \"%1\"==\"env\" if not \"%TQ_MODE%\"==\"off\" echo $env:TQ_WS = 'acme'\r\n" +
		"exit /b 0\r\n" +
		":off\r\n" +
		"echo Remove-Item -ErrorAction SilentlyContinue Env:TQ_WS\r\n" +
		"echo $env:TQ_MODE = 'off'\r\n" +
		"exit /b 0\r\n"
	if err := os.WriteFile(bin, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	hook, err := HookFor(sh, bin)
	if err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(dir, "hook.ps1")
	if err := os.WriteFile(hookPath, []byte(hook), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	script := "Invoke-Expression ([IO.File]::ReadAllText('" + hookPath + "'))\n" +
		"tq off\n" +
		"Write-Output \"A:$($env:TQ_MODE):$($env:TQ_WS)\"\n" +
		"__tq_hook\n" +
		"claude x\n" +
		"tq on\n" +
		"Write-Output \"B:$($env:TQ_MODE):$($env:TQ_WS)\"\n" +
		"claude y\n" +
		"codex-off z\n" +
		"tq version\n"
	cmd := exec.Command(exe, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = shellEnv(t, "TQ_FAKE_LOG="+log)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	checkNoLeak(t, sh, string(out))
	if s := string(out); !strings.Contains(s, "A:off:") || !strings.Contains(s, "B::acme") {
		t.Fatalf("env after off/on: %s", s)
	}
	checkLog(t, log, sh)
}
