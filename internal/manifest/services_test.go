package manifest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// isolateHome keeps the catalog lookups (data and agent ids) away from the real home.
func isolateHome(t *testing.T) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// The email block names a known provider; an unknown one does not load, and the error names the
// field and the known values.
func TestEmailBlockValidates(t *testing.T) {
	isolateHome(t)
	base := "schema: tentaqles-client-v2\nclient: acme\n"
	if _, err := Load(write(t, base+"email: { provider: gmail }\n")); err != nil {
		t.Fatal(err)
	}
	_, err := Load(write(t, base+"email: { provider: yahoo }\n"))
	if err == nil || !strings.Contains(err.Error(), "email.provider") || !strings.Contains(err.Error(), "outlook") {
		t.Fatalf("error text: %v", err)
	}
}
