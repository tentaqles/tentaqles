package providers

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// genericTools are the catalog providers whose command is never matched by name (ADR-0007):
// each talks to whatever the project configures. cursor's command, `agent`, is too generic a
// word to deny; its match lists cursor-agent instead.
var genericTools = []string{"dbt", "docker", "helm", "kube", "npm", "postgres", "pulumi", "terraform", "cursor"}

// TestCatalogMatchLists: every one of the catalog files carries a match: block, every block
// validates, and a provider with a vendor CLI lists that CLI as an executable.
func TestCatalogMatchLists(t *testing.T) {
	entries, err := embeddedFS.ReadDir("catalog")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		raw, err := embeddedFS.ReadFile(path.Join("catalog", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		id := strings.TrimSuffix(e.Name(), ".yaml")
		if !strings.Contains(string(raw), "\nmatch:") {
			t.Errorf("%s: no match: block", e.Name())
		}
		p, _ := c.Get(id)
		if err := p.Match.Validate(); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		if p.CLI != nil && !slices.Contains(genericTools, id) {
			_, claimedAsSub := p.Match.Subcommands[p.CLI.Command]
			if !slices.Contains(p.Match.Executables, p.CLI.Command) && !claimedAsSub {
				t.Errorf("%s: match does not claim the CLI %q (as an executable or subcommands key)", id, p.CLI.Command)
			}
		}
		if slices.Contains(genericTools, id) && p.CLI != nil && slices.Contains(p.Match.Executables, p.CLI.Command) {
			t.Errorf("%s: a generic tool must not match its command %q by name", id, p.CLI.Command)
		}
		n++
	}
	if n != 67 {
		t.Fatalf("catalog has %d files, want 62", n)
	}
	aws, _ := c.Get("aws")
	if !slices.Contains(aws.Match.Modules, "awscli") || !slices.Contains(aws.Match.Paths, "~/.aws") ||
		!slices.Contains(aws.Match.Hosts, "amazonaws.com") || !slices.Contains(aws.Match.MCP, "aws") ||
		!slices.Contains(aws.Match.Env, "AWS_SECRET_ACCESS_KEY") {
		t.Fatalf("aws match = %+v", aws.Match)
	}
}

func TestMatchValidate(t *testing.T) {
	good := Match{
		Executables: []string{"aws", "linode-cli", "aws_completer"}, Modules: []string{"azure.cli", "awscli"},
		Env: []string{"AWS_PROFILE", "TF_TOKEN_*"}, Paths: []string{"~/.aws", "%APPDATA%/gcloud", "%LOCALAPPDATA%/x/y.json"},
		Hosts: []string{"amazonaws.com", "api.aws"}, MCP: []string{"aws"},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]Match{
		"exe with a path":     {Executables: []string{"bin/aws"}},
		"exe with a space":    {Executables: []string{"aws cli"}},
		"module with a dash":  {Modules: []string{"aws-cli"}},
		"env with a dash":     {Env: []string{"AWS-PROFILE"}},
		"env star in middle":  {Env: []string{"AWS_*_KEY"}},
		"path without a root": {Paths: []string{".aws"}},
		"absolute path":       {Paths: []string{"/home/me/.aws"}},
		"path with ..":        {Paths: []string{"~/../etc"}},
		"path with backslash": {Paths: []string{`~/.aws\credentials`}},
		"path with a glob":    {Paths: []string{"~/.aws/*"}},
		"bare root":           {Paths: []string{"~/"}},
		"host with a scheme":  {Hosts: []string{"https://amazonaws.com"}},
		"host one label":      {Hosts: []string{"localhost"}},
		"host upper":          {Hosts: []string{"AmazonAWS.com"}},
		"mcp with a dash":     {MCP: []string{"aws-labs"}},
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: Validate accepted %+v", name, m)
		}
	}
}

// TestUserOverrideExtendsMatch: a user file for a catalog id replaces the entry but its match
// lists are the union with the embedded ones, so an override never shortens what the guard
// recognises; a new id brings its own lists.
func TestUserOverrideExtendsMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TQ_HOME", home)
	dir := filepath.Join(home, "providers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("aws.yaml", "id: aws\nname: AWS\ncategory: cloud\ncli: { command: aws }\nmatch:\n  executables: [awsv2]\n  hosts: [example-aws.dev]\n")
	write("acme-cloud.yaml", "id: acme-cloud\nname: Acme Cloud\ncategory: cloud\nmatch:\n  executables: [acme]\n  mcp: [acme]\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	aws, _ := c.Get("aws")
	for _, want := range []string{"aws", "eksctl", "awsv2"} {
		if !slices.Contains(aws.Match.Executables, want) {
			t.Errorf("aws executables %v lack %q", aws.Match.Executables, want)
		}
	}
	if !slices.Contains(aws.Match.Hosts, "amazonaws.com") || !slices.Contains(aws.Match.Hosts, "example-aws.dev") {
		t.Errorf("aws hosts = %v", aws.Match.Hosts)
	}
	if !slices.Contains(aws.Match.Paths, "~/.aws") {
		t.Errorf("an override with no paths dropped the embedded ones: %v", aws.Match.Paths)
	}
	acme, ok := c.Get("acme-cloud")
	if !ok || !slices.Contains(acme.Match.Executables, "acme") {
		t.Fatalf("acme-cloud = %+v %v", acme.Match, ok)
	}

	write("bad.yaml", "id: bad\nname: Bad\ncategory: cloud\nmatch:\n  executables: [\"a b\"]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "match.executables") {
		t.Fatalf("a bad match entry loaded: %v", err)
	}
}
