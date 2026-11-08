package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/browser"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestDoctor_BrowserProfileMissing(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	data := testutil.TempDir(t)
	prev := browser.UserDataRoot
	browser.UserDataRoot = func(string) string { return data }
	t.Cleanup(func() { browser.UserDataRoot = prev })

	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	body := "schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {} }\n" +
		"browsers:\n  - name: chrome\n    profile: Profile 7\n  - name: edge\n    profile: Default\n"
	os.WriteFile(markerFor(t, root), []byte(body), 0o600)
	// edge's profile exists, chrome's does not
	os.MkdirAll(filepath.Join(data, "Default"), 0o755)

	fs := Run(cfg, deps(nil, root, nil))
	var got []Finding
	for _, f := range fs {
		if f.Code == "browser-profile-missing" {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("browser-profile-missing findings = %+v", got)
	}
	f := got[0]
	if f.Level != "warn" || f.Workspace != "acme" || !strings.Contains(f.Msg, "Profile 7") || !strings.Contains(f.Msg, "chrome") || f.Fix == "" {
		t.Fatalf("the finding does not name the profile and the fix: %+v", f)
	}
	if strings.Contains(f.Msg, "Default") {
		t.Fatalf("a profile that exists was reported: %+v", f)
	}

	os.MkdirAll(filepath.Join(data, "Profile 7"), 0o755)
	if fs = Run(cfg, deps(nil, root, nil)); has(fs, "browser-profile-missing") {
		t.Fatalf("a profile that exists is still reported: %+v", fs)
	}
}

// A client with no browsers: list gets no browser finding.
func TestDoctor_NoBrowsersNoFinding(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, manifest.FileName),
		[]byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {} }\n"), 0o600)
	fs := Run(cfg, deps(nil, root, nil))
	if has(fs, "browser-profile-missing") || has(fs, "browser-shims-missing") {
		t.Fatalf("%+v", fs)
	}
}
