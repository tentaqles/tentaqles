package policy

import "testing"

// The owner's hybrid MCP list (ADR-0007, 2026-11-05): a server is kept when the catalog
// attributes it to an in-scope provider, when it is a general-purpose provider-free server,
// or when its exact name is in mcp.allow; anything else is removed at render and denied by
// the hook. An out-of-scope attribution always removes.
func TestJudgeMCP_Hybrid(t *testing.T) {
	f := newScopeFixture(t)
	s := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: mustCat(t).All(),
		Set: f.set, UserHome: f.home, MCPAllow: []string{"team-tool", "aws-thing"}})
	a := func(xs ...string) []any {
		out := make([]any, len(xs))
		for i, x := range xs {
			out[i] = x
		}
		return out
	}
	cases := []struct {
		name  string
		entry map[string]any
		keep  bool
	}{
		// (a) in-scope attribution
		{"azure", map[string]any{"command": "npx", "args": a("-y", "@azure/mcp@latest", "server", "start")}, true},
		{"ado", map[string]any{"command": "npx", "args": a("-y", "@azure-devops/mcp", "myorg")}, true},
		{"azmon", map[string]any{"command": "node", "args": a("C:/a/srv.js"), "env": map[string]any{"AZURE_TENANT_ID": "t"}}, true},
		{"azfunc", map[string]any{"type": "http", "url": "https://myfunc.azurewebsites.net/runtime/webhooks/mcp"}, true},
		{"azmcp-docker", map[string]any{"command": "docker", "args": a("run", "-i", "--rm", "mcr.microsoft.com/azure-sdk/azure-mcp")}, true},
		// (b) general-purpose
		{"playwright", map[string]any{"command": "npx", "args": a("@playwright/mcp@latest")}, true},
		{"c7", map[string]any{"type": "http", "url": "https://mcp.context7.com/mcp"}, true},
		{"mem", map[string]any{"command": "npx", "args": a("-y", "@modelcontextprotocol/server-memory")}, true},
		{"fs", map[string]any{"command": "npx", "args": a("-y", "@modelcontextprotocol/server-filesystem", "C:/work")}, true},
		{"fetch", map[string]any{"command": "uvx", "args": a("mcp-server-fetch")}, true},
		{"gitlocal", map[string]any{"command": "uvx", "args": a("mcp-server-git", "--repository", ".")}, true},
		{"pg", map[string]any{"command": "docker", "args": a("run", "-i", "--rm", "-e", "DATABASE_URL", "mcp/postgres", "postgresql://localhost/db")}, true},
		{"pg2", map[string]any{"command": "uvx", "args": a("postgres-mcp")}, true},
		{"learn", map[string]any{"type": "http", "url": "https://learn.microsoft.com/api/mcp"}, true},
		{"devtools", map[string]any{"command": "npx", "args": a("-y", "chrome-devtools-mcp@latest")}, true},
		// (c) mcp.allow by exact name
		{"team-tool", map[string]any{"command": "node", "args": a("C:/work/tools/team.js")}, true},
		// removed: unknown vendor or local tool under a neutral name, a general name over an
		// unknown command, a prefix look-alike of a general package
		{"helper", map[string]any{"command": "npx", "args": a("-y", "@acmecloud/mcp-server")}, false},
		{"tools", map[string]any{"type": "http", "url": "https://mcp.acmecloud.io/mcp"}, false},
		{"memory", map[string]any{"command": "node", "args": a("C:/x/my-memory.js")}, false},
		{"gitx", map[string]any{"command": "uvx", "args": a("mcp-server-github-enterprise")}, false},
		{"Team-Tool", map[string]any{"command": "node", "args": a("C:/work/tools/team.js")}, false}, // exact name only
		// an out-of-scope attribution wins over mcp.allow
		{"aws-thing", map[string]any{"command": "uvx", "args": a("awslabs.aws-api-mcp-server")}, false},
	}
	for _, c := range cases {
		why, deny := s.JudgeMCP(c.name, c.entry)
		if deny == c.keep {
			t.Errorf("%s: deny=%v (%s), want keep=%v", c.name, deny, why, c.keep)
		}
		if deny && why == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
}

// The hook: a same-named server is denied unless every entry is kept; a claude.ai connector
// is judged by its name (no entry exists); a server found in no source keeps the name rule.
func TestScope_MCPHybridInTheHook(t *testing.T) {
	f := newScopeFixture(t)
	entries := map[string][]map[string]any{
		"memory": {{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-memory"}}},
		"helper": {{"command": "npx", "args": []any{"-y", "@acmecloud/mcp-server"}}},
		"mixed":  {{"command": "uvx", "args": []any{"mcp-server-time"}}, {"command": "node", "args": []any{"x.js"}}},
		"mine":   {{"command": "node", "args": []any{"x.js"}}},
	}
	s := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: mustCat(t).All(),
		Set: f.set, UserHome: f.home, MCPAllow: []string{"mine"}, MCPEntries: func() map[string][]map[string]any { return entries }})
	for _, c := range []struct {
		tool string
		deny bool
	}{
		{"mcp__memory__read_graph", false},
		{"mcp__helper__x", true},
		{"mcp__mixed__x", true},
		{"mcp__mine__x", false},
		{"mcp__nowhere__x", false},                  // in no source: the name rule only
		{"mcp__claude_ai_Acme_Cloud__list", true},   // a connector of no provider of the scope
		{"mcp__claude_ai_Context7__resolve", false}, // a general-purpose connector
		{"mcp__claude_ai_Microsoft_Learn__search", false},
		{"mcp__claude_ai_Azure__list", false},          // in scope by name
		{"mcp__claude_ai_Vercel__list_projects", true}, // out of scope by name
	} {
		d := s.Evaluate(f.cwd, ToolCall{Tool: c.tool, Content: "{}"})
		if (d.Action == Deny) != c.deny {
			t.Errorf("%s: deny=%v (%s)", c.tool, d.Action == Deny, d.Reason())
		}
	}
}

// The critic fixtures' kept sets under the hybrid list: every known server stays; the two
// local scripts of no provider (k13, k17) now go unless mcp.allow names them.
func TestJudgeMCP_FixtureKeptSets(t *testing.T) {
	f := newScopeFixture(t)
	a := func(xs ...string) []any {
		out := make([]any, len(xs))
		for i, x := range xs {
			out[i] = x
		}
		return out
	}
	keep := map[string]map[string]any{
		"playwright": {"command": "npx", "args": a("@playwright/mcp@latest")},
		"context7":   {"type": "http", "url": "https://mcp.context7.com/mcp"},
		"memory":     {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-memory")},
		"azure":      {"command": "npx", "args": a("-y", "@azure/mcp@latest", "server", "start")},
		"msdocs":     {"type": "http", "url": "https://learn.microsoft.com/api/mcp"},
		"ado":        {"command": "npx", "args": a("-y", "@azure-devops/mcp", "myorg")},
		"azmon":      {"command": "node", "args": a("C:/a/srv.js"), "env": map[string]any{"AZURE_SUBSCRIPTION_ID": "s", "AZURE_TENANT_ID": "t"}},
		"postgres":   {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-postgres", "postgresql://localhost/db")},
		"fetch":      {"command": "uvx", "args": a("mcp-server-fetch")},
		"gitlocal":   {"command": "uvx", "args": a("mcp-server-git", "--repository", ".")},
		"p02-ok":     {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-filesystem", ".")},
		"k05-c7":     {"command": "npx", "args": a("-y", "@upstash/context7-mcp")},
		"k06-seq":    {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-sequential-thinking")},
		"k08-pgdock": {"command": "docker", "args": a("run", "-i", "--rm", "-e", "DATABASE_URL", "mcp/postgres", "postgresql://localhost/db")},
		"k09-time":   {"command": "uvx", "args": a("mcp-server-time")},
		"k12-azfunc": {"type": "http", "url": "https://myfunc.azurewebsites.net/runtime/webhooks/mcp", "headers": map[string]any{"x-functions-key": "${AZURE_FUNCTIONS_KEY}"}},
		"k14-sqlite": {"command": "uvx", "args": a("mcp-server-sqlite", "--db-path", "C:/work/db.sqlite")},
		"k15-every":  {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-everything")},
		"k16-azmcp":  {"command": "docker", "args": a("run", "-i", "--rm", "-e", "AZURE_TENANT_ID", "mcr.microsoft.com/azure-sdk/azure-mcp")},
	}
	gone := map[string]map[string]any{
		"k13-localpy":  {"command": "python", "args": a("C:/work/tools/mcp_server.py"), "env": map[string]any{"DB_PATH": "C:/work/db.sqlite"}},
		"k17-node-env": {"command": "node", "args": a("C:/work/srv.js"), "env": map[string]any{"NODE_ENV": "development"}},
	}
	for n, e := range keep {
		if why, deny := f.s.JudgeMCP(n, e); deny {
			t.Errorf("%s denied: %s", n, why)
		}
	}
	for n, e := range gone {
		if _, deny := f.s.JudgeMCP(n, e); !deny {
			t.Errorf("%s kept", n)
		}
	}
	allowed := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: mustCat(t).All(),
		Set: f.set, UserHome: f.home, MCPAllow: []string{"k13-localpy", "k17-node-env"}})
	for n, e := range gone {
		if why, deny := allowed.JudgeMCP(n, e); deny {
			t.Errorf("%s in mcp.allow denied: %s", n, why)
		}
	}
}
