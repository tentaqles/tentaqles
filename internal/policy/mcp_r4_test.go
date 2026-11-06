package policy

import "testing"

// The QA round 3 fixtures (gen3.py): official vendor servers of catalog providers, an
// out-of-scope CLI started by a package runner, and a vendor's credential variable referenced
// anywhere in the entry are attributed; the kept set stays.
func TestOutOfScopeMCPEntry_Round3Fixtures(t *testing.T) {
	f := newScopeFixture(t)
	a := func(xs ...string) []any {
		out := make([]any, len(xs))
		for i, x := range xs {
			out[i] = x
		}
		return out
	}
	h := func(k, v string) map[string]any { return map[string]any{k: v} }
	out := map[string]map[string]any{
		"c01-pay":           {"type": "http", "url": "https://mcp.stripe.com"},
		"c02-err":           {"type": "http", "url": "https://mcp.sentry.dev/mcp"},
		"c03-iss":           {"type": "sse", "url": "https://mcp.linear.app/sse"},
		"c04-wiki":          {"type": "http", "url": "https://mcp.notion.com/mcp"},
		"c05-tix":           {"type": "sse", "url": "https://mcp.atlassian.com/v1/sse"},
		"c06-pgc":           {"type": "http", "url": "https://mcp.neon.tech/mcp"},
		"c07-sites":         {"command": "npx", "args": a("-y", "@netlify/mcp")},
		"c08-dyno":          {"command": "npx", "args": a("-y", "@heroku/mcp-server")},
		"c09-atlas":         {"command": "npx", "args": a("-y", "mongodb-mcp-server", "--readOnly")},
		"c10-fb":            {"command": "npx", "args": a("-y", "firebase-tools@latest", "mcp")},
		"c11-hdr":           {"type": "http", "url": "https://gw.example.com/mcp", "headers": h("Authorization", "Bearer ${VERCEL_TOKEN}")},
		"c12-envref":        {"command": "node", "args": a("C:/t/s.js"), "env": h("API_TOKEN", "${GITHUB_PERSONAL_ACCESS_TOKEN}")},
		"c13-envfile":       {"command": "docker", "args": a("run", "-i", "--rm", "--env-file", "C:/sec/aws.env", "ghcr.io/acme/tool")},
		"c13b-eattached":    {"command": "docker", "args": a("run", "-i", "--rm", "-eAWS_SECRET_ACCESS_KEY", "ghcr.io/acme/tool")},
		"c14-pipx":          {"command": "pipx", "args": a("run", "awslabs.aws-api-mcp-server")},
		"c15-uvxfrom":       {"command": "uvx", "args": a("--from", "awslabs.cloudwatch-mcp-server@latest", "awslabs.cloudwatch-mcp-server")},
		"c16-dockerio":      {"command": "docker", "args": a("run", "-i", "--rm", "docker.io/mcp/github")},
		"c17-upper":         {"type": "http", "url": "HTTPS://MCP.VERCEL.COM/"},
		"c18-portuser":      {"type": "http", "url": "https://u@mcp.supabase.com:443/mcp"},
		"c19-dot":           {"type": "http", "url": "https://mcp.vercel.com./"},
		"c20-mcpremote-hdr": {"command": "npx", "args": a("mcp-remote", "https://proxy.example.com/mcp", "--header", "Authorization:Bearer ${CLOUDFLARE_API_TOKEN}")},
		"c21-bunx":          {"command": "bunx", "args": a("@supabase/mcp-server-supabase")},
		"c22-pnpm":          {"command": "pnpm", "args": a("dlx", "@vercel/mcp")},
		"c23-npmexec":       {"command": "npm", "args": a("exec", "--", "@aws/aws-mcp")},
		"c24-cmdwrap":       {"command": "cmd", "args": a("/c", "npx -y @cloudflare/mcp-server-cloudflare run x")},
		"c25-deno":          {"command": "deno", "args": a("run", "-A", "npm:@supabase/mcp-server-supabase")},
		"c26-gitlab":        {"type": "http", "url": "https://gitlab.com/api/v4/mcp"},
		"c27-sentrypkg":     {"command": "npx", "args": a("-y", "@sentry/mcp-server@latest")},
		"c28-awsenv":        {"command": "node", "args": a("C:/t/s.js"), "env": map[string]any{"PROFILE": "x", "AWS_REGION": "us-east-1"}},
		"c29-headerkey":     {"type": "http", "url": "https://gw.example.com/mcp", "headers": h("X-Vercel-Token", "x")},
		"c30-gcloudfull":    {"command": "C:/Users/dev/AppData/Local/Google/Cloud SDK/google-cloud-sdk/bin/gcloud.cmd", "args": a("mcp")},
		"c31-ghcopilot-ws":  {"type": "http", "url": "https://api.githubcopilot.com/mcp/x/repos/readonly"},
		"c32-wrangler":      {"command": "npx", "args": a("-y", "wrangler", "dev")},
		"c33-ghcli":         {"command": "gh", "args": a("mcp-server")},
		"c34-vercelcli":     {"command": "npx", "args": a("-y", "vercel@latest", "mcp")},
		"n3":                {"command": "npx", "args": a("-y", "wrangler@latest", "dev")},
		"n5":                {"command": "uvx", "args": a("--with", "awscli", "python", "-m", "awscli", "s3", "ls")},
		"n6":                {"command": "npx", "args": a("-y", "@railway/mcp-server")},
		"n7":                {"type": "http", "url": "https://mcp.render.com/mcp"},
		"n8":                {"command": "docker", "args": a("run", "-i", "--rm", "--env", "AWS_ACCESS_KEY_ID=AKIA", "ghcr.io/acme/tool")},
		"n9-with-eq":        {"command": "uvx", "args": a("--with=awscli", "python", "x.py")},
		"n10-pkgflag":       {"command": "npx", "args": a("-p", "vercel", "mcp-tool")},
		"cx-bearer":         {"url": "https://gw.example.com/mcp", "env": h("VERCEL_TOKEN", "")}, // Codex bearer_token_env_var, read as env
		"cx-dollar":         {"command": "sh", "args": a("-c", "TOKEN=$VERCEL_TOKEN node s.js")},
	}
	kept := map[string]map[string]any{
		"k01-azure":        {"command": "npx", "args": a("-y", "@azure/mcp@latest", "server", "start")},
		"k02-ado":          {"command": "npx", "args": a("-y", "@azure-devops/mcp", "myorg")},
		"k03-learn":        {"type": "http", "url": "https://learn.microsoft.com/api/mcp"},
		"k04-pw":           {"command": "npx", "args": a("@playwright/mcp@latest")},
		"k05-c7":           {"command": "npx", "args": a("-y", "@upstash/context7-mcp")},
		"k06-seq":          {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-sequential-thinking")},
		"k07-fs":           {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-filesystem", "C:/work")},
		"k08-pgdock":       {"command": "docker", "args": a("run", "-i", "--rm", "-e", "DATABASE_URL", "mcp/postgres", "postgresql://localhost/db")},
		"k09-time":         {"command": "uvx", "args": a("mcp-server-time")},
		"k10-azenv":        {"command": "node", "args": a("C:/a/s.js"), "env": map[string]any{"AZURE_TENANT_ID": "t", "AZURE_CLIENT_ID": "c"}},
		"k11-git":          {"command": "uvx", "args": a("mcp-server-git", "--repository", ".")},
		"k12-azfunc":       {"type": "http", "url": "https://myfunc.azurewebsites.net/runtime/webhooks/mcp", "headers": h("x-functions-key", "${AZURE_FUNCTIONS_KEY}")},
		"k13-localpy":      {"command": "python", "args": a("C:/work/tools/mcp_server.py"), "env": h("DB_PATH", "C:/work/db.sqlite")},
		"k14-sqlite":       {"command": "uvx", "args": a("mcp-server-sqlite", "--db-path", "C:/work/db.sqlite")},
		"k15-everything":   {"command": "npx", "args": a("-y", "@modelcontextprotocol/server-everything")},
		"k16-azmcp-docker": {"command": "docker", "args": a("run", "-i", "--rm", "-e", "AZURE_TENANT_ID", "mcr.microsoft.com/azure-sdk/azure-mcp")},
		"k17-node-env":     {"command": "node", "args": a("C:/work/srv.js"), "env": map[string]any{"NODE_ENV": "development", "PORT": "3000", "GIT_AUTHOR_NAME": "me"}},
		"k18-npm-flags":    {"command": "npx", "args": a("-y", "--registry", "https://registry.npmjs.org", "@modelcontextprotocol/server-memory")},
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
