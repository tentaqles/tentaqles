package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// modeWorkspace writes acme with body; grant is "" (none), "relaxed" or
// "bypass" (a legacy .bypass file).
func modeWorkspace(t *testing.T, body, grant string) *resolve.Workspace {
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
	switch grant {
	case "relaxed":
		if err := trust.SaveGrant(h, trust.Grant{Mode: "relaxed", Workspace: "acme"}); err != nil {
			t.Fatal(err)
		}
	case "bypass":
		if err := trust.AllowBypass(h); err != nil {
			t.Fatal(err)
		}
	}
	return &resolve.Workspace{Name: "acme", Root: root, ManifestPath: mp, Hash: h, Manifest: m}
}

func modeCodes(w *resolve.Workspace) (map[string]string, map[string]string, int) {
	fs, add := collectHub()
	modeChecks(w, Deps{}, add)
	msgs, fixes := map[string]string{}, map[string]string{}
	for _, f := range *fs {
		msgs[f.code] = f.msg
		fixes[f.code] = f.fix
	}
	return msgs, fixes, len(*fs)
}

func TestModeChecks(t *testing.T) {
	w := modeWorkspace(t, "identities: { gh: {} }\nguard:\n  mode: relaxed\n", "")
	_, fixes, _ := modeCodes(w)
	if fixes["mode-not-granted"] != "tq allow --mode acme relaxed" {
		t.Fatalf("want not-granted, got %v", fixes)
	}
	w = modeWorkspace(t, "identities: { gh: {} }\nguard:\n  unattended:\n    projects: [app]\n", "")
	if _, fixes, _ := modeCodes(w); fixes["mode-not-granted"] != "tq allow --mode acme relaxed" {
		t.Fatalf("a legacy block without a grant: %v", fixes)
	}
	w = modeWorkspace(t, "identities: { gh: {} }\nguard:\n  mode: custom\n  base: relaxed\n  categories: {system: skip}\n", "relaxed")
	if msgs, fixes, _ := modeCodes(w); fixes["mode-not-granted"] != "tq allow --mode acme custom" || !strings.Contains(msgs["mode-not-granted"], "system") {
		t.Fatalf("a custom table looser than the relaxed grant: %v", msgs)
	}

	w = modeWorkspace(t, "identities: { gh: {} }\nguard:\n  mode: relaxed\n  rules_override: {tq/nope: ask}\n", "relaxed")
	msgs, _, _ := modeCodes(w)
	if !strings.Contains(msgs["mode-unknown-rule"], "tq/nope") {
		t.Fatalf("want unknown-rule, got %v", msgs)
	}
	w = modeWorkspace(t, "identities: { gh: {} }\nguard:\n  unattended:\n    workspace: true\n    keep_asking: [tq/nope2]\n", "bypass")
	if msgs, _, _ := modeCodes(w); !strings.Contains(msgs["mode-unknown-rule"], "tq/nope2") {
		t.Fatalf("legacy keep_asking unknown: %v", msgs)
	}

	w = modeWorkspace(t, "identities: { gh: {}, az: {} }\nguard:\n  mode: relaxed\n", "relaxed")
	if msgs, _, _ := modeCodes(w); !strings.HasPrefix(msgs["mode-cloud"], "guard mode skips prod-changes or cloud-destructive with a cloud identity") {
		t.Fatalf("want cloud warning, got %v", msgs)
	}
	w = modeWorkspace(t, "identities: { gh: {}, az: {} }\nguard:\n  mode: relaxed\n", "")
	if msgs, _, _ := modeCodes(w); msgs["mode-cloud"] != "" {
		t.Fatal("an ungranted relaxed runs at default: no cloud warning")
	}

	w = modeWorkspace(t, "identities: { gh: {} }\nguard:\n  disable: [tq/push-pii, tq/force-push]\n  actions: {tq/env-dump: ask}\n", "")
	msgs, fixes, _ = modeCodes(w)
	if got := msgs["guard-locked-override"]; !strings.Contains(got, "disable:tq/push-pii") || !strings.Contains(got, "actions:tq/env-dump") || strings.Contains(got, "force-push") || fixes["guard-locked-override"] == "" {
		t.Fatalf("want guard-locked-override, got %v", msgs)
	}

	for _, body := range []string{"identities: { gh: {} }\n", "identities: { gh: {} }\nguard:\n  mode: strict\n"} {
		w = modeWorkspace(t, body, "")
		if _, _, n := modeCodes(w); n != 0 {
			t.Fatalf("%q must be silent", body)
		}
	}
}

func TestGlobalModeChecks(t *testing.T) {
	isolateHub(t)
	f := guardcfg.File{Mode: guardcfg.ModeRelaxed, Disable: []string{"tq/text-pii"}, RulesOverride: map[string]guardcfg.Value{"me/x": guardcfg.ValueAsk}}
	if err := guardcfg.Save(f); err != nil {
		t.Fatal(err)
	}
	fs, add := collectHub()
	globalModeChecks(Deps{}, add)
	codes := map[string]string{}
	for _, x := range *fs {
		codes[x.code] = x.fix + " | " + x.msg
	}
	if !strings.HasPrefix(codes["mode-not-granted"], "tq allow --mode --global relaxed") ||
		!strings.Contains(codes["mode-unknown-rule"], "me/x") || !strings.Contains(codes["guard-locked-override"], "disable:tq/text-pii") {
		t.Fatalf("global findings: %v", codes)
	}
	gr, _ := guardcfg.GlobalGrantFor(f, guardcfg.ModeRelaxed)
	if err := trust.SaveGrant(trust.GlobalGrantKey, gr); err != nil {
		t.Fatal(err)
	}
	fs, add = collectHub()
	globalModeChecks(Deps{}, add)
	for _, x := range *fs {
		if x.code == "mode-not-granted" {
			t.Fatalf("granted: %+v", x)
		}
	}
	found := false
	for _, c := range Registered() {
		found = found || c.Name == "mode"
	}
	if !found {
		t.Fatal("the mode check is registered")
	}
}
