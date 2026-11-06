package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// isolateReach points the state at a temp dir, clears the agent markers, stubs git and the form,
// and returns the clients folder.
func isolateReach(t *testing.T, a *reach.Answers) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TQ_NO_BANNER", "")
	clearAgentMarkers(t)
	prevForm, prevGit := reachForm, reachRunGit
	reachForm = func() (*reach.Answers, error) { return a, nil }
	reachRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { reachForm, reachRunGit = prevForm, prevGit })
	clients := filepath.Join(home, "clients")
	if err := os.MkdirAll(clients, 0o755); err != nil {
		t.Fatal(err)
	}
	return clients
}

func runReach(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
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

func TestReach_PathMakesATrustedClient(t *testing.T) {
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
	m, err := manifest.Load(paths.ManifestPath("acme"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"az", "claude", "gh", "gmail", "jira", "slack", "snowflake"} {
		if !slices.Contains(m.Scope(), id) {
			t.Errorf("%s is not in scope: %v", id, m.Scope())
		}
	}
	if len(m.Browsers) != 1 || m.Browsers[0].Profile != "Profile 3" || !slices.Equal(m.OtherProviders, []string{"In-house"}) {
		t.Errorf("manifest: %+v %v", m.Browsers, m.OtherProviders)
	}
	h, _ := trust.HashFile(paths.ManifestPath("acme"))
	if v := trust.Check("acme", h); !v.OK {
		t.Fatalf("not trusted after reach: %s", v.Reason)
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
	m, err := manifest.Load(paths.ManifestPath("beta"))
	if err != nil || m.Client != "beta" {
		t.Fatalf("%+v %v", m, err)
	}
	if !slices.Equal(m.Scope(), []string{"claude"}) {
		t.Fatalf("every question skipped: scope %v", m.Scope())
	}
}

// A client's manifest is what the guard reads: reach makes the client the guard already knows.
func TestReach_GuardsTheNewClient(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test", Clouds: []string{"azure"}})
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", paths.IdentityDir("acme", "claude"))
	if code, cat, rule, errOut := hookDecision(t, dir, "Bash", cmdIn("aws ec2 run-instances")); code != 2 || cat != "identity" {
		t.Fatalf("aws on an Azure client: code=%d %s/%s %s", code, cat, rule, errOut)
	}
	if code, _, _, errOut := hookDecision(t, dir, "Bash", cmdIn("az group list")); code != 0 {
		t.Fatalf("az on an Azure client is denied: %s", errOut)
	}
}

func TestReach_ExistingClientShowsAndChangesNothing(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	dir := filepath.Join(clients, "acme")
	if out, err := runReach(t, dir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	before, _ := os.ReadFile(paths.ManifestPath("acme"))
	reachForm = func() (*reach.Answers, error) { t.Fatal("no form for an existing client"); return nil, nil }
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { t.Fatal("no confirmation: nothing is written"); return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	out, err := runReach(t, dir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"identity: acme", "This folder is already a client.", "It can use: ", "snowflake", "In-house"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "isn't trusted") {
		t.Errorf("a trusted client reads untrusted:\n%s", out)
	}
	after, _ := os.ReadFile(paths.ManifestPath("acme"))
	if !bytes.Equal(before, after) {
		t.Fatal("the manifest changed")
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

// Under every marker reach refuses before it writes; a human gets a trusted client.
func TestReachDoesNotTrustFromAgent(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	for _, k := range agentMarkers {
		clearAgentMarkers(t)
		t.Setenv(k, "1")
		name := "shadow-" + strings.ToLower(strings.ReplaceAll(k, "_", "-"))
		out, err := runReach(t, filepath.Join(clients, name))
		if !errors.Is(err, errHumanOnly) {
			t.Fatalf("reach under %s: want errHumanOnly, got %v %q", k, err, out)
		}
		if _, err := os.Stat(filepath.Join(clients, name)); !os.IsNotExist(err) {
			t.Fatalf("reach under %s wrote the folder", k)
		}
		if _, err := os.Stat(filepath.Join(paths.IdentitiesRoot(), name)); !os.IsNotExist(err) {
			t.Fatalf("reach under %s wrote an identity dir", k)
		}
	}
	clearAgentMarkers(t)
	if out, err := runReach(t, filepath.Join(clients, "human")); err != nil {
		t.Fatalf("a human reach: %v %q", err, out)
	}
	if !trust.IsTrusted("human", manifestHash(t, filepath.Join(clients, "human"))) {
		t.Fatal("a human reach trusts the new folder")
	}
}

func TestReachRefusesAgentBeforeAnything(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	reachForm = func() (*reach.Answers, error) { t.Fatal("the form must not start"); return nil, nil }
	t.Setenv("CLAUDECODE", "1")
	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir)
	if !errors.Is(err, errHumanOnly) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out, "identity:") {
		t.Fatalf("output before the refusal:\n%s", out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a refused reach wrote the folder")
	}
}

func TestReachConfirmsBeforeWriting(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir)
	if !errors.Is(err, humanonly.ErrAborted) {
		t.Fatalf("err = %v\n%s", err, out)
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

// Two folders with one name would share one identity's manifest, credentials and trust.
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

// reach makes the state folder private before it writes there.
func TestReach_EnsuresProtectedHome(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	calls := stubProtectedHome(t)
	if out, err := runReach(t, filepath.Join(clients, "acme")); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if *calls != 1 {
		t.Fatalf("calls = %d", *calls)
	}
}

func TestRootIsTentaqles(t *testing.T) {
	if got := NewRoot().Use; got != "tentaqles" {
		t.Fatalf("root command is %q", got)
	}
}
