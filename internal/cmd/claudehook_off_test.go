package cmd

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
)

// Under TQ_MODE=off every claude-hook event exits 0 with no output, even
// for a payload the guard would deny (spec E §8.4). TQ_ENABLED=0 only skips
// shell activation: the hooks still run (security m3).
func TestClaudeHookOffExitsSilently(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	payload := hookPayloadFor(t, root, "cat .env")
	for _, env := range []struct{ k, v string }{{"TQ_MODE", "off"}} {
		for _, ev := range []string{"pre-tool-use", "session-start", "stop", "prompt-submit"} {
			t.Run(env.k+"="+env.v+"/"+ev, func(t *testing.T) {
				t.Setenv("TQ_MODE", "")
				t.Setenv("TQ_ENABLED", "")
				t.Setenv(env.k, env.v)
				code, out, errOut := runHook(t, []string{"claude-hook", ev}, payload)
				if code != 0 || out != "" || errOut != "" {
					t.Fatalf("want silent exit 0, got %d stdout=%q stderr=%q", code, out, errOut)
				}
			})
		}
	}
}

func TestHookOff(t *testing.T) {
	cases := []struct {
		mode, enabled string
		want          bool
	}{
		{"", "", false},
		{"off", "", true},
		// one matcher everywhere: exactly off (correctness m6)
		{"OFF", "", false},
		{" off ", "", false},
		{"default", "", false},
		{"relaxed", "", false},
		// TQ_ENABLED=0 is shell activation only (security m3)
		{"", "0", false},
		{"", "1", false},
		{"strict", "0", false},
		{"off", "0", true},
	}
	for _, c := range cases {
		t.Setenv("TQ_MODE", c.mode)
		t.Setenv("TQ_ENABLED", c.enabled)
		if got := hookOff(); got != c.want {
			t.Errorf("TQ_MODE=%q TQ_ENABLED=%q: hookOff = %v, want %v", c.mode, c.enabled, got, c.want)
		}
	}
	// The guard is still on without the env: a deny payload is denied.
	t.Setenv("TQ_MODE", "")
	t.Setenv("TQ_ENABLED", "")
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "cat .env"))
	if code == 0 {
		t.Fatal("with tq on, cat .env must be blocked")
	}
}

// TQ_ENABLED=0 no longer switches the guard off: a deny payload is still
// denied from a legacy kill-switch window (security m3).
func TestHookRunsUnderTQEnabledZero(t *testing.T) {
	root := setupTrustedWorkspaceWithManifest(t, "acme", "")
	t.Setenv("TQ_MODE", "")
	t.Setenv("TQ_ENABLED", "0")
	if code, _, _ := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, root, "cat .env")); code != 2 {
		t.Fatalf("TQ_ENABLED=0 must not switch the guard off: code %d", code)
	}
	if !guardcfg.IsOff("off") || guardcfg.IsOff("OFF") || guardcfg.IsOff(" off") {
		t.Fatal("IsOff is exactly off")
	}
}
