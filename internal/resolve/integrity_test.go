package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// A covered file edited after tq allow resolves untrusted with Mismatch set
// and a reason naming the file and tq hooks verify; a never-allowed
// workspace is plain untrusted with tq allow as the fix.
func TestResolve_IntegrityMismatchIsUntrustedWithReason(t *testing.T) {
	base, cfg := setup(t)
	root, pp := mkProtected(t, base, "acme", true)
	if r := Resolve(root, cfg); r.Workspace == nil {
		t.Fatalf("%+v", r)
	}
	settings := filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o700)
	os.WriteFile(settings, []byte(`{"theme":"dark"}`), 0o600)
	if r := Resolve(root, cfg); r.Workspace == nil {
		t.Fatalf("a settings.json without enforcing keys is no change: %+v", r)
	}
	os.WriteFile(settings, []byte(`{"hooks":{"PreToolUse":[]}}`), 0o600)
	r := Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted == nil || !r.Mismatch || r.Legacy {
		t.Fatalf("%+v", r)
	}
	if want := "untrusted: integrity mismatch, changed " + trust.Key(settings+"#tq") + " (run: tq hooks verify acme)"; r.Reason != want {
		t.Fatalf("reason %q, want %q", r.Reason, want)
	}
	h, _ := trust.HashFile(pp)
	if err := trust.Deny("acme", h); err != nil {
		t.Fatal(err)
	}
	r = Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted == nil || r.Mismatch || r.Reason != "untrusted: trust revoked by tq deny (run: tq allow acme)" {
		t.Fatalf("a revoked workspace is untrusted with tq allow as the fix: %+v", r)
	}
}
