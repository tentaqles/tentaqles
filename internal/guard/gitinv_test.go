package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInlineIdentityOverride_WrappedForms(t *testing.T) {
	for _, c := range []string{
		`env git -c user.email=x@y.z commit -m x`,
		`"git" -c user.email=x@y.z commit -m x`,
	} {
		if !HasInlineIdentityOverride(c) {
			t.Errorf("%q should be an inline identity override", c)
		}
	}
}
