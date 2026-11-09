package paths

import (
	"path/filepath"
	"testing"
)

func TestLobeDirIsUnderTheIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TQ_HOME", dir)
	want := filepath.Join(dir, "identities", "acme", "lobe")
	if got := LobeDir("acme"); got != want {
		t.Fatalf("LobeDir = %q, want %q", got, want)
	}
	if got := IdentityDir("acme", "lobe"); LobeDir("acme") != got {
		t.Fatalf("LobeDir %q != IdentityDir %q", LobeDir("acme"), got)
	}
}
