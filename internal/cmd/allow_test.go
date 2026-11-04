package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// manifestHash hashes the manifest at root as trust does.
func manifestHash(t *testing.T, root string) string {
	t.Helper()
	h, err := trust.HashFile(filepath.Join(root, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Trust is by file hash: a hand edit of a trusted manifest drops its trust
// (tq run refuses it) until the human runs tq allow again (FR-16).
func TestEditedManifestLosesTrust(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	if !trust.IsTrusted(manifestHash(t, root)) {
		t.Fatal("mkLaunchWS trusts the manifest")
	}
	mp := filepath.Join(root, manifest.FileName)
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, append(b, "language: en\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if trust.IsTrusted(manifestHash(t, root)) {
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
	if !trust.IsTrusted(manifestHash(t, root)) {
		t.Fatal("tq allow trusts the edited manifest as it is now")
	}
}

// tq allow and tq deny refuse an agent session by its environment marker:
// a session cannot trust (or untrust) a folder itself (FR-11).
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
	if !trust.IsTrusted(manifestHash(t, root)) {
		t.Fatal("a refused deny changes nothing")
	}
	clearAgentMarkers(t)
	if out, err := runTq(t, "deny", "acme"); err != nil || !strings.Contains(out, "Denied acme") {
		t.Fatalf("a human deny: %v %q", err, out)
	}
	if trust.IsTrusted(manifestHash(t, root)) {
		t.Fatal("deny removes the trust")
	}
}
