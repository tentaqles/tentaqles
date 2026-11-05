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
	if IsTrusted("acme", h) {
		t.Fatal("should start untrusted")
	}
	if err := Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted("acme", h) || !HasRecord("acme", h) {
		t.Fatal("allow failed")
	}
	if _, err := os.Stat(RecordPath("acme", h)); err != nil {
		t.Fatalf("the record is trust/<name>/<hash>: %v", err)
	}
	if err := Deny("acme", h); err != nil {
		t.Fatal(err)
	}
	if IsTrusted("acme", h) {
		t.Fatal("deny must clear the trust")
	}
	if err := Deny("acme", h); err != nil {
		t.Fatalf("a second deny is not an error: %v", err)
	}
}

// The key is name plus hash: the same bytes trusted for acme are not trusted
// for another folder name.
func TestTrustIsKeyedByWorkspaceName(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(f, []byte("client: acme\n"), 0o600)
	h, _ := HashFile(f)
	if err := Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	if !IsTrusted("acme", h) {
		t.Fatal("acme")
	}
	if IsTrusted("evil", h) || HasRecord("evil", h) {
		t.Fatal("the record belongs to acme alone")
	}
	if HasLegacyRecord(h) {
		t.Fatal("Allow writes no legacy record")
	}
}

// A hash-only record of an older tq still counts until it is rekeyed; Deny
// removes it too.
func TestLegacyRecordCountsUntilRekeyedAndDenyRemovesIt(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	f := filepath.Join(t.TempDir(), "m.yaml")
	os.WriteFile(f, []byte("client: acme\n"), 0o600)
	h, _ := HashFile(f)
	if err := touch(LegacyRecordPath(h)); err != nil {
		t.Fatal(err)
	}
	if !HasLegacyRecord(h) || HasRecord("acme", h) {
		t.Fatal("legacy record only")
	}
	if !IsTrusted("acme", h) {
		t.Fatal("a legacy record counts for the name resolve checked")
	}
	if err := Deny("acme", h); err != nil {
		t.Fatal(err)
	}
	if HasLegacyRecord(h) || IsTrusted("acme", h) {
		t.Fatal("Deny must remove the legacy record as well")
	}
}

func TestInvalidKeysAreNeverTrusted(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	h := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "../acme"} {
		if err := Allow(name, h); err == nil {
			t.Fatalf("Allow(%q) must refuse", name)
		}
		if IsTrusted(name, h) {
			t.Fatalf("IsTrusted(%q) must be false", name)
		}
	}
	if err := Allow("acme", "../"+h); err == nil {
		t.Fatal("a hash with a separator must be refused")
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
