package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Under an agent marker TQ_HOME is ignored unless AllowHomeOverride is set:
// the hook inherits the harness environment, and an agent-writable settings
// file can point TQ_HOME at a forged state folder.
func TestHome_IgnoresTQHomeUnderAnAgentMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	forged := filepath.Join(t.TempDir(), "forged")
	t.Setenv("TQ_HOME", forged)
	for _, k := range agentMarkers {
		t.Setenv(k, "")
	}
	prev := AllowHomeOverride
	t.Cleanup(func() { AllowHomeOverride = prev })

	AllowHomeOverride = false
	if got := Home(); got != forged {
		t.Fatalf("no marker: TQ_HOME applies, got %q", got)
	}
	t.Setenv("CLAUDECODE", "1")
	want := filepath.Join(home, ".tentaqles")
	if got := Home(); got != want {
		t.Fatalf("under a marker Home() must be the default %q, got %q", want, got)
	}
	if DefaultHome() != want {
		t.Fatalf("DefaultHome = %q", DefaultHome())
	}
	if HomeOverride() != forged {
		t.Fatalf("HomeOverride = %q", HomeOverride())
	}
	AllowHomeOverride = true
	if got := Home(); got != forged {
		t.Fatalf("with the override allowed TQ_HOME applies, got %q", got)
	}
	// TQ_HOME equal to the default is not an override.
	t.Setenv("TQ_HOME", want)
	if HomeOverride() != "" {
		t.Fatalf("HomeOverride = %q", HomeOverride())
	}
	t.Setenv("TQ_HOME", "")
	if got := Home(); got != want {
		t.Fatalf("unset TQ_HOME: %q", got)
	}
}
