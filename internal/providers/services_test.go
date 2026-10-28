package providers

import (
	"reflect"
	"strings"
	"testing"
)

// Spec B §8: the catalog fixes onboarding and doctor rely on.

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

func TestCatalog_MCPOnlyProviders(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"slack": "chat", "teams": "chat", "gmail": "email", "outlook": "email"}
	for id, cat := range want {
		p, ok := c.Get(id)
		if !ok {
			t.Errorf("provider %q missing", id)
			continue
		}
		if p.Category != cat {
			t.Errorf("%s: category %q, want %q", id, p.Category, cat)
		}
		if p.CLI != nil || p.HasIdentity() || p.Login != nil {
			t.Errorf("%s: an MCP-only provider has no CLI, identity or login", id)
		}
		if p.MCP == "" {
			t.Errorf("%s: needs an mcp: bundle catalog reference", id)
		}
		if p.MCPName() != p.MCP {
			t.Errorf("%s: MCPName %q != MCP %q", id, p.MCPName(), p.MCP)
		}
	}
	if len(c.ByCategory("chat")) < 2 || len(c.ByCategory("email")) < 2 {
		t.Error("chat and email categories need their providers")
	}
}

func TestProvider_MCPNameDefaultsToID(t *testing.T) {
	if got := (Provider{ID: "linear"}).MCPName(); got != "linear" {
		t.Fatalf("MCPName = %q", got)
	}
}

func TestValidate_RejectsBadMCPName(t *testing.T) {
	for _, bad := range []string{"has space", "semi;colon", "UPPER/slash"} {
		p := Provider{ID: "good", Name: "x", Category: "chat", MCP: bad}
		if err := p.Validate(); err == nil {
			t.Errorf("mcp %q should be refused", bad)
		}
	}
	if err := (Provider{ID: "good", Name: "x", Category: "chat", MCP: "claude_ai-Slack.v2"}).Validate(); err != nil {
		t.Errorf("a plain mcp name must load: %v", err)
	}
}

func TestCatalog_AzureDevOpsLoginVerifyIdentity(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := c.Get("azure-devops")
	if !ok {
		t.Fatal("azure-devops missing")
	}
	if p.CLI == nil || p.CLI.Command != "az" {
		t.Fatalf("cli = %+v, want az", p.CLI)
	}
	if p.Login == nil || !reflect.DeepEqual(p.Login.Args, []string{"login"}) {
		t.Errorf("login = %+v, want az login", p.Login)
	}
	if p.Verify == nil || !reflect.DeepEqual(p.Verify.Args, []string{"account", "show"}) {
		t.Errorf("verify = %+v, want az account show", p.Verify)
	}
	if p.Verify != nil && (p.Verify.Account == nil || p.Verify.Account.JSON != "user.name") {
		t.Errorf("verify account field = %+v, want json user.name", p.Verify.Account)
	}
	if got := p.Identity.Env; !reflect.DeepEqual(got, map[string]string{"AZURE_CONFIG_DIR": "{dir}"}) {
		t.Errorf("identity env = %v", got)
	}
	if p.Install.Windows.Winget == "" || p.Install.Macos.Brew == "" || p.Install.Linux.Apt == "" {
		t.Errorf("install hints missing: %+v", p.Install)
	}
	if !strings.Contains(p.Install.Windows.Note, "az extension add --name azure-devops") {
		t.Errorf("windows note should name the devops extension: %q", p.Install.Windows.Note)
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

func TestCatalog_LinearAsanaEnvNotes(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"linear", "asana"} {
		p, _ := c.Get(id)
		if !strings.Contains(p.Notes, "tq secret set") || !strings.Contains(p.Notes, "tracker.env") {
			t.Errorf("%s notes should name tq secret set and tracker.env: %q", id, p.Notes)
		}
		if p.MCPName() == "" {
			t.Errorf("%s: mcp name", id)
		}
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
