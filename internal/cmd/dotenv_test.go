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

// workingBash returns a bash that runs `bash -c "exit 0"` successfully, or "" when there is
// none. On Windows the first bash on PATH can be the WSL launcher
// (C:\Windows\System32\bash.exe), which fails when no distribution is installed, so every
// bash.exe on PATH is tried, then the usual Git for Windows install folders.
func workingBash() string {
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("bash"); err == nil && bashRuns(p) {
			return p
		}
		return ""
	}
	var candidates []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir != "" {
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
		if st, err := os.Stat(p); err == nil && !st.IsDir() && bashRuns(p) {
			return p
		}
	}
	return ""
}

func bashRuns(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, "-c", "exit 0").Run() == nil
}

func TestDotenvRunLoadsAndMasks(t *testing.T) {
	isolateHome(t)
	bash := workingBash()
	if bash == "" {
		t.Skip("no working bash found (the WSL launcher on PATH does not count); install Git for Windows to run this test")
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
