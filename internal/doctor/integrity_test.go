package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/zalando/go-keyring"
)

func findingOf(fs []Finding, code string) *Finding {
	for i := range fs {
		if fs[i].Code == code {
			return &fs[i]
		}
	}
	return nil
}

// integrity lists every covered file that changed since a human signed; it
// replaces the plain untrusted finding, whose fix would be wrong.
func TestRun_IntegrityListsTheChangedFiles(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	mp := filepath.Join(ws, manifest.FileName)
	settings := filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(settings, []byte("{}\n"), 0o600)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: dev@acme.com }\nidentities: { claude: {} }\n# edited\n"), 0o600)
	fs := Run(cfg, deps(nil, ws, nil))
	f := findingOf(fs, "integrity")
	if f == nil || f.Level != "error" || f.Workspace != "acme" {
		t.Fatalf("%+v", fs)
	}
	for _, want := range []string{"2 covered file(s) changed", "added " + settings, "changed " + mp} {
		if !strings.Contains(f.Msg, want) {
			t.Fatalf("message lacks %q: %s", want, f.Msg)
		}
	}
	if !strings.Contains(f.Fix, "tq hooks verify acme") || !strings.Contains(f.Fix, "tq hooks verify --accept acme") {
		t.Fatalf("fix: %s", f.Fix)
	}
	if has(fs, "untrusted") {
		t.Fatal("a mismatch is not the plain untrusted")
	}
	if Exit(fs) != 1 {
		t.Fatal("an integrity error exits 1")
	}
	// A record whose signature fails is reported as such.
	raw, _ := os.ReadFile(trust.SignedRecordPath("acme"))
	os.WriteFile(trust.SignedRecordPath("acme"), []byte(strings.Replace(string(raw), `"signature": "`, `"signature": "00`, 1)), 0o600)
	fs = Run(cfg, deps(nil, ws, nil))
	if f := findingOf(fs, "integrity"); f == nil || !strings.Contains(f.Msg, "integrity record signature invalid") {
		t.Fatalf("%+v", fs)
	}
}

// The plain-file key fallback is reported while the file exists.
func TestRun_IntegrityKeyFileWarns(t *testing.T) {
	keyring.MockInitWithError(errors.New("no keychain"))
	t.Cleanup(keyring.MockInit)
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	if !trust.KeyFromFile() {
		t.Fatal("precondition: the key fell back to the file")
	}
	fs := Run(cfg, deps(nil, ws, nil))
	f := findingOf(fs, "integrity-key-file")
	if f == nil || f.Level != "warn" || !strings.Contains(f.Msg, trust.KeyFile()) || !strings.Contains(f.Fix, "tq allow <name>") {
		t.Fatalf("%+v", fs)
	}
	if has(fs, "integrity") || has(fs, "untrusted") {
		t.Fatalf("the workspace is trusted with the file key: %+v", fs)
	}
}

// A workspace trusted through a marker of an older tq is reported until a
// signed record replaces it.
func TestRun_IntegrityLegacyRecordWarns(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	h, _ := trust.HashFile(filepath.Join(ws, manifest.FileName))
	if err := os.Remove(trust.SignedRecordPath("acme")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(trust.LegacyRecordPath(h), nil, 0o600)
	fs := Run(cfg, deps(nil, ws, nil))
	f := findingOf(fs, "integrity-record-legacy")
	if f == nil || f.Level != "warn" || f.Workspace != "acme" || !strings.Contains(f.Fix, "tq allow acme") {
		t.Fatalf("%+v", fs)
	}
	if has(fs, "untrusted") || has(fs, "integrity") {
		t.Fatalf("a legacy marker still trusts: %+v", fs)
	}
	if err := trust.Allow("acme", h); err != nil {
		t.Fatal(err)
	}
	if fs := Run(cfg, deps(nil, ws, nil)); has(fs, "integrity-record-legacy") {
		t.Fatalf("the signed record silences it: %+v", fs)
	}
}
