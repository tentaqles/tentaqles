package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashAllowDeny(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(f, []byte("a: 1\n"), 0o600)
	h, err := HashFile(f)
	if err != nil || len(h) != 64 {
		t.Fatalf("hash %q %v", h, err)
	}
	if IsTrusted(h) {
		t.Fatal("should start untrusted")
	}
	if err := Allow(h); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted(h) {
		t.Fatal("allow failed")
	}
	if IsBypassAllowed(h) {
		t.Fatal("bypass must be separate")
	}
	if err := AllowBypass(h); err != nil || !IsBypassAllowed(h) {
		t.Fatal("bypass allow failed")
	}
	if err := Deny(h); err != nil {
		t.Fatal(err)
	}
	if IsTrusted(h) || IsBypassAllowed(h) {
		t.Fatal("deny must clear both")
	}
}

func TestHashChangesWhenFileChanges(t *testing.T) {
	f := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(f, []byte("a: 1\n"), 0o600)
	h1, _ := HashFile(f)
	os.WriteFile(f, []byte("a: 2\n"), 0o600)
	h2, _ := HashFile(f)
	if h1 == h2 {
		t.Fatal("hash must change")
	}
}
