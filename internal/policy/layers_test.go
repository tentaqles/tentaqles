package policy

import (
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// An MCP server is attributed by its config entry, not only its name (ADR-0008): the
// package or image it runs, its command, the variables it sets and its url.
func TestOutOfScopeMCPEntry(t *testing.T) {
	f := newScopeFixture(t)
	cases := []struct {
		name  string
		entry map[string]any
		out   bool
	}{
		{"cloud", map[string]any{"command": "uvx", "args": []any{"awslabs.aws-api-mcp-server@latest"}}, true},
		{"cloud2", map[string]any{"command": "npx", "args": []any{"-y", "@aws/mcp"}}, true},
		{"img", map[string]any{"command": "docker", "args": []any{"run", "-i", "--rm", "public.ecr.aws/awslabs/x"}}, true},
		{"helper", map[string]any{"command": "node", "args": []any{"server.js"}, "env": map[string]any{"AWS_PROFILE": "prod"}}, true},
		{"gh", map[string]any{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-github"}}, true},
		{"code", map[string]any{"command": "docker", "args": []any{"run", "ghcr.io/github/github-mcp-server"}}, true},
		{"remote", map[string]any{"type": "http", "url": "https://bedrock-runtime.us-east-1.amazonaws.com/mcp"}, true},
		{"argurl", map[string]any{"command": "npx", "args": []any{"mcp-remote", "https://api.github.com/mcp"}}, true},
		{"runs-aws", map[string]any{"command": `C:\Program Files\Amazon\AWSCLIV2\aws.exe`}, true},
		{"local", map[string]any{"command": "node", "args": []any{`C:\Users\me\AppData\Roaming\npm\node_modules\@aws\mcp\dist\index.js`}}, true},
		{"cli-in-docker", map[string]any{"command": "docker", "args": []any{"run", "-i", "amazon/aws-cli:2.17"}}, true}, // P2.1's images list
		{"aws", nil, true}, // by name, as before
		// kept
		{"azure", map[string]any{"command": "npx", "args": []any{"-y", "@azure/mcp@latest"}}, false},
		{"mydb", map[string]any{"command": "uvx", "args": []any{"postgres-mcp"}, "env": map[string]any{"DATABASE_URL": "x"}}, false},
		{"npmreg", map[string]any{"command": "node", "args": []any{"x.js"}, "env": map[string]any{"NPM_TOKEN": "t"}}, false}, // a generic tool's variable
		{"docs", map[string]any{"url": "https://docs.example.com/mcp"}, false},
		{"azure-bridge", map[string]any{"command": "uvx", "args": []any{"awslabs.aws-api-mcp-server"}}, true}, // the entry is AWS's whatever the name
	}
	for _, c := range cases {
		what, out := f.s.OutOfScopeMCPEntry(c.name, c.entry)
		if out != c.out {
			t.Errorf("%s: out=%v (%s), want %v", c.name, out, what, c.out)
		}
		if out && what == "" {
			t.Errorf("%s: no reason", c.name)
		}
	}
}

// The hook judges an mcp__<server>__ call by that server's entry in the identity's harness
// config, so a server renamed to a word of no provider is still denied.
func TestScope_MCPByConfigEntry(t *testing.T) {
	f := newScopeFixture(t)
	entries := map[string][]map[string]any{
		"cloud":   {{"command": "uvx", "args": []any{"awslabs.aws-api-mcp-server@latest"}, "env": map[string]any{"AWS_PROFILE": "prod"}}},
		"my.tool": {{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-github"}}},
		"mydb":    {{"command": "uvx", "args": []any{"postgres-mcp"}}},
	}
	calls := 0
	s := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az", "azure-devops", "claude"}, Providers: mustCat(t).All(),
		Set: f.set, UserHome: f.home, MCPEntries: func() map[string][]map[string]any { calls++; return entries }})
	for _, c := range []struct {
		tool string
		deny bool
	}{
		{"mcp__cloud__call_aws", true},
		{"mcp__my_tool__create_issue", true}, // Claude spells a dot of the server name as _
		{"mcp__mydb__query", false},
		{"mcp__unknown__x", false},
	} {
		d := s.Evaluate(f.cwd, ToolCall{Tool: c.tool, Content: "{}"})
		if (d.Action == Deny) != c.deny {
			t.Errorf("%s: deny=%v", c.tool, d.Action == Deny)
		}
		if c.deny && d.Matched[0].ID != OutOfScopeMCPID {
			t.Errorf("%s: rule %s", c.tool, d.Matched[0].ID)
		}
	}
	if calls != 1 {
		t.Fatalf("entries loaded %d times, want once", calls)
	}
	// Not an MCP call: the entries are never read.
	s2 := NewScope(ScopeInputs{Identity: "company-x", Scope: []string{"az"}, Providers: mustCat(t).All(), Set: f.set,
		MCPEntries: func() map[string][]map[string]any { t.Fatal("read for a shell call"); return nil }})
	s2.Evaluate(f.cwd, ToolCall{Tool: "Bash", Command: "ls"})
}

func mustCat(t *testing.T) *providers.Catalog {
	t.Helper()
	c, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The layers never unset a generic tool's variable (NPM_TOKEN, TF_TOKEN_*): it is the
// project's, not a vendor account's.
func TestOutOfScopeEnv_SkipsGenericTools(t *testing.T) {
	s := ScopeFor("company-x", []string{"az", "claude"}, nil, nil)
	got := s.OutOfScopeEnv([]string{"NPM_TOKEN", "TF_TOKEN_app_terraform_io", "PULUMI_ACCESS_TOKEN", "AWS_PROFILE", "SLACK_BOT_TOKEN"})
	if want := []string{"AWS_PROFILE", "SLACK_BOT_TOKEN"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OutOfScopeEnv = %v, want %v", got, want)
	}
}
