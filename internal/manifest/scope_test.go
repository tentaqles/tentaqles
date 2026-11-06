package manifest

import (
	"reflect"
	"strings"
	"testing"
)

// TestScope: the provider scope is derived from what an existing manifest already declares
// (the identities block, or claude+gh for a v1 manifest, the cloud block, the git provider,
// the company services, the agents, the bundle's MCP servers) plus the optional scope: list;
// sorted, each id once.
func TestScope(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"v1 defaults", "schema: tentaqles-client-v1\nclient: x\ncloud: { provider: azure }\n", []string{"az", "claude", "gh"}},
		{"identities and cloud.also", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {}, az: {} }\ncloud: { provider: azure, also: [aws] }\n", []string{"aws", "az", "claude"}},
		{"git provider", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: azure-devops }\nidentities: { az: {}, claude: {} }\n", []string{"az", "azure-devops", "claude"}},
		{"git provider gitlab", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: gitlab }\nidentities: { claude: {} }\n", []string{"claude", "glab"}},
		{"git provider github", "schema: tentaqles-client-v2\nclient: x\ngit: { provider: github }\nidentities: { claude: {}, gh: {} }\n", []string{"claude", "gh"}},
		{"services and agents", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\nchat: { provider: slack }\nemail: { provider: none }\ndata_platform: { provider: databricks }\nagents: [codex]\n", []string{"claude", "codex", "databricks", "slack"}},
		{"bundle mcp", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\nclaude: { bundle: { mcp: [atlassian, linear, my-own-server] } }\n", []string{"claude", "jira", "linear"}},
		{"explicit scope", "schema: tentaqles-client-v2\nclient: x\nidentities: { claude: {} }\nscope: [vercel, terraform, claude]\n", []string{"claude", "terraform", "vercel"}},
	}
	for _, c := range cases {
		m, err := Load(write(t, c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := m.Scope(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Scope() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScopeRejectsUnknownIDs(t *testing.T) {
	for _, body := range []string{"scope: [fortran]\n", "scope: [aws, aws]\n"} {
		if _, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body)); err == nil || !strings.Contains(err.Error(), "scope") {
			t.Errorf("%q loaded: %v", body, err)
		}
	}
}

// TestGuardFlag: guard: v2 turns the v2 identity rules on; no flag, or v1, keeps the v1
// rules. The guard block tq 0.9.0 dropped (a mapping) still loads, is listed in Ignored and
// is not the flag; an unknown scalar fails the load.
func TestGuardFlag(t *testing.T) {
	for body, want := range map[string]bool{
		"":              false,
		"guard: v1\n":   false,
		"guard: v2\n":   true,
		"guard: V2\n":   true,
		"guard: \"\"\n": false,
	} {
		m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body))
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if m.GuardV2() != want {
			t.Errorf("%q: GuardV2() = %v, want %v", body, m.GuardV2(), want)
		}
		if len(m.Ignored) != 0 {
			t.Errorf("%q: Ignored = %v, a scalar guard flag is not a dropped block", body, m.Ignored)
		}
	}
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nguard:\n  unattended: { workspace: true }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.GuardV2() || !reflect.DeepEqual(m.Ignored, []string{"guard"}) {
		t.Fatalf("legacy guard block: GuardV2=%v Ignored=%v", m.GuardV2(), m.Ignored)
	}
	if _, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nguard: strict\n")); err == nil || !strings.Contains(err.Error(), "guard") {
		t.Fatalf("guard: strict loaded: %v", err)
	}
}

// TestMCPAllow: mcp.allow is a list of exact MCP server names a human keeps under guard: v2
// (the owner's hybrid list, ADR-0007); names only, each once.
func TestMCPAllow(t *testing.T) {
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nmcp: { allow: [my-db, plugin_tools_srv, docs.v2] }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.MCPAllow(); !reflect.DeepEqual(got, []string{"my-db", "plugin_tools_srv", "docs.v2"}) {
		t.Fatalf("MCPAllow = %v", got)
	}
	none, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"))
	if err != nil || none.MCPAllow() != nil {
		t.Fatalf("no mcp block: %v %v", none.MCPAllow(), err)
	}
	for _, body := range []string{"mcp: { allow: [\"npx -y x\"] }\n", "mcp: { allow: [\"a/b\"] }\n", "mcp: { allow: [x, x] }\n", "mcp: { allow: [\"\"] }\n", "mcp: { allow: [\"$(x)\"] }\n"} {
		if _, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"+body)); err == nil || !strings.Contains(err.Error(), "mcp.allow") {
			t.Errorf("%q loaded: %v", body, err)
		}
	}
}
