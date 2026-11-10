package cmd

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildTentaqles builds the real program into a temp folder and returns the binary's path. It runs
// before any test isolation, so the Go toolchain finds its module cache.
func buildTentaqles(t *testing.T) string {
	t.Helper()
	name := "tentaqles"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	out, err := exec.Command("go", "build", "-o", bin, filepath.Join("..", "..", "cmd", "tentaqles")).CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// selftestLimit bounds one run of the program. A run that is still going then is killed and fails the
// test, so a stuck selftest never outlives `go test` (one was once left spinning for hours after the
// test binary's own timeout ended it).
const selftestLimit = 8 * time.Minute

// runSelftestBinary runs bin with args in folder dir and returns its stdout, stderr and exit code.
func runSelftestBinary(t *testing.T, bin, dir string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), selftestLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %v still running after %s, killed\nstdout:\n%s\nstderr:\n%s", bin, args, selftestLimit, stdout.String(), stderr.String())
	}
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	ee, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run %s: %v", bin, err)
	}
	return stdout.String(), stderr.String(), ee.ExitCode()
}

func TestLobeSelftestCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	bin := buildTentaqles(t)
	dir := existingClient(t)

	stdout, stderr, code := runSelftestBinary(t, bin, dir, "lobe", "selftest")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	want := "writers=20 lines=10000 torn=0 lost=0\nreplaces=1000 partial=0\nrepo-key app=r_a172cedc\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	outside := t.TempDir()
	stdout, stderr, code = runSelftestBinary(t, bin, outside, "lobe", "selftest")
	if code != 1 {
		t.Fatalf("outside a client: exit %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "run it inside a client's folder") {
		t.Fatalf("outside a client: no refusal line:\n%s%s", stdout, stderr)
	}
}
