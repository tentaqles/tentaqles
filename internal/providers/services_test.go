package providers

import (
	"reflect"
	"testing"
)

// The catalog facts doctor and reach rely on.

func TestCatalog_ChatAndEmailAreCategories(t *testing.T) {
	for _, c := range []string{"chat", "email"} {
		found := false
		for _, k := range Categories {
			if k == c {
				found = true
			}
		}
		if !found {
			t.Errorf("category %q missing from %v", c, Categories)
		}
	}
}

func TestCatalog_JiraUsesAcli(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := c.Get("jira")
	if p.CLI == nil || p.CLI.Command != "acli" {
		t.Fatalf("jira cli = %+v, want acli", p.CLI)
	}
	if p.Login == nil || !reflect.DeepEqual(p.Login.Args, []string{"jira", "auth", "login"}) {
		t.Errorf("jira login = %+v", p.Login)
	}
	if p.HasIdentity() {
		t.Error("acli has no verified config-home variable; it must not claim isolation")
	}
	if p.Notes == "" {
		t.Error("jira needs a note: acli keeps one global login")
	}
}

// az and azure-devops are two catalog entries for one CLI: the variable belongs to az itself, so
// both may declare it. Two different CLIs on one variable stay refused.
func TestCatalog_SameCLISameVariableIsAllowed(t *testing.T) {
	c := &Catalog{byID: map[string]Provider{
		"az":           {ID: "az", CLI: &CLI{Command: "az"}, Identity: Identity{Env: map[string]string{"AZURE_CONFIG_DIR": "{dir}"}}},
		"azure-devops": {ID: "azure-devops", CLI: &CLI{Command: "az"}, Identity: Identity{Env: map[string]string{"AZURE_CONFIG_DIR": "{dir}"}}},
	}}
	if err := c.checkEnvCollisions(); err != nil {
		t.Fatalf("one CLI under two ids must load: %v", err)
	}
	c.byID["other"] = Provider{ID: "other", CLI: &CLI{Command: "other"}, Identity: Identity{Env: map[string]string{"AZURE_CONFIG_DIR": "{dir}"}}}
	if err := c.checkEnvCollisions(); err == nil {
		t.Fatal("a different CLI on the same variable must be refused")
	}
	delete(c.byID, "other")
	c.byID["azure-devops"] = Provider{ID: "azure-devops", CLI: &CLI{Command: "az"}, Identity: Identity{Env: map[string]string{"AZURE_CONFIG_DIR": "{dir}/x"}}}
	if err := c.checkEnvCollisions(); err == nil {
		t.Fatal("the same CLI with a different template must be refused")
	}
}
