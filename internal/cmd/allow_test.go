package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// manifestHash hashes the manifest tq reads for root as trust does: the
// protected copy when the root holds a marker, the root file otherwise.
func manifestHash(t *testing.T, root string) string {
	t.Helper()
	mp, _, reason := resolve.ManifestFile(root)
	if reason != "" {
		t.Fatalf("%s: %s", root, reason)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Trust is by file hash: a hand edit of a trusted manifest drops its trust
// (tq run refuses it) until the human runs tq allow again (FR-16). The same
// holds in both layouts: the manifest in the root (old) and the protected
// copy under $TQ_HOME with a marker in the root (ADR-0002); in the protected
// layout the hash is the protected file's, so editing the marker alone does
// not touch it (TestResolve_EditedProtectedManifestLosesTrust_MarkerEditDoesNot).
func TestEditedManifestLosesTrust(t *testing.T) {
	for _, layout := range []string{"root", "protected"} {
		t.Run(layout, func(t *testing.T) {
			_, mp := mkLaunchWSLayout(t, "acme", "", layout == "protected")
			clearAgentMarkers(t)
			hash := func() string {
				h, err := trust.HashFile(mp)
				if err != nil {
					t.Fatal(err)
				}
				return h
			}
			if !trust.IsTrusted("acme", hash()) {
				t.Fatal("mkLaunchWSLayout trusts the manifest")
			}
			b, err := os.ReadFile(mp)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(mp, append(b, "language: en\n"...), 0o600); err != nil {
				t.Fatal(err)
			}
			if trust.IsTrusted("acme", hash()) {
				t.Fatal("an edited manifest must lose its trust")
			}
			calls := fakeExec(t)
			if _, err := runTq(t, "run", "acme", "--", "claude"); err == nil || !strings.Contains(err.Error(), "untrusted") || len(*calls) != 0 {
				t.Fatalf("tq run must refuse the edited manifest: %v %+v", err, *calls)
			}
			out, err := runTq(t, "allow", "acme")
			if err != nil || !strings.Contains(out, "Trusted acme") {
				t.Fatalf("tq allow: %v %q", err, out)
			}
			if !trust.IsTrusted("acme", hash()) {
				t.Fatal("tq allow trusts the edited manifest as it is now")
			}
		})
	}
}

// tq allow and tq deny refuse an agent session by its environment marker:
// a session cannot trust (or untrust) a folder itself (FR-11).
// tq add is human-only too (PRD v2 §8.1): under an agent marker it refuses
// before anything is written, so a session can never get a folder scaffolded,
// let alone trusted. A human at a terminal who confirms gets it trusted.
func TestAddDoesNotTrustFromAgent(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	base := filepath.Dir(root)
	prevRunGit := addRunGit
	addRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { addRunGit = prevRunGit })
	for _, k := range agentMarkers {
		clearAgentMarkers(t)
		t.Setenv(k, "1")
		name := "shadow-" + strings.ToLower(strings.ReplaceAll(k, "_", "-"))
		out, err := runTq(t, "add", name, "--base", base, "--git-email", "anyone@example.com", "--identities", "gh")
		if !errors.Is(err, errHumanOnly) {
			t.Fatalf("tq add under %s: want errHumanOnly, got %v %q", k, err, out)
		}
		if _, err := os.Stat(filepath.Join(base, name)); !os.IsNotExist(err) {
			t.Fatalf("tq add under %s scaffolded the folder", k)
		}
		if _, err := os.Stat(filepath.Join(paths.IdentitiesRoot(), name)); !os.IsNotExist(err) {
			t.Fatalf("tq add under %s wrote an identity dir", k)
		}
	}
	clearAgentMarkers(t)
	out, err := runTq(t, "add", "human", "--base", base, "--git-email", "anyone@example.com", "--identities", "gh")
	if err != nil || !strings.Contains(out, "(trusted)") {
		t.Fatalf("a human tq add: %v %q", err, out)
	}
	if !trust.IsTrusted("human", manifestHash(t, filepath.Join(base, "human"))) {
		t.Fatal("a human tq add trusts the new folder")
	}
}

func TestAllowAndDenyRefuseAgents(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	for _, verb := range []string{"allow", "deny"} {
		for _, k := range agentMarkers {
			clearAgentMarkers(t)
			t.Setenv(k, "1")
			if _, err := runTq(t, verb, "acme"); !errors.Is(err, errHumanOnly) {
				t.Errorf("tq %s under %s: want ErrHumanOnly, got %v", verb, k, err)
			}
		}
	}
	if !trust.IsTrusted("acme", manifestHash(t, root)) {
		t.Fatal("a refused deny changes nothing")
	}
	clearAgentMarkers(t)
	if out, err := runTq(t, "deny", "acme"); err != nil || !strings.Contains(out, "Denied acme") {
		t.Fatalf("a human deny: %v %q", err, out)
	}
	if trust.IsTrusted("acme", manifestHash(t, root)) {
		t.Fatal("deny removes the trust")
	}
}
