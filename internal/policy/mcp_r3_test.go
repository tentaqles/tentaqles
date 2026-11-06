package policy

import (
	"testing"
)

// The critic's MCP fixtures (round 2 of ADR-0007, genmcp.py): every vendor server below is
// attributed to its provider by its entry, whatever it is named; the kept set stays.
func TestOutOfScopeMCPEntry_VendorFixtures(t *testing.T) {
	f := newScopeFixture(t)
	a := func(xs ...string) []any {
		out := make([]any, len(xs))
		for i, x := range xs {
			out[i] = x
		}
		return out
	}
	out := map[string]map[string]any{
		"m01-cloud":      {"type": "stdio", "command": "uvx", "args": a("awslabs.aws-api-mcp-server@latest"), "env": map[string]any{}},
		"m02-infra":      {"command": "uvx", "args": a("--from", "awslabs-core-mcp-server", "core")},
		"m03-store":      {"command": "docker", "args": a("run", "-i", "--rm", "-e", "AWS_REGION", "public.ecr.aws/awslabs-mcp/awslabs/aws-api-mcp-server:latest")},
		"m04-proxy":      {"command": "uvx", "args": a("mcp-proxy-for-aws@latest", "https://aws-mcp.us-east-1.api.aws/mcp")},
		"m05-bin":        {"command": `C:\Users\dev\.local\bin\awslabs.aws-api-mcp-server.exe`, "args": a()},
		"m06-gitrepo":    {"command": "uvx", "args": a("--from", "git+https://github.com/awslabs/mcp.git#subdirectory=src/aws-api-mcp-server", "aws-api-mcp-server")},
		"m07-bq":         {"command": "npx", "args": a("-y", "@toolbox-sdk/server", "--prebuilt", "bigquery", "--stdio"), "env": map[string]any{"BIGQUERY_PROJECT": "p1"}},
		"m08-gcpremote":  {"type": "http", "url": "https://bigquery.googleapis.com/mcp"},
		"m09-gcpenv":     {"command": "node", "args": a("C:/work/tools/server.js"), "env": map[string]any{"GOOGLE_APPLICATION_CREDENTIALS": "C:/k.json"}},
		"m10-repo":       {"type": "http", "url": "https://api.githubcopilot.com/mcp/", "headers": map[string]any{"Authorization": "Bearer x"}},
		"m11-repo2":      {"command": "docker", "args": a("run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server")},
		"m12-repo3":      {"command": `C:\tools\github-mcp-server.exe`, "args": a("stdio"), "env": map[string]any{"GITHUB_PERSONAL_ACCESS_TOKEN": "x"}},
		"m13-repo4":      {"command": "docker", "args": a("run", "-i", "--rm", "mcp/github")},
		"m14-deploy":     {"type": "http", "url": "https://mcp.vercel.com"},
		"m15-deploy2":    {"command": "npx", "args": a("-y", "mcp-remote", "https://mcp.vercel.com/my-team/my-project")},
		"m16-deploy3":    {"command": "node", "args": a("C:/x/index.js"), "env": map[string]any{"VERCEL_TOKEN": "x"}},
		"m17-db":         {"command": "cmd", "args": a("/c", "npx", "-y", "@supabase/mcp-server-supabase@latest", "--read-only")},
		"m18-db2":        {"type": "http", "url": "https://mcp.supabase.com/mcp?project_ref=abc"},
		"m19-edge":       {"type": "sse", "url": "https://bindings.mcp.cloudflare.com/sse"},
		"m20-edge2":      {"command": "npx", "args": a("mcp-remote", "https://observability.mcp.cloudflare.com/mcp")},
		"m21-edge3":      {"command": "npx", "args": a("-y", "@cloudflare/mcp-server-cloudflare", "run", "acct")},
		"m22-edge4":      {"command": "node", "args": a("C:/y/index.js"), "env": map[string]any{"CLOUDFLARE_API_TOKEN": "x"}},
		"m23-awsenvonly": {"command": "python", "args": a("C:/z/srv.py"), "env": map[string]any{"AWS_PROFILE": "prod"}},
		"m24-npmcmd":     {"command": `C:\Users\dev\AppData\Roaming\npm\aws-mcp.cmd`},
		"m25-pkgflag":    {"command": "npx", "args": a("--package=@aws/aws-mcp", "-y", "aws-mcp-run")},
		"m26-gcpdocker":  {"command": "docker", "args": a("run", "-i", "--rm", "-v", "C:/k.json:/k.json", "us-docker.pkg.dev/cloud-run/mcp/gcp-mcp:latest")},
		"m27-shellwrap":  {"command": "powershell", "args": a("-c", "uvx awslabs.aws-api-mcp-server")},
		"m28-bashwrap":   {"command": "bash", "args": a("-c", "exec npx -y @cloudflare/mcp-server-cloudflare")},
		"m29-dockerenv":  {"command": "docker", "args": a("run", "-i", "--rm", "--env=AWS_SECRET_ACCESS_KEY=x", "my/server")},
		"p01-cloud":      {"command": "uvx", "args": a("awslabs.cloudwatch-mcp-server")},
	}
	kept := map[string]map[string]any{
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
		"toolbox-pg": {"command": "npx", "args": a("-y", "@toolbox-sdk/server", "--prebuilt", "postgres", "--stdio")},
	}
	for n, e := range out {
		if what, ok := f.s.OutOfScopeMCPEntry(n, e); !ok {
			t.Errorf("%s kept, want removed", n)
		} else if what == "" {
			t.Errorf("%s: no reason", n)
		}
	}
	for n, e := range kept {
		if what, ok := f.s.OutOfScopeMCPEntry(n, e); ok {
			t.Errorf("%s removed (%s), want kept", n, what)
		}
	}
}

// Same-named servers in several sources: the hook denies when any of them is out of scope,
// whatever order the sources were read in.
func TestScope_MCPSameNameAnySourceDenies(t *testing.T) {
	f := newScopeFixture(t)
	benign := map[string]any{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-memory"}}
	aws := map[string]any{"command": "uvx", "args": []any{"awslabs.aws-api-mcp-server"}}
	for _, order := range [][]map[string]any{{benign, aws}, {aws, benign}} {
		s := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: mustCat(t).All(),
			Set: f.set, UserHome: f.home, MCPEntries: func() map[string][]map[string]any { return map[string][]map[string]any{"memory": order} }})
		if d := s.Evaluate(f.cwd, ToolCall{Tool: "mcp__memory__call_aws", Content: "{}"}); d.Action != Deny {
			t.Fatalf("order %v: allowed", order[0]["command"])
		}
	}
}
