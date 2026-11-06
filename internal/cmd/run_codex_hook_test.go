package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// realCodexHookResolves is the check TestMain replaces for the launch tests.
var realCodexHookResolves func([]string) error

// A codex launch needs a tq on the launch PATH: the Codex hook runs a bare
// `tq`, and a tq Codex cannot find runs no guard (ADR-0006).
func TestCodexHookResolves(t *testing.T) {
	empty := t.TempDir()
	if err := realCodexHookResolves([]string{"PATH=" + empty}); err == nil || !strings.Contains(err.Error(), "no guard") {
		t.Fatalf("no tq on PATH: %v", err)
	}
	withTQ := t.TempDir()
	name := "tq"
	if runtime.GOOS == "windows" {
		name = "tq.exe"
	}
	if err := os.WriteFile(filepath.Join(withTQ, name), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := realCodexHookResolves([]string{"Path=" + empty + string(os.PathListSeparator) + withTQ}); err != nil {
		t.Fatalf("tq on PATH: %v", err)
	}
	// The launch itself refuses.
	mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	calls := fakeExec(t)
	prev := codexHookResolves
	codexHookResolves = realCodexHookResolves
	t.Cleanup(func() { codexHookResolves = prev })
	t.Setenv("PATH", empty)
	if _, err := runTq(t, "run", "acme", "--", "codex", "exec", "x"); err == nil || len(*calls) != 0 {
		t.Fatalf("launch with no tq on PATH: err=%v calls=%d", err, len(*calls))
	}
}
