package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"

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
