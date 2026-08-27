package envplan

import (
	"path/filepath"
	"reflect"
	"testing"
)

// legacyProvider is the pre-refactor hard-coded shape, kept here only to
// pin the contract for TestProviders_MatchesLegacyTable.
type legacyProvider struct {
	Vars      map[string]string
	LoginCmd  string
	LoginArgs []string
}

// TestProviders_MatchesLegacyTable pins Providers() to the values the old
// hard-coded map produced for the original 10 identities, for Vars("/x").
// Note: "kube" LoginCmd changes from "" (old hard-coded default) to
// "kubectl" under the catalog-driven fallback rule (LoginCmd = Login.Command
// if set, else CLI.Command). This is an intentional, documented behavior
// change from the provider-catalog refactor, not a regression: kube has no
// login block in the catalog and its CLI.Command is "kubectl".
func TestProviders_MatchesLegacyTable(t *testing.T) {
	dir := filepath.FromSlash("/x")
	legacy := map[string]legacyProvider{
		"claude": {Vars: map[string]string{"CLAUDE_CONFIG_DIR": dir}, LoginCmd: "claude", LoginArgs: []string{"/login"}},
		"codex":  {Vars: map[string]string{"CODEX_HOME": dir}, LoginCmd: "codex", LoginArgs: []string{"login"}},
		"gemini": {Vars: map[string]string{"GEMINI_CLI_HOME": dir}, LoginCmd: "gemini", LoginArgs: nil},
		"cursor": {Vars: map[string]string{"CURSOR_CONFIG_DIR": dir}, LoginCmd: "agent", LoginArgs: []string{"login"}},
		"gh":     {Vars: map[string]string{"GH_CONFIG_DIR": dir}, LoginCmd: "gh", LoginArgs: []string{"auth", "login"}},
		"az":     {Vars: map[string]string{"AZURE_CONFIG_DIR": dir}, LoginCmd: "az", LoginArgs: []string{"login"}},
		"gcloud": {Vars: map[string]string{"CLOUDSDK_CONFIG": dir}, LoginCmd: "gcloud", LoginArgs: []string{"auth", "login"}},
		"aws": {Vars: map[string]string{
			"AWS_CONFIG_FILE":             filepath.Join(dir, "config"),
			"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "credentials"),
		}, LoginCmd: "aws", LoginArgs: []string{"configure"}},
		"kube": {Vars: map[string]string{"KUBECONFIG": filepath.Join(dir, "config")}, LoginCmd: "kubectl", LoginArgs: nil},
		"npm":  {Vars: map[string]string{"NPM_CONFIG_USERCONFIG": filepath.Join(dir, "npmrc")}, LoginCmd: "npm", LoginArgs: []string{"login"}},
	}

	got := Providers()
	for id, want := range legacy {
		p, ok := got[id]
		if !ok {
			t.Fatalf("Providers() missing legacy id %q", id)
		}
		if p.Name != id {
			t.Errorf("%s: Name = %q, want %q", id, p.Name, id)
		}
		vars := p.Vars(dir)
		if !reflect.DeepEqual(vars, want.Vars) {
			t.Errorf("%s: Vars(%q) = %#v, want %#v", id, dir, vars, want.Vars)
		}
		if p.LoginCmd != want.LoginCmd {
			t.Errorf("%s: LoginCmd = %q, want %q", id, p.LoginCmd, want.LoginCmd)
		}
		if !reflect.DeepEqual(p.LoginArgs, want.LoginArgs) {
			t.Errorf("%s: LoginArgs = %#v, want %#v", id, p.LoginArgs, want.LoginArgs)
		}
	}
}

// TestProviders_IncludesNewIdentities checks providers added by the catalog
// that carry identity env vars show up in Providers().
func TestProviders_IncludesNewIdentities(t *testing.T) {
	got := Providers()
	for _, id := range []string{"glab", "snowflake", "databricks", "opencode", "terraform"} {
		if _, ok := got[id]; !ok {
			t.Errorf("Providers() missing new identity %q", id)
		}
	}
}

// TestProviders_ExcludesInformational checks catalog entries with no
// identity env vars (informational-only providers) are excluded.
func TestProviders_ExcludesInformational(t *testing.T) {
	got := Providers()
	for _, id := range []string{"jira", "asana", "linear", "bitbucket", "vercel", "supabase", "postgres", "azure-devops"} {
		if _, ok := got[id]; ok {
			t.Errorf("Providers() should not include informational id %q", id)
		}
	}
}
