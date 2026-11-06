package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func dotenvSecret() string { return "pw" + strings.Repeat("Z3k", 7) }

// windowsBash returns a bash that can run the masking test, or "" when this box has none. The
// first bash on PATH can be the WSL launcher (C:\Windows\System32\bash.exe, or an alias under
// WindowsApps), which fails without a distribution and doesn't pass the environment through
// with one, so those never count. A bash that exists but doesn't answer fails the test: a skip
// there would silently stop proving masking.
func windowsBash(t *testing.T) string {
	t.Helper()
	var candidates []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		lower := strings.ToLower(dir)
		if dir != "" && !strings.Contains(lower, `\system32`) && !strings.Contains(lower, `\windowsapps`) {
			candidates = append(candidates, filepath.Join(dir, "bash.exe"))
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)", "LocalAppData"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		if env == "LocalAppData" {
			base = filepath.Join(base, "Programs")
		}
		candidates = append(candidates, filepath.Join(base, "Git", "bin", "bash.exe"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := exec.CommandContext(ctx, p, "-c", "exit 0").Run()
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut {
			t.Fatalf("%s didn't answer within 30s, so masking can't be proved here", p)
		}
		if err == nil {
			return p
		}
	}
	return ""
}

func TestDotenvRunLoadsAndMasks(t *testing.T) {
	isolateHome(t)
	bash := "bash" // Elsewhere a missing bash fails the test, as it always has.
	if runtime.GOOS == "windows" {
		if bash = windowsBash(t); bash == "" {
			t.Skip("no working bash found (the WSL launcher doesn't count); install Git for Windows to run this test")
		}
	}
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	os.WriteFile(env, []byte("DB_PASSWORD="+dotenvSecret()+"\nREGION=us\n"), 0o600) // tq:allow-secret: fake fixture built at runtime
	os.Unsetenv("DB_PASSWORD")
	code, out, errOut := runHook(t, []string{"dotenv", "run", "--file", env, "--", bash, "-c", `echo "pw=$DB_PASSWORD region=$REGION"; echo "err=$DB_PASSWORD" >&2; exit 7`}, "")
	if code != 7 {
		t.Fatalf("exit code not propagated: %d (out=%q err=%q)", code, out, errOut)
	}
	if strings.Contains(out+errOut, dotenvSecret()) {
		t.Fatalf("secret leaked: out=%q err=%q", out, errOut)
	}
	if !strings.Contains(out, "pw=[REDACTED:DB_PASSWORD] region=us") || !strings.Contains(errOut, "err=[REDACTED:DB_PASSWORD]") {
		t.Fatalf("out=%q err=%q", out, errOut)
	}
}

func TestDotenvRunRefusesNonEnvFiles(t *testing.T) {
	isolateHome(t)
	key := filepath.Join(t.TempDir(), "id_rsa")
	os.WriteFile(key, []byte("X=1\n"), 0o600)
	root := NewRoot()
	root.SetArgs([]string{"dotenv", "run", "--file", key, "--", "true"})
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "not a .env file") {
		t.Fatalf("err=%v out=%q", err, out.String())
	}
}

func TestPolicy_EnvReadDeniedWithInstruction(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	for _, cmd := range []string{"cat .env", "grep DATABASE_URL .env", `Get-Content .\.env`} {
		tool := "Bash"
		if strings.HasPrefix(cmd, "Get-") {
			tool = "PowerShell"
		}
		code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, tool, `{"command":`+jsonPath(t, cmd)+`}`))
		if code != 2 || !strings.Contains(errOut, "tq dotenv run") {
			t.Errorf("%s: code=%d err=%q", cmd, code, errOut)
		}
	}
	for _, cmd := range []string{"tq dotenv run -- npm run dev", "tq dotenv run --file .env.local -- npm test", "cat .env.example"} {
		code, out, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Bash", `{"command":`+jsonPath(t, cmd)+`}`))
		if code != 0 || out != "" {
			t.Errorf("%s must pass: code=%d out=%q err=%q", cmd, code, out, errOut)
		}
	}
}
