package providers

import (
	"strings"
	"testing"
)

// A new id in scope would allow what it claims, so it may not claim a built-in's git hosts, MCP
// hosts or MCP packages (the critic's inputs for this rule, ADR-0007 round 3).
func TestClaimConflict_MCPAndGitLists(t *testing.T) {
	builtin := map[string]Provider{"gh": {ID: "gh", Match: Match{
		Hosts:       []string{"api.github.com"},
		GitHosts:    []string{"github.com"},
		MCPHosts:    []string{"api.githubcopilot.com"},
		MCPPackages: []string{"@modelcontextprotocol/server-github", "@github/"},
		Images:      []string{"ghcr.io/github/github-mcp-server"},
	}}}
	refused := []struct {
		list string
		m    Match
	}{
		{"git_hosts", Match{GitHosts: []string{"github.com"}}},
		{"git_hosts", Match{GitHosts: []string{"ssh.github.com"}}},
		{"mcp_hosts", Match{MCPHosts: []string{"api.githubcopilot.com"}}},
		{"mcp_hosts", Match{MCPHosts: []string{"api.github.com"}}},
		{"hosts", Match{Hosts: []string{"api.githubcopilot.com"}}},
		{"mcp_packages", Match{MCPPackages: []string{"@modelcontextprotocol/server-github"}}},
		{"mcp_packages", Match{MCPPackages: []string{"@modelcontextprotocol/"}}},
		{"mcp_packages", Match{MCPPackages: []string{"@github/thing"}}},
		{"mcp_packages", Match{MCPPackages: []string{"docker.io/@github/"}}},
		{"mcp_packages", Match{MCPPackages: []string{"ghcr.io/github/"}}},
	}
	for _, c := range refused {
		err := claimConflict(Provider{ID: "acme", Match: c.m}, builtin)
		if err == nil || !strings.Contains(err.Error(), "match."+c.list) {
			t.Errorf("%+v: err = %v, want a refusal of match.%s", c.m, err, c.list)
		}
	}
	allowed := Match{
		Hosts: []string{"api.acme.test"}, GitHosts: []string{"git.acme.test"},
		MCPHosts: []string{"mcp.acme.test"}, MCPPackages: []string{"@acme/", "acme-mcp"},
	}
	if err := claimConflict(Provider{ID: "acme", Match: allowed}, builtin); err != nil {
		t.Errorf("a provider's own entries: %v", err)
	}
}
