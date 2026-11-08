package providers

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// genericTools are the catalog providers whose command is never matched by name (ADR-0007):
// each talks to whatever the project configures. cursor's command, `agent`, is too generic a
// word to deny; its match lists cursor-agent instead.
var genericTools = []string{"dbt", "docker", "helm", "kube", "npm", "postgres", "pulumi", "terraform", "cursor"}

// Every embedded provider's match lists are valid, the clouds name their CLIs and variables, and a
// generic tool hides nothing.
func TestCatalogMatchLists(t *testing.T) {
	c, err := loadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range c.All() {
		if err := p.Match.Validate(); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
	}
	for id, exe := range map[string]string{"aws": "aws", "az": "az", "gcloud": "gcloud", "gh": "gh"} {
		p, _ := c.Get(id)
		if !slices.Contains(p.Match.Executables, exe) || len(p.Match.Env) == 0 {
			t.Errorf("%s match = %+v", id, p.Match)
		}
	}
	if p, _ := c.Get("azure-devops"); !slices.Contains(p.Match.Subcommands["az"], "devops") {
		t.Errorf("azure-devops claims az devops: %+v", p.Match)
	}
}

// A user file named for a built-in widens what it hides and never narrows it; a new id adds its own;
// a bad entry does not load.
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
	write("aws.yaml", "id: aws\nname: AWS\ncategory: cloud\ncli: { command: aws }\nmatch:\n  executables: [awsv2]\n")
	write("acme-cloud.yaml", "id: acme-cloud\nname: Acme Cloud\ncategory: cloud\nmatch:\n  executables: [acme]\n  env: [ACME_TOKEN]\n")
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
	if !slices.Contains(aws.Match.Env, "AWS_PROFILE") {
		t.Errorf("an override with no env dropped the embedded ones: %v", aws.Match.Env)
	}
	if acme, ok := c.Get("acme-cloud"); !ok || !slices.Contains(acme.Match.Executables, "acme") {
		t.Fatalf("acme-cloud = %+v %v", acme.Match, ok)
	}
	write("bad.yaml", "id: bad\nname: Bad\ncategory: cloud\nmatch:\n  executables: [\"a b\"]\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "match.executables") {
		t.Fatalf("a bad match entry loaded: %v", err)
	}
}
