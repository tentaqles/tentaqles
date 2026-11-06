package providers

import (
	"slices"
	"sort"
	"testing"
)

// GenericTool is the catalog's structural rule (ADR-0006, ADR-0008): a provider whose CLI
// command its match lists do not name. It is exactly the generic tools the catalog test lists.
func TestGenericTool(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range c.All() {
		if p.GenericTool() {
			got = append(got, p.ID)
		}
	}
	sort.Strings(got)
	want := slices.Clone(genericTools)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("generic tools = %v, want %v", got, want)
	}
}

func TestAmbiguousExecutable(t *testing.T) {
	for _, n := range []string{"cn", "tea", "snow", "render", "vc", "fly", "atlas", "copilot", "goose", "Render.EXE"} {
		if !AmbiguousExecutable(n) {
			t.Errorf("%s is not ambiguous", n)
		}
	}
	for _, n := range []string{"aws", "az", "gcloud", "flyctl", "vercel", "snowsql", "gh", "aws_completer"} {
		if AmbiguousExecutable(n) {
			t.Errorf("%s is ambiguous", n)
		}
	}
}

func TestMCPPackagesValidateAndUnion(t *testing.T) {
	good := Match{MCPPackages: []string{"awslabs.", "@aws/", "public.ecr.aws/", "ghcr.io/github/github-mcp-server", "@modelcontextprotocol/server-github"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "a b", "-x", "../x", "npm:", "git+", "docker.io/"} {
		if err := (Match{MCPPackages: []string{bad}}).Validate(); err == nil {
			t.Errorf("%q validates", bad)
		}
	}
	u := Match{MCPPackages: []string{"a/"}}.Union(Match{MCPPackages: []string{"b/", "a/"}})
	if !slices.Equal(u.MCPPackages, []string{"a/", "b/"}) {
		t.Fatalf("union = %v", u.MCPPackages)
	}
	if (Match{MCPPackages: []string{"x"}}).Empty() {
		t.Fatal("a package list is not empty")
	}
	c, _ := Embedded()
	for id, pkg := range map[string]string{"aws": "awslabs.", "gh": "@modelcontextprotocol/server-github", "gcloud": "@google-cloud/"} {
		p, _ := c.Get(id)
		if !slices.Contains(p.Match.MCPPackages, pkg) {
			t.Errorf("%s: mcp_packages %v lacks %q", id, p.Match.MCPPackages, pkg)
		}
	}
	gh, _ := c.Get("gh")
	if !slices.Contains(gh.Match.MCP, "gh") {
		t.Errorf("gh: mcp %v lacks gh", gh.Match.MCP)
	}
}
