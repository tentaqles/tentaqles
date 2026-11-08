package envplan

import (
	"path/filepath"
	"reflect"
	"testing"
)

// wantProvider is what Providers() gives an identity: its config-home variables for a folder, and
// its login command.
type wantProvider struct {
	Vars      map[string]string
	LoginCmd  string
	LoginArgs []string
}

// TestProviders_ConfigHomesAndLogins pins Providers() for the main identities, for Vars("/x").
// kube has no login block in the catalog, so its login command is its CLI, kubectl.
func TestProviders_ConfigHomesAndLogins(t *testing.T) {
	dir := filepath.FromSlash("/x")
	want := map[string]wantProvider{
		"claude": {Vars: map[string]string{"CLAUDE_CONFIG_DIR": dir}, LoginCmd: "claude", LoginArgs: []string{"auth", "login"}},
		"codex":  {Vars: map[string]string{"CODEX_HOME": dir}, LoginCmd: "codex", LoginArgs: []string{"login"}},
		"gemini": {Vars: map[string]string{"GEMINI_CLI_HOME": dir, "GEMINI_FORCE_FILE_STORAGE": "true"}, LoginCmd: "gemini", LoginArgs: nil},
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
	for id, w := range want {
		p, ok := got[id]
		if !ok {
			t.Fatalf("Providers() has no %q", id)
		}
		if p.Name != id {
			t.Errorf("%s: Name = %q, want %q", id, p.Name, id)
		}
		vars := p.Vars(dir)
		if !reflect.DeepEqual(vars, w.Vars) {
			t.Errorf("%s: Vars(%q) = %#v, want %#v", id, dir, vars, w.Vars)
		}
		if p.LoginCmd != w.LoginCmd {
			t.Errorf("%s: LoginCmd = %q, want %q", id, p.LoginCmd, w.LoginCmd)
		}
		if !reflect.DeepEqual(p.LoginArgs, w.LoginArgs) {
			t.Errorf("%s: LoginArgs = %#v, want %#v", id, p.LoginArgs, w.LoginArgs)
		}
	}
}

// TestProviders_IncludesNewIdentities checks providers added by the catalog
// that carry identity env vars show up in Providers().
func TestProviders_IncludesNewIdentities(t *testing.T) {
	got := Providers()
	// azure-devops is the az CLI under its own id: `tentaqles login <ws> azure-devops` runs az login.
	for _, id := range []string{"glab", "snowflake", "databricks", "postgres", "terraform", "azure-devops"} {
		if _, ok := got[id]; !ok {
			t.Errorf("Providers() missing new identity %q", id)
		}
	}
}

// TestProviders_ExcludesInformational checks catalog entries with no
// identity env vars (informational-only providers) are excluded.
func TestProviders_ExcludesInformational(t *testing.T) {
	got := Providers()
	// cursor and opencode moved here: their config-dir variables are real but
	// cover settings, not credentials, so claiming them as identities told the
	// user each workspace had its own login when they all shared one.
	for _, id := range []string{"jira", "asana", "linear", "bitbucket", "vercel", "supabase", "cursor", "opencode"} {
		if _, ok := got[id]; ok {
			t.Errorf("Providers() should not include informational id %q", id)
		}
	}
}
