package cmd

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/reach"
)

// add codex, by value alone and as add agent codex: the scope, and codex's own folder for CODEX_HOME.
func TestAdd_AgentByValueAndByCategory(t *testing.T) {
	dir := existingClient(t)
	chdir(t, dir)
	out, err := runChange(t, "yes\n", "add", "codex")
	if err != nil {
		t.Fatalf("add codex: %v\n%s", err, out)
	}
	m := readManifest(t, "acme")
	if !slices.Contains(m.Scope(), "codex") {
		t.Fatalf("codex not in scope: %v", m.Scope())
	}
	if fi, err := os.Stat(paths.IdentityDir("acme", "codex")); err != nil || !fi.IsDir() {
		t.Fatalf("no folder for codex: %v", err)
	}
	for _, want := range []string{"tentaqles add: about to change the client acme", "agent and task providers: jira, slack, snowflake -> codex, jira, slack, snowflake", "tentaqles login acme codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}

	// A provider with no login of its own goes in the scope and gets no folder.
	if out, err := runChange(t, "yes\n", "add", "agent", "cursor"); err != nil {
		t.Fatalf("add agent cursor: %v\n%s", err, out)
	}
	m = readManifest(t, "acme")
	if !slices.Contains(m.Scope(), "cursor") {
		t.Fatalf("cursor not in scope: %v", m.Scope())
	}
	if _, err := os.Stat(paths.IdentityDir("acme", "cursor")); !os.IsNotExist(err) {
		t.Fatalf("cursor has a folder: %v", err)
	}

	// remove codex by value alone takes it out of the scope and the identities.
	if out, err := runChange(t, "yes\n", "remove", "codex"); err != nil {
		t.Fatalf("remove codex: %v\n%s", err, out)
	}
	m = readManifest(t, "acme")
	if _, ok := m.Identities["codex"]; ok || slices.Contains(m.Scope(), "codex") {
		t.Fatalf("codex still there: %v %v", m.Identities, m.Scope())
	}
	if !slices.Contains(m.Scope(), "cursor") {
		t.Fatalf("removing codex took cursor too: %v", m.Scope())
	}
}

// Declined: nothing is written. Claude Code is not an agent to add.
func TestAdd_AgentDeclinedWritesNothing(t *testing.T) {
	dir := existingClient(t)
	chdir(t, dir)
	before := mustRead(t, paths.ManifestPath("acme"))
	if out, err := runChange(t, "no\n", "add", "agent", "codex"); !errors.Is(err, errAborted) {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !bytes.Equal(before, mustRead(t, paths.ManifestPath("acme"))) {
		t.Fatal("a declined change wrote the manifest")
	}
	if _, err := runChange(t, "yes\n", "add", "agent", "claude"); err == nil {
		t.Fatal("claude was added as an agent")
	}
	if !bytes.Equal(before, mustRead(t, paths.ManifestPath("acme"))) {
		t.Fatal("a refused change wrote the manifest")
	}
}

// add agent with no value opens the agent question with the client's current answers.
func TestAdd_AgentNoValueOpensTheCategoryForm(t *testing.T) {
	dir := existingClient(t)
	chdir(t, dir)
	prev := reachCategoryForm
	t.Cleanup(func() { reachCategoryForm = prev })
	reachCategoryForm = func(cat string, cur *reach.Answers) (*reach.Answers, error) {
		if cat != reach.CatAgent {
			t.Fatalf("form %q", cat)
		}
		n := *cur
		n.Agent = []string{"gemini"}
		return &n, nil
	}
	if out, err := runChange(t, "yes\n", "add", "agent"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := readManifest(t, "acme"); !slices.Contains(m.Scope(), "gemini") {
		t.Fatalf("scope: %v", m.Scope())
	}
}

// The reach menu's agent entry changes only the agents: codex is added, and the typed yes is asked.
func TestReach_ExistingClientChangesAgents(t *testing.T) {
	dir := existingClient(t)
	menu(t, reachPartAgents, func(a *reach.Answers) {
		a.Agent = []string{"codex"}
	})
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := readManifest(t, "acme")
	if _, ok := m.Identities["codex"]; !ok || !slices.Contains(m.Scope(), "codex") {
		t.Fatalf("codex identity missing: %v scope %v", m.Identities, m.Scope())
	}
	if _, err := os.Stat(paths.IdentityDir("acme", "codex")); err != nil {
		t.Fatalf("no folder for codex: %v", err)
	}
}

// The form's agent question keeps the agents it starts with when its run changes nothing, and
// names the agent category's choices; a refused run is an error.
func TestAskAgents_KeepsTheChoice(t *testing.T) {
	a := &reach.Answers{Agent: []string{"codex"}}
	if err := askAgents(func(huh.Field) error { return nil }, a); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Agent, []string{"codex"}) {
		t.Fatalf("agents %v", a.Agent)
	}
	boom := errors.New("no terminal")
	if err := askAgents(func(huh.Field) error { return boom }, a); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}
