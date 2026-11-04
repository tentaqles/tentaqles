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
// tq add scaffolds a folder for anyone, but only a human gets it trusted: under
// an agent marker the new manifest stays untrusted and the output hands the
// human the tq allow line.
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
		if err != nil {
			t.Fatalf("tq add under %s: %v %q", k, err, out)
		}
		if trust.IsTrusted(manifestHash(t, filepath.Join(base, name))) {
			t.Fatalf("tq add under %s trusted the new folder", k)
		}
		if !strings.Contains(out, "untrusted") || !strings.Contains(out, "tq allow "+name) {
			t.Fatalf("tq add under %s: want the tq allow line, got %q", k, out)
		}
	}
	clearAgentMarkers(t)
	out, err := runTq(t, "add", "human", "--base", base, "--git-email", "anyone@example.com", "--identities", "gh")
	if err != nil || !strings.Contains(out, "(trusted)") {
		t.Fatalf("a human tq add: %v %q", err, out)
	}
	if !trust.IsTrusted(manifestHash(t, filepath.Join(base, "human"))) {
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
