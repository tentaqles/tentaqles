package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func unattendedWorkspace(t *testing.T, body string, bypass bool) *resolve.Workspace {
	t.Helper()
	isolateHub(t)
	root := testutil.TempDir(t)
	mp := filepath.Join(root, manifest.FileName)
	raw := "schema: tentaqles-client-v2\nclient: acme\n" + body
	if err := os.WriteFile(mp, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if bypass {
		if err := trust.AllowBypass(h); err != nil {
			t.Fatal(err)
		}
	}
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func codesOf(w *resolve.Workspace) (map[string]string, int) {
	fs, add := collectHub()
	unattendedChecks(w, add)
	out := map[string]string{}
	for _, f := range *fs {
		out[f.code] = f.msg
	}
	return out, len(*fs)
}

func TestUnattendedChecks(t *testing.T) {
	w := unattendedWorkspace(t, "identities: { gh: {} }\nguard:\n  unattended:\n    workspace: true\n", false)
	got, _ := codesOf(w)
	if _, ok := got["unattended-not-effective"]; !ok {
		t.Fatalf("want not-effective, got %v", got)
	}

	w = unattendedWorkspace(t, "identities: { gh: {} }\nguard:\n  unattended:\n    workspace: true\n    keep_asking: [tq/nope]\n", true)
	got, _ = codesOf(w)
	if _, ok := got["unattended-unknown-rule"]; !ok {
		t.Fatalf("want unknown-rule, got %v", got)
	}

	w = unattendedWorkspace(t, "identities: { gh: {}, az: {} }\nguard:\n  unattended:\n    workspace: true\n", true)
	got, _ = codesOf(w)
	if got["unattended-cloud"] != "unattended + cloud identity: Claude can run cloud CLIs without asking in this workspace" {
		t.Fatalf("want cloud warning, got %v", got)
	}

	w = unattendedWorkspace(t, "identities: { gh: {} }\n", true)
	if _, n := codesOf(w); n != 0 {
		t.Fatalf("unconfigured must be silent")
	}
}
