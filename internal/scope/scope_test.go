package scope

import (
	"reflect"
	"slices"
	"testing"

	"github.com/tentaqles/tentaqles/internal/providers"
)

func fixture() ([]providers.Provider, map[string]providers.Match) {
	provs := []providers.Provider{
		{ID: "aws", Match: providers.Match{Executables: []string{"aws", "sam"}, Env: []string{"AWS_PROFILE", "AWS_*"}}},
		{ID: "az", Match: providers.Match{Executables: []string{"az"}, Env: []string{"AZURE_CONFIG_DIR"}}},
		{ID: "azure-devops", Match: providers.Match{Subcommands: map[string][]string{"az": {"devops", "repos"}}, Env: []string{"AZURE_DEVOPS_EXT_PAT"}}},
		{ID: "terraform", CLI: &providers.CLI{Command: "terraform"}, Match: providers.Match{Env: []string{"TF_TOKEN_*"}}},
	}
	embedded := map[string]providers.Match{}
	for _, p := range provs {
		embedded[p.ID] = p.Match
	}
	return provs, embedded
}

// An out-of-scope provider's CLIs are hidden by every spelling; an in-scope one's are not.
func TestOutOfScopeExecutables(t *testing.T) {
	provs, emb := fixture()
	s := compile("acme", []string{"az"}, provs, emb)
	if got := s.OutOfScopeExecutables(); !reflect.DeepEqual(got, map[string]string{"aws": "aws", "sam": "aws"}) {
		t.Fatalf("OutOfScopeExecutables = %v", got)
	}
	for _, name := range []string{"aws", "AWS.EXE", `C:\Program Files\Amazon\aws.exe`, "/usr/local/bin/aws", `a\ws`} {
		if id, ok := s.OutOfScopeExecutable(name); !ok || id != "aws" {
			t.Errorf("%s: %q %v", name, id, ok)
		}
	}
	for _, name := range []string{"az", "az.cmd", "git", "awsx"} {
		if _, ok := s.OutOfScopeExecutable(name); ok {
			t.Errorf("%s is hidden", name)
		}
	}
}

// A CLI that an in-scope provider claims even by one subcommand stays on the PATH.
func TestSubcommandClaimKeepsTheCLI(t *testing.T) {
	provs, emb := fixture()
	s := compile("acme", []string{"azure-devops"}, provs, emb)
	if _, ok := s.OutOfScopeExecutable("az"); ok {
		t.Fatal("az is hidden though azure-devops, in scope, owns az devops")
	}
	if _, ok := s.OutOfScopeExecutables()["aws"]; !ok {
		t.Fatal("aws is not hidden")
	}
}

// An out-of-scope provider's variables are unset, prefixes included; an in-scope one's, and a
// generic tool's, are left alone.
func TestOutOfScopeEnv(t *testing.T) {
	provs, emb := fixture()
	s := compile("acme", []string{"az"}, provs, emb)
	got := s.OutOfScopeEnv([]string{"AWS_PROFILE", "aws_region", "AZURE_CONFIG_DIR", "TF_TOKEN_app", "PATH", "AWS_"})
	if want := []string{"AWS_PROFILE", "aws_region"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OutOfScopeEnv = %v, want %v", got, want)
	}
}

// For an in-scope provider only the embedded catalog's claim counts: a user file cannot narrow
// what a built-in hides by claiming another provider's CLI for an in-scope id.
func TestInScopeClaimIsTheEmbeddedOne(t *testing.T) {
	provs, emb := fixture()
	provs[1].Match.Executables = append(provs[1].Match.Executables, "aws") // a user file widening az
	s := compile("acme", []string{"az"}, provs, emb)
	if _, ok := s.OutOfScopeExecutable("aws"); !ok {
		t.Fatal("a user file's claim on aws for in-scope az unmasked aws")
	}
	if !slices.Contains(s.OutOfScopeEnv([]string{"AWS_PROFILE"}), "AWS_PROFILE") {
		t.Fatal("AWS_PROFILE is kept")
	}
}
