package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// IDEATION, line 52: in a folder that already is a client, reach offers a menu that changes the setup
// through the same form. Each change is human-only, confirmed, and signed again like tq allow.

// existingClient makes acme with fullAnswers through reach, and returns its folder.
func existingClient(t *testing.T) string {
	t.Helper()
	clients := isolateReach(t, fullAnswers())
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir, "--no-terminal"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return dir
}

// menu answers the menu with part and the form with edit applied to the client's current answers.
func menu(t *testing.T, part string, edit func(a *reach.Answers)) {
	t.Helper()
	reachMenu = func() (string, error) { return part, nil }
	reachEditForm = func(p string, cur *reach.Answers) (*reach.Answers, error) {
		if p != part {
			t.Fatalf("form part %q, menu said %q", p, part)
		}
		next := *cur
		next.Clouds, next.Team, next.Browsers = slices.Clone(cur.Clouds), slices.Clone(cur.Team), slices.Clone(cur.Browsers)
		edit(&next)
		return &next, nil
	}
}

func readManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(paths.ManifestPath("acme"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReach_ExistingClientChangesProviders(t *testing.T) {
	dir := existingClient(t)
	var installed []string
	reachInstall = func(_ *cobra.Command, ws *resolve.Workspace) { installed = ws.Manifest.Scope() }
	menu(t, reachPartProviders, func(a *reach.Answers) {
		a.Clouds = []string{"azure", "aws"}
		a.Team = nil
		a.Task = "linear"
	})
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t)
	for _, id := range []string{"aws", "az", "linear", "snowflake"} {
		if !slices.Contains(m.Scope(), id) {
			t.Errorf("%s not in scope %v", id, m.Scope())
		}
	}
	for _, id := range []string{"slack", "jira"} {
		if slices.Contains(m.Scope(), id) {
			t.Errorf("%s still in scope %v", id, m.Scope())
		}
	}
	if !trust.IsTrusted("acme", manifestHash(t, dir)) {
		t.Fatalf("the edited manifest is not signed:\n%s", out)
	}
	if fi, err := os.Stat(paths.IdentityDir("acme", "aws")); err != nil || !fi.IsDir() {
		t.Errorf("no folder for the new aws identity: %v", err)
	}
	if !slices.Contains(installed, "aws") {
		t.Errorf("the CLIs were not installed after a provider was added: %v", installed)
	}
	for _, want := range []string{"about to change the client acme", "clouds: azure -> azure, aws", "acme is updated. It can use:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// The guard reads the new scope at once.
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir("acme", "claude"))
	if code, _, _, errOut := hookDecision(t, dir, "Bash", cmdIn("aws s3 ls")); code != 0 {
		t.Errorf("aws, now in scope, is denied: %s", errOut)
	}
}

func TestReach_ExistingClientChangesEmail(t *testing.T) {
	dir := existingClient(t)
	reachInstall = func(*cobra.Command, *resolve.Workspace) { t.Error("nothing to install for an email change") }
	menu(t, reachPartEmail, func(a *reach.Answers) { a.Email = "dev@acme.test" })
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t); m.Git.Email != "dev@acme.test" {
		t.Fatalf("git.email = %q", m.Git.Email)
	}
	b, err := os.ReadFile(gitcfg.WorkspaceFile(dir))
	if err != nil || !strings.Contains(string(b), "email = dev@acme.test") {
		t.Fatalf("the folder's git config: %v %s", err, b)
	}
	if !trust.IsTrusted("acme", manifestHash(t, dir)) {
		t.Fatal("the edited manifest is not signed")
	}
}

func TestReach_ExistingClientChangesBrowser(t *testing.T) {
	dir := existingClient(t)
	menu(t, reachPartBrowsers, func(a *reach.Answers) {
		a.Browsers = []manifest.Browser{{Name: "chrome", Profile: "Work"}}
	})
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t)
	if len(m.Browsers) != 1 || m.Browsers[0] != (manifest.Browser{Name: "chrome", Profile: "Work"}) {
		t.Fatalf("browsers: %+v", m.Browsers)
	}
	if !strings.Contains(out, "browsers: edge (Profile 3) -> chrome (Work)") {
		t.Errorf("the confirmation does not name the change:\n%s", out)
	}
}

// A declined yes writes nothing: the manifest, its trust and the git config stay as they were.
func TestReach_ExistingClientDeclinedChangesNothing(t *testing.T) {
	dir := existingClient(t)
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	h := manifestHash(t, dir)
	menu(t, reachPartEmail, func(a *reach.Answers) { a.Email = "dev@acme.test" })
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	if out, err := runReach(t, dir, "--no-banner"); !errors.Is(err, humanonly.ErrAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	after, _ := os.ReadFile(paths.ManifestPath("acme"))
	if !bytes.Equal(before, after) || !trust.IsTrusted("acme", h) {
		t.Fatal("a declined edit changed the manifest or its trust")
	}
	if b, _ := os.ReadFile(gitcfg.WorkspaceFile(dir)); strings.Contains(string(b), "dev@acme.test") {
		t.Fatal("a declined edit changed the git config")
	}
}

// An answer that changes nothing asks no yes and writes nothing.
func TestReach_ExistingClientNoChange(t *testing.T) {
	dir := existingClient(t)
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	menu(t, reachPartProviders, func(*reach.Answers) {})
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { t.Fatal("a yes was asked for no change"); return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	out, err := runReach(t, dir, "--no-banner")
	if err != nil || !strings.Contains(out, "Nothing changed.") {
		t.Fatalf("%v\n%s", err, out)
	}
	if after, _ := os.ReadFile(paths.ManifestPath("acme")); !bytes.Equal(before, after) {
		t.Fatal("the manifest changed")
	}
}

// An untrusted client is shown, never edited: the menu does not appear.
func TestReach_UntrustedClientHasNoMenu(t *testing.T) {
	dir := existingClient(t)
	if err := trust.Deny("acme", manifestHash(t, dir)); err != nil {
		t.Fatal(err)
	}
	reachMenu = func() (string, error) { t.Fatal("the menu appeared for an untrusted client"); return "", nil }
	out, err := runReach(t, dir, "--no-banner")
	if err != nil || !strings.Contains(out, "isn't trusted right now") {
		t.Fatalf("%v\n%s", err, out)
	}
}

// Under an agent marker an existing client is refused before the menu, as a new one is.
func TestReach_ExistingClientRefusesAgent(t *testing.T) {
	dir := existingClient(t)
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	reachMenu = func() (string, error) { t.Fatal("the menu appeared for an agent"); return "", nil }
	for _, k := range agentMarkers {
		clearAgentMarkers(t)
		t.Setenv(k, "1")
		if out, err := runReach(t, dir, "--no-banner"); !errors.Is(err, errHumanOnly) {
			t.Fatalf("reach under %s: %v %q", k, err, out)
		}
	}
	if after, _ := os.ReadFile(paths.ManifestPath("acme")); !bytes.Equal(before, after) {
		t.Fatal("the manifest changed")
	}
}
