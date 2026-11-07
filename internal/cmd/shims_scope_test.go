package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// Round 3, L-17: another client's browser shim is another identity's file. A session in client A
// must not run client B's tq-open by its path: B's record verifies, so the shim would open B's
// profile. Its own shim stays usable by path.
func TestPreToolUse_OtherClientShimByPathDenied(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	// beta is a second trusted-looking client in the same base; its shims folder is written
	// the way tq settings render writes it.
	shimDir := paths.ShimsDir("beta")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(shimDir, "tq-open"+exeSuffix())
	if err := os.WriteFile(other, []byte("shim"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		`"` + other + `" https://example.com`,
		`"` + filepath.ToSlash(other) + `" https://example.com`,
	} {
		code, category, rule, errOut := hookDecision(t, root, "Bash", cmdIn(cmd))
		if code != 2 || category != policy.CategoryIdentity {
			t.Fatalf("%s: code=%d category=%q rule=%q: %s", cmd, code, category, rule, errOut)
		}
		if rule != policy.OutOfScopeHostID && rule != policy.OtherIdentityID && rule != policy.OtherBrowserProfileID {
			t.Fatalf("%s: rule %q, want an identity deny of another identity's folder: %s", cmd, rule, errOut)
		}
	}
}

// Round 3, L-17, the other half: a session in client A running A's own tq-open by its path is
// not denied by the other-identity rule.
func TestPreToolUse_OwnShimByPathNotDeniedAsOther(t *testing.T) {
	root, _ := setupScoped(t, "company-x", browserBody)
	shimDir := paths.ShimsDir("company-x")
	if err := os.MkdirAll(shimDir, 0o700); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(shimDir, "tq-open"+exeSuffix())
	if err := os.WriteFile(own, []byte("shim"), 0o700); err != nil {
		t.Fatal(err)
	}
	code, _, rule, errOut := hookDecision(t, root, "Bash", cmdIn(`"`+own+`" https://example.com`))
	if rule == policy.OtherIdentityID {
		t.Fatalf("A's own shim denied as another identity's: %s", errOut)
	}
	if code != 0 {
		t.Fatalf("A's own shim by path denied (rule %q): %s", rule, errOut)
	}
}
