package providers

import "testing"

// mcpVendors are the catalog providers whose vendor publishes an official MCP server (a
// remote endpoint or a package), as of 2026-11. Each must carry it as data (mcp_hosts or
// mcp_packages) so the harness config layer and the hook attribute it whatever the server is
// named (ADR-0008). A vendor that starts publishing one joins this list with its entry.
var mcpVendors = []string{
	"asana", "aws", "az", "azure-devops", "dbt", "doctl", "firebase", "gcloud", "gh", "gitea",
	"glab", "heroku", "hostinger", "jira", "linear", "mongodb-atlas", "neon", "netlify", "notion",
	"pulumi", "railway", "render", "sentry", "slack", "snowflake", "stripe", "supabase",
	"terraform", "twilio", "vercel", "wrangler",
}

func TestMCPVendorsHaveMCPData(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range mcpVendors {
		p, ok := c.Get(id)
		if !ok {
			t.Errorf("%s: not in the catalog", id)
			continue
		}
		if len(p.Match.MCPHosts)+len(p.Match.MCPPackages) == 0 {
			t.Errorf("%s publishes an MCP server but its match lists no mcp_hosts or mcp_packages", id)
		}
	}
}
