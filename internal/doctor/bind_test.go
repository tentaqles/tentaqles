package doctor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestDoctor_FlagsAnUnrecordedClient(t *testing.T) {
	cfg, root := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := deps(map[string]string{}, root, map[string]string{"user.useConfigOnly": "true"})

	fs := Run(cfg, d)
	var got []Finding
	for _, f := range fs {
		if f.Code == "client-folder-unrecorded" {
			got = append(got, f)
		}
	}
	if len(got) != 1 || got[0].Workspace != "acme" || !strings.Contains(got[0].Msg, "acme") {
		t.Fatalf("want one client-folder-unrecorded finding naming acme, got %+v", got)
	}

	if err := cfg.Bind("acme", root); err != nil {
		t.Fatal(err)
	}
	if fs := Run(cfg, d); has(fs, "client-folder-unrecorded") {
		t.Fatalf("a recorded client is still reported unrecorded: %+v", fs)
	}
}

func TestDoctor_FlagsARefusedCopy(t *testing.T) {
	cfg, root := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	if err := cfg.Bind("acme", root); err != nil {
		t.Fatal(err)
	}
	two := testutil.TempDir(t)
	if _, err := cfg.AddBase(two); err != nil {
		t.Fatal(err)
	}
	// markerFor writes the copy's marker only; the manifest it points at is the recorded client's.
	markerFor(t, filepath.Join(two, "acme"))

	fs := Run(cfg, deps(map[string]string{}, root, map[string]string{"user.useConfigOnly": "true"}))
	if !has(fs, "client-folder-refused") {
		t.Fatalf("the copy is not reported as client-folder-refused: %+v", fs)
	}
	// The recorded folder is sound, so the copy is the only error source; a manifest-invalid finding
	// would mean the copy was reported as a broken manifest.
	if has(fs, "manifest-invalid") {
		t.Fatalf("the refused copy is reported as manifest-invalid: %+v", fs)
	}
}
