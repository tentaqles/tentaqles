package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
)

// runChange runs tentaqles add|remove args with stdin as the typed answer.
func runChange(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// A change in the client's folder: the manifest, the new identity's folder, the confirmation.
func TestAdd_CloudInTheClientsFolder(t *testing.T) {
	dir := existingClient(t)
	chdir(t, filepath.Join(dir))
	out, err := runChange(t, "yes\n", "add", "cloud", "aws")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t, "acme")
	if !slices.Contains(m.Scope(), "aws") {
		t.Fatalf("aws not in scope: %v", m.Scope())
	}
	if fi, err := os.Stat(paths.IdentityDir("acme", "aws")); err != nil || !fi.IsDir() {
		t.Fatalf("no folder for aws: %v", err)
	}
	for _, want := range []string{"tentaqles add: about to change the client acme", "clouds: azure -> azure, aws", "tentaqles login acme aws"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// --client takes a name or a folder; set is add; --yes skips the typed yes.
func TestAdd_ClientFlagAndYes(t *testing.T) {
	dir := existingClient(t)
	chdir(t, t.TempDir())
	if out, err := runChange(t, "", "set", "email", "dev@acme.test", "--client", "acme", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); m.Git.Email != "dev@acme.test" {
		t.Fatalf("git.email = %q", m.Git.Email)
	}
	if b, _ := os.ReadFile(gitcfg.WorkspaceFile(dir)); !strings.Contains(string(b), "email = dev@acme.test") {
		t.Fatalf("the folder's git config: %s", b)
	}
	if out, err := runChange(t, "", "remove", "task", "--client", dir, "-y"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); slices.Contains(m.Scope(), "jira") {
		t.Fatalf("jira still in scope: %v", m.Scope())
	}
}

// A client on Azure DevOps with gh listed by hand: tentaqles remove gh takes it out of the scope,
// keeping the rest of the file.
func TestRemove_HandListedIdentity(t *testing.T) {
	base := newBase(t)
	root := writeClient(t, base, "yduqs", "git: { provider: azure-devops, email: me@yduqs.example }\nidentities: { claude: {}, gh: {} }\ncloud: { provider: azure }\nstack: [spark] # kept\n")
	chdir(t, root)
	out, err := runChange(t, "yes\n", "remove", "gh")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t, "yduqs")
	if slices.Contains(m.Scope(), "gh") || !slices.Contains(m.Scope(), "azure-devops") || !slices.Contains(m.Scope(), "az") {
		t.Fatalf("scope: %v", m.Scope())
	}
	if !strings.Contains(string(mustRead(t, paths.ManifestPath("yduqs"))), "# kept") {
		t.Fatal("a hand-written comment was lost")
	}
	if !strings.Contains(out, "gh is no longer in this client's scope") {
		t.Errorf("the kept login folder is not named:\n%s", out)
	}
}

// Without the typed yes nothing is written.
func TestChange_DeclinedWritesNothing(t *testing.T) {
	dir := existingClient(t)
	chdir(t, dir)
	before := mustRead(t, paths.ManifestPath("acme"))
	if out, err := runChange(t, "no\n", "add", "cloud", "gcp"); !errors.Is(err, errAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, mustRead(t, paths.ManifestPath("acme"))) {
		t.Fatal("a declined change wrote the manifest")
	}
}

// Refused: inside Claude Code, in a neutral folder, with a bad value; the form needs a terminal.
func TestChange_Refusals(t *testing.T) {
	dir := existingClient(t)
	before := mustRead(t, paths.ManifestPath("acme"))
	chdir(t, dir)
	if _, err := runChange(t, "yes\n", "add", "git", "svn"); err == nil || !strings.Contains(err.Error(), "github") {
		t.Fatalf("a bad value: %v", err)
	}
	if _, err := runChange(t, "yes\n", "remove", "team", "discord"); err == nil {
		t.Fatal("removing what the client lacks was accepted")
	}
	if _, err := runChange(t, "yes\n", "add", "task"); !errors.Is(err, errNoTerminal) {
		t.Fatalf("the form without a terminal: %v", err)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, err := runChange(t, "yes\n", "add", "cloud", "aws"); err == nil || !strings.Contains(err.Error(), "your own terminal") {
		t.Fatalf("inside Claude Code: %v", err)
	}
	unsetenv(t, "CLAUDECODE")
	chdir(t, t.TempDir())
	if _, err := runChange(t, "yes\n", "add", "cloud", "aws"); err == nil || !strings.Contains(err.Error(), "belongs to no client") {
		t.Fatalf("a neutral folder: %v", err)
	}
	if !bytes.Equal(before, mustRead(t, paths.ManifestPath("acme"))) {
		t.Fatal("a refused change wrote the manifest")
	}
}

// With no value the category's form opens with the current answers, and its answers apply.
func TestAdd_NoValueOpensTheCategoryForm(t *testing.T) {
	dir := existingClient(t)
	chdir(t, dir)
	prev := reachCategoryForm
	t.Cleanup(func() { reachCategoryForm = prev })
	reachCategoryForm = func(cat string, cur *reach.Answers) (*reach.Answers, error) {
		if cat != reach.CatTask || cur.Task != "jira" {
			t.Fatalf("form %q from %+v", cat, cur)
		}
		n := *cur
		n.Task = "linear"
		return &n, nil
	}
	if out, err := runChange(t, "yes\n", "add", "task"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); !slices.Contains(m.Scope(), "linear") || slices.Contains(m.Scope(), "jira") {
		t.Fatalf("scope: %v", m.Scope())
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A new client's language is English (United States) unless the form says otherwise;
// tentaqles add language changes it, and the client's Claude settings follow.
func TestAdd_Language(t *testing.T) {
	dir := existingClient(t)
	if m := readManifest(t, "acme"); m.Language != "en-US" {
		t.Fatalf("a new client's language = %q", m.Language)
	}
	settings := filepath.Join(paths.IdentityDir("acme", "claude"), "settings.json")
	if !strings.Contains(string(mustRead(t, settings)), `"language": "English (United States)"`) {
		t.Fatalf("settings.json:\n%s", mustRead(t, settings))
	}
	chdir(t, dir)
	out, err := runChange(t, "yes\n", "add", "language", "pt-BR")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); m.Language != "pt-BR" {
		t.Fatalf("language = %q", m.Language)
	}
	if !strings.Contains(string(mustRead(t, settings)), `"language": "Portuguese (Brazil)"`) {
		t.Fatalf("settings.json:\n%s", mustRead(t, settings))
	}
	if !strings.Contains(out, "language: English (United States) -> Portuguese (Brazil)") {
		t.Errorf("the change is not listed:\n%s", out)
	}
	if _, err := runChange(t, "yes\n", "remove", "language"); err == nil {
		t.Fatal("removing the language was accepted")
	}
}
