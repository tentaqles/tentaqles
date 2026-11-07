package identitystatus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func noRun(context.Context, []string, string, []string) (string, error) {
	return "", errors.New("exit status 1")
}

func noEnv(*resolve.Workspace) []string { return nil }

func notFound(string) (string, error) { return "", exec.ErrNotFound }

// L-05: a keychain lookup that blocks gives credential missing at the limit, and does not hold
// the command.
func TestStatusJSON_KeychainBounded(t *testing.T) {
	res := workspace(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	start := time.Now()
	s := Build(res, Deps{
		RunCLI: noRun, Environ: noEnv, LookPath: notFound,
		Env:           func(string) (string, bool) { return "", false },
		HasToken:      func(string, string) (bool, error) { <-release; return true, nil },
		KeychainLimit: 200 * time.Millisecond,
	})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("status took %s with a 200ms keychain limit", d)
	}
	for _, id := range []string{"gh", "linear", "jira"} {
		if p := byID(s)[id]; p.Credential != Missing {
			t.Errorf("%s credential = %q, want missing", id, p.Credential)
		}
	}
	// A lookup that answers in time still counts.
	s = Build(res, Deps{
		RunCLI: noRun, Environ: noEnv, LookPath: notFound,
		Env:      func(string) (string, bool) { return "", false },
		HasToken: func(_, name string) (bool, error) { return name == "linear", nil },
	})
	if byID(s)["linear"].Credential != Present || byID(s)["gh"].Credential != Missing {
		t.Errorf("answered lookups: %+v", s.Providers)
	}
}

// L-03: a user file that overrides a provider replaces its merged entry, which never has
// connect:; the connection is still judged by the embedded entry.
func TestStatusJSON_ConnectionSurvivesAnOverride(t *testing.T) {
	res := workspace(t)
	if err := os.MkdirAll(providers.UserDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	override := "id: gh\nname: GitHub\ncategory: vcs\nmatch:\n  env: [MY_GH_VAR]\n"
	if err := os.WriteFile(filepath.Join(providers.UserDir(), "gh.yaml"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, errs := providers.LoadAll()
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if p, _ := cat.Get("gh"); p.Connect != nil || !slices.Contains(p.Match.Env, "MY_GH_VAR") {
		t.Fatalf("the override is not in the merged catalog: %+v", p)
	}
	deps := Deps{Catalog: cat, RunCLI: noRun, Environ: noEnv, LookPath: notFound}
	if p := byID(Build(res, deps))["gh"]; p.Connection != Missing {
		t.Errorf("before connect: %+v", p)
	}
	cj := filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json")
	if err := os.MkdirAll(filepath.Dir(cj), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cj, []byte(`{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := byID(Build(res, deps))["gh"]; p.Connection != Present {
		t.Errorf("after connect: %+v", p)
	}
}

// L-06: an identity with no claude config reads missing, from no config file: a stale one in
// the identity's folder is not read.
func TestStatusJSON_NoClaudeIdentityReadsMissing(t *testing.T) {
	res := workspace(t)
	delete(res.Workspace.Manifest.Identities, "claude")
	cj := filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json")
	if err := os.MkdirAll(filepath.Dir(cj), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cj, []byte(`{"mcpServers":{"github":{"type":"http","url":"https://x.test"},"linear":{"type":"http","url":"https://y.test"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Build(res, Deps{RunCLI: noRun, Environ: noEnv, LookPath: notFound})
	got := byID(s)
	if got["gh"].Connection != Missing || got["linear"].Connection != Missing {
		t.Errorf("connections = gh %q, linear %q, want missing", got["gh"].Connection, got["linear"].Connection)
	}
	if got["glab"].Connection != None {
		t.Errorf("glab, which no MCP server reaches: %q", got["glab"].Connection)
	}
}
