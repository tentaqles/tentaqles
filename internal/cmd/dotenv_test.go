package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func dotenvSecret() string { return "pw" + strings.Repeat("Z3k", 7) }

func TestDotenvKeysNeverPrintsValues(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	os.WriteFile(env, []byte("DB_PASSWORD="+dotenvSecret()+"\nEMPTY=\nDB_PASSWORD=again"+dotenvSecret()+"\n"), 0o600) // tq:allow-secret: fake fixture built at runtime
	code, out, _ := runHook(t, []string{"dotenv", "keys", env}, "")
	if code != 0 || strings.Contains(out, dotenvSecret()) || !strings.Contains(out, "DB_PASSWORD\tset") || !strings.Contains(out, "EMPTY\tempty") || !strings.Contains(out, "duplicate") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, _, errOut := runHook(t, []string{"dotenv", "keys", env, "--require", "EMPTY", "--require", "DB_PASSWORD"}, "")
	if code != 1 || !strings.Contains(errOut, "EMPTY") || strings.Contains(errOut, "DB_PASSWORD") {
		t.Fatalf("require: code=%d err=%q", code, errOut)
	}
}

func TestDotenvRunLoadsAndMasks(t *testing.T) {
	isolateHome(t)
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("needs bash")
		}
	}
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	os.WriteFile(env, []byte("DB_PASSWORD="+dotenvSecret()+"\nREGION=us\n"), 0o600) // tq:allow-secret: fake fixture built at runtime
	t.Setenv("DB_PASSWORD", "")
	os.Unsetenv("DB_PASSWORD")
	code, out, errOut := runHook(t, []string{"dotenv", "run", "--file", env, "--", "bash", "-c", `echo "pw=$DB_PASSWORD region=$REGION"; echo "err=$DB_PASSWORD" >&2; exit 7`}, "")
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

func TestPolicy_EnvAskSuggestsSafeVerbs(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Bash", `{"command":"cat .env"}`))
	if code != 0 || !strings.Contains(askReason(t, out), "tq dotenv keys") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = runHook(t, []string{"claude-hook", "pre-tool-use"}, toolPayload(t, dir, "Bash", `{"command":"tq dotenv keys .env"}`))
	if code != 0 || out != "" {
		t.Fatalf("tq dotenv keys must not ask: code=%d out=%q", code, out)
	}
}
