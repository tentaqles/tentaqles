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
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// isolateReach points the state at a temp dir, stubs git, the form, the menu and the plugin
// install, and returns the clients folder.
func isolateReach(t *testing.T, a *reach.Answers) string {
	t.Helper()
	home := isolateHome(t)
	t.Setenv("TQ_NO_BANNER", "")
	prevForm, prevGit, prevPlugin, prevMenu, prevEdit := reachForm, reachRunGit, reachGuardPlugin, reachMenu, reachEditForm
	reachForm = func() (*reach.Answers, error) { return a, nil }
	reachRunGit = func(...string) (string, error) { return "", nil }
	// No test but the plugin ones runs claude.
	reachGuardPlugin = func(*cobra.Command, *resolve.Workspace) {}
	reachMenu = func() (string, error) { return reachPartNothing, nil }
	reachEditForm = func(string, *reach.Answers) (*reach.Answers, error) {
		t.Fatal("an edit form ran without a test asking for one")
		return nil, nil
	}
	t.Cleanup(func() {
		reachForm, reachRunGit, reachGuardPlugin, reachMenu, reachEditForm = prevForm, prevGit, prevPlugin, prevMenu, prevEdit
	})
	clients := filepath.Join(home, "clients")
	if err := os.MkdirAll(clients, 0o755); err != nil {
		t.Fatal(err)
	}
	return clients
}

// runReach runs tentaqles reach with the answer "yes" to its confirmation.
func runReach(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runReachIn(t, "yes\n", args...)
}

func runReachIn(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"reach"}, args...))
	err := root.Execute()
	return out.String(), err
}

func fullAnswers() *reach.Answers {
	return &reach.Answers{
		Email: "me@acme.test", Git: "github", Clouds: []string{"azure"}, Team: []string{"slack"}, Data: []string{"snowflake"},
		Task: "jira", EmailProvider: "gmail", Browsers: []manifest.Browser{{Name: "edge", Profile: "Profile 3"}}, Other: []string{"In-house"},
	}
}

func readManifest(t *testing.T, name string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(paths.ManifestPath(name))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReach_PathMakesAClient(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "identity: acme") || !strings.Contains(out, dir) {
		t.Fatalf("the banner names the client and folder:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Fatal("no marker in the folder")
	}
	m := readManifest(t, "acme")
	for _, id := range []string{"az", "claude", "gh", "gmail", "jira", "slack", "snowflake"} {
		if !slices.Contains(m.Scope(), id) {
			t.Errorf("%s is not in scope: %v", id, m.Scope())
		}
	}
	if len(m.Browsers) != 1 || m.Browsers[0].Profile != "Profile 3" || !slices.Equal(m.OtherProviders, []string{"In-house"}) {
		t.Errorf("manifest: %+v %v", m.Browsers, m.OtherProviders)
	}
	for _, id := range []string{"claude", "gh", "az"} {
		if fi, err := os.Stat(paths.IdentityDir("acme", id)); err != nil || !fi.IsDir() {
			t.Errorf("no identity folder for %s: %v", id, err)
		}
	}
	if b, err := os.ReadFile(gitcfg.WorkspaceFile(dir)); err != nil || !strings.Contains(string(b), "email = me@acme.test") {
		t.Errorf("the folder's git identity: %v %s", err, b)
	}
	if !strings.Contains(out, "Not managed by tentaqles: In-house") {
		t.Fatalf("an Other provider's limit isn't stated:\n%s", out)
	}
}

func TestReach_CurrentFolder(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	dir := filepath.Join(clients, "beta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	if out, err := runReach(t); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "beta"); !slices.Equal(m.Scope(), []string{"claude"}) {
		t.Fatalf("every question skipped: scope %v", m.Scope())
	}
}

// The manifest reach writes is what the hook reads: an Azure client's terminal session may not run
// the AWS CLI, and may run the Azure one.
func TestReach_GuardsTheNewClient(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test", Clouds: []string{"azure"}})
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "aws ec2 run-instances")); code != 2 {
		t.Fatalf("aws on an Azure client was allowed: %s", errOut)
	}
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "az group list")); code != 0 {
		t.Fatalf("az on an Azure client is denied: %s", errOut)
	}
}

func TestReach_NoBanner(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	out, err := runReach(t, filepath.Join(clients, "one"), "--no-banner")
	if err != nil || strings.Contains(out, "identity: one") {
		t.Fatalf("--no-banner: %v\n%s", err, out)
	}
	t.Setenv("TQ_NO_BANNER", "1")
	out, err = runReach(t, filepath.Join(clients, "two"))
	if err != nil || strings.Contains(out, "identity: two") {
		t.Fatalf("TQ_NO_BANNER=1: %v\n%s", err, out)
	}
}

// Nothing is written before a typed yes.
func TestReach_ConfirmsBeforeWriting(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	dir := filepath.Join(clients, "acme")
	for _, answer := range []string{"", "no\n", "y\n"} {
		out, err := runReachIn(t, answer, dir)
		if !errors.Is(err, errAborted) {
			t.Fatalf("answer %q: err = %v\n%s", answer, err, out)
		}
		if !strings.Contains(out, "about to make "+dir) {
			t.Fatalf("the confirmation names the folder:\n%s", out)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("a declined reach wrote the folder")
		}
		if _, err := os.Stat(paths.ManifestPath("acme")); !os.IsNotExist(err) {
			t.Fatal("a declined reach wrote the manifest")
		}
	}
}

// reach is the developer's command: inside a Claude Code session it refuses before it asks
// anything.
func TestReach_RefusedInsideClaudeCode(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	reachForm = func() (*reach.Answers, error) { t.Fatal("the form must not start"); return nil, nil }
	t.Setenv("CLAUDECODE", "1")
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir); err == nil || !strings.Contains(err.Error(), "your own terminal") || strings.Contains(out, "identity:") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a refused reach wrote the folder")
	}
}

func TestReach_BadFolders(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	called := false
	reachForm = func() (*reach.Answers, error) { called = true; return fullAnswers(), nil }
	if _, err := runReach(t, filepath.Join(clients, "Has Spaces")); err == nil || !strings.Contains(err.Error(), "can't name a client") {
		t.Fatalf("bad name: %v", err)
	}
	file := filepath.Join(clients, "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runReach(t, file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("a file: %v", err)
	}
	if called {
		t.Fatal("a bad folder must fail before the form")
	}
}

// Two folders with one name would share one identity's manifest and logins.
func TestReach_SameNameElsewhereRefused(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	if out, err := runReach(t, filepath.Join(clients, "acme")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	other := filepath.Join(filepath.Dir(clients), "more", "acme")
	if _, err := runReach(t, other); err == nil || !strings.Contains(err.Error(), "already exists elsewhere") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("the refused folder was created")
	}
}

func TestRootIsTentaqles(t *testing.T) {
	if got := NewRoot().Use; got != "tentaqles" {
		t.Fatalf("root command is %q", got)
	}
}

// --- an existing client ---------------------------------------------------

// existingClient makes acme with fullAnswers through reach, and returns its folder.
func existingClient(t *testing.T) string {
	t.Helper()
	clients := isolateReach(t, fullAnswers())
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir); err != nil {
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

// Shown, and nothing changes, when the menu answers nothing.
func TestReach_ExistingClientShowsAndChangesNothing(t *testing.T) {
	dir := existingClient(t)
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	reachForm = func() (*reach.Answers, error) { t.Fatal("no new-client form for an existing client"); return nil, nil }
	out, err := runReachIn(t, "", dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"identity: acme", "This folder is already a client.", "It can use: ", "snowflake", "In-house"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if after, _ := os.ReadFile(paths.ManifestPath("acme")); !bytes.Equal(before, after) {
		t.Fatal("the manifest changed")
	}
}

func TestReach_ExistingClientChangesProviders(t *testing.T) {
	dir := existingClient(t)
	menu(t, reachPartProviders, func(a *reach.Answers) {
		a.Clouds = []string{"azure", "aws"}
		a.Team = nil
		a.Task = "linear"
	})
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t, "acme")
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
	if fi, err := os.Stat(paths.IdentityDir("acme", "aws")); err != nil || !fi.IsDir() {
		t.Errorf("no folder for the new aws identity: %v", err)
	}
	for _, want := range []string{"about to change the client acme", "clouds: azure -> azure, aws", "acme is updated. It can use:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// The hook reads the new scope at once.
	if code, _, errOut := runHook(t, []string{"claude-hook", "pre-tool-use"}, hookPayloadFor(t, dir, "aws s3 ls")); code != 0 {
		t.Errorf("aws, now in scope, is denied: %s", errOut)
	}
}

func TestReach_ExistingClientChangesEmail(t *testing.T) {
	dir := existingClient(t)
	menu(t, reachPartEmail, func(a *reach.Answers) { a.Email = "dev@acme.test" })
	if out, err := runReach(t, dir, "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); m.Git.Email != "dev@acme.test" {
		t.Fatalf("git.email = %q", m.Git.Email)
	}
	if b, err := os.ReadFile(gitcfg.WorkspaceFile(dir)); err != nil || !strings.Contains(string(b), "email = dev@acme.test") {
		t.Fatalf("the folder's git config: %v %s", err, b)
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
	if m := readManifest(t, "acme"); len(m.Browsers) != 1 || m.Browsers[0] != (manifest.Browser{Name: "chrome", Profile: "Work"}) {
		t.Fatalf("browsers: %+v", m.Browsers)
	}
	if !strings.Contains(out, "browsers: edge (Profile 3) -> chrome (Work)") {
		t.Errorf("the confirmation does not name the change:\n%s", out)
	}
}

// A declined yes writes nothing: the manifest and the git config stay as they were.
func TestReach_ExistingClientDeclinedChangesNothing(t *testing.T) {
	dir := existingClient(t)
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	menu(t, reachPartEmail, func(a *reach.Answers) { a.Email = "dev@acme.test" })
	if out, err := runReachIn(t, "no\n", dir, "--no-banner"); !errors.Is(err, errAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if after, _ := os.ReadFile(paths.ManifestPath("acme")); !bytes.Equal(before, after) {
		t.Fatal("a declined edit changed the manifest")
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
	out, err := runReachIn(t, "", dir, "--no-banner")
	if err != nil || !strings.Contains(out, "Nothing changed.") || strings.Contains(out, "Type yes") {
		t.Fatalf("%v\n%s", err, out)
	}
	if after, _ := os.ReadFile(paths.ManifestPath("acme")); !bytes.Equal(before, after) {
		t.Fatal("the manifest changed")
	}
}
