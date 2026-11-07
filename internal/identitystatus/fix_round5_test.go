package identitystatus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/secret"
)

// L-15: a provider whose MCP server is registered reads present, whatever reach the catalog
// gives it: az's azure server, and glab's gitlab server.
func TestStatusJSON_RegisteredServerReadsPresent(t *testing.T) {
	res := workspace(t)
	res.Workspace.Manifest.ExtraScope = append(res.Workspace.Manifest.ExtraScope, "az")
	cj := filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json")
	if err := os.MkdirAll(filepath.Dir(cj), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cj, []byte(`{"mcpServers":{"azure":{"type":"http","url":"https://x.azurewebsites.net/mcp"},"gitlab":{"type":"http","url":"https://gitlab.com/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := byID(Build(res, Deps{RunCLI: noRun, Environ: noEnv, LookPath: notFound}))
	if got["az"].Connection != Present {
		t.Errorf("az connection = %q, want present", got["az"].Connection)
	}
	if got["glab"].Connection != Present {
		t.Errorf("glab connection = %q, want present", got["glab"].Connection)
	}
}

// L-18: a CLI-login provider with no verify command, no credential variable and no keychain token
// reads credential none; with one of those it reads present; a provider with a verify keeps its
// reading (missing when the verify fails).
func TestStatusJSON_CredentialReadings(t *testing.T) {
	res := workspace(t)
	res.Workspace.Manifest.ExtraScope = append(res.Workspace.Manifest.ExtraScope, "az")
	noEnvAt := func(string) (string, bool) { return "", false }
	failing := func(context.Context, []string, string, []string) (string, error) { return "", errors.New("logged out") }
	base := Deps{RunCLI: failing, Environ: noEnv, LookPath: notFound, Env: noEnvAt}

	// No verify, no variable, no token: none.
	if p := byID(Build(res, base))["az"]; p.Credential != None {
		t.Errorf("az with nothing = %q, want none", p.Credential)
	}
	// A credential variable of the provider: present.
	withVar := base
	withVar.Env = func(k string) (string, bool) {
		if k == "AZURE_CLIENT_SECRET" {
			return "x", true
		}
		return "", false
	}
	if p := byID(Build(res, withVar))["az"]; p.Credential != Present {
		t.Errorf("az with a credential variable = %q, want present", p.Credential)
	}
	// A verify command with a failed check: missing, as before.
	if p := byID(Build(res, base))["gh"]; p.Credential != Missing {
		t.Errorf("gh (verify failed, no token) = %q, want missing", p.Credential)
	}
	// A keychain token for a tracker provider: present.
	if err := secret.Set("acme", "linear", "lin_api_TOKEN_ROUND5"); err != nil {
		t.Fatal(err)
	}
	if p := byID(Build(res, base))["linear"]; p.Credential != Present {
		t.Errorf("linear with a keychain token = %q, want present", p.Credential)
	}
	// A verify that passes: present.
	passing := func(_ context.Context, _ []string, name string, _ []string) (string, error) {
		if name == "gh" {
			return "Logged in to github.com account someone", nil
		}
		return "", errors.New("exit 1")
	}
	pass := base
	pass.RunCLI = passing
	if p := byID(Build(res, pass))["gh"]; p.Credential != Present {
		t.Errorf("gh with a passing verify = %q, want present", p.Credential)
	}
}
