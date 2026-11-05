package resolve

import (
	"os"
	"path/filepath"
	"strings"
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
	os.WriteFile(settings, []byte("{}\n"), 0o600)
	r := Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted == nil || !r.Mismatch {
		t.Fatalf("%+v", r)
	}
	if want := "untrusted: integrity mismatch, added " + settings + " (run: tq hooks verify acme)"; r.Reason != want {
		t.Fatalf("reason %q, want %q", r.Reason, want)
	}
	h, _ := trust.HashFile(pp)
	if err := trust.Deny("acme", h); err != nil {
		t.Fatal(err)
	}
	r = Resolve(root, cfg)
	if r.Workspace != nil || r.Untrusted == nil || r.Mismatch || !strings.HasPrefix(r.Reason, "untrusted (run: tq allow acme)") {
		t.Fatalf("%+v", r)
	}
}
