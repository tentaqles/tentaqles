package identitystatus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secret"
)

func workspace(t *testing.T) resolve.Result {
	t.Helper()
	home := t.TempDir()
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	paths.AllowHomeOverride = true
	t.Cleanup(func() { paths.AllowHomeOverride = false })
	secret.MockInit()
	m := &manifest.Manifest{
		Client:     "acme",
		Identities: map[string]manifest.Identity{"gh": {}, "glab": {}, "claude": {}},
		ExtraScope: []string{"jira", "linear"},
		Browsers:   []manifest.Browser{{Name: "chrome", Profile: "Acme"}},
	}
	m.OtherProviders = []string{"Some Internal Tool"}
	return resolve.Result{Workspace: &resolve.Workspace{Name: "acme", Root: filepath.Join(home, "acme"), Manifest: m}}
}

func byID(s Status) map[string]Provider {
	out := map[string]Provider{}
	for _, p := range s.Providers {
		out[p.ID] = p
	}
	return out
}

// F-03: a hanging verify command gives verified false within the limit, a failing one gives
// false, a passing one true; a provider with none is never verified.
func TestStatusJSON_VerifyBounded(t *testing.T) {
	res := workspace(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	run := func(ctx context.Context, env []string, name string, args []string) (string, error) {
		switch name {
		case "gh":
			return "Logged in to github.com account someone", nil
		case "glab":
			<-release // ignores ctx: only the limit gets Build out
			return "", nil
		case "acli", "claude":
			return "", errors.New("exit status 1")
		}
		t.Errorf("unexpected verify command %q", name)
		return "", nil
	}
	start := time.Now()
	s := Build(res, Deps{
		RunCLI: run, Environ: func(*resolve.Workspace) []string { return nil },
		LookPath: func(string) (string, error) { return "", errors.New("not found") }, VerifyLimit: 300 * time.Millisecond,
	})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("status took %s with a 300ms limit", d)
	}
	got := byID(s)
	for id, want := range map[string]bool{"gh": true, "glab": false, "jira": false, "linear": false, "claude": false} {
		p, ok := got[id]
		if !ok {
			t.Fatalf("no entry for %s in %+v", id, s.Providers)
		}
		if p.Verified != want {
			t.Errorf("%s verified = %v, want %v", id, p.Verified, want)
		}
	}
}

func TestStatusJSON_Fields(t *testing.T) {
	res := workspace(t)
	if err := secret.Set("acme", "linear", "lin_api_MARKER"); err != nil {
		t.Fatal(err)
	}
	s := Build(res, Deps{
		RunCLI:  func(context.Context, []string, string, []string) (string, error) { return "", errors.New("x") },
		Environ: func(*resolve.Workspace) []string { return nil },
		LookPath: func(n string) (string, error) {
			if n == "gh" {
				return n, nil
			}
			return "", exec.ErrNotFound
		},
		Env: func(k string) (string, bool) { return "", false },
	})
	if s.Identity == nil || *s.Identity != "acme" || s.Trusted == nil || !*s.Trusted {
		t.Fatalf("identity/trusted: %+v", s)
	}
	if len(s.Browsers) != 1 || s.Browsers[0].Profile != "Acme" || len(s.OtherProviders) != 1 {
		t.Fatalf("browsers/other: %+v %+v", s.Browsers, s.OtherProviders)
	}
	got := byID(s)
	if p := got["gh"]; p.CLI == nil || *p.CLI != "gh" || !p.CLIResolves || p.Credential != Missing || p.Connection != Missing || p.Category != "vcs" {
		t.Errorf("gh: %+v", p)
	}
	if p := got["glab"]; p.CLIResolves {
		t.Errorf("glab resolves: %+v", p)
	}
	if p := got["linear"]; p.CLI != nil || p.Credential != Present || p.Connection != Missing {
		t.Errorf("linear: %+v", p)
	}
	if p := got["claude"]; p.Connection != None {
		t.Errorf("claude: %+v", p)
	}
	// The connection is by server name, read from the client's .claude.json.
	cj := filepath.Join(paths.IdentityDir("acme", "claude"), ".claude.json")
	if err := os.MkdirAll(filepath.Dir(cj), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cj, []byte(`{"mcpServers":{"github":{"type":"http","url":"https://x"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s = Build(res, Deps{Environ: func(*resolve.Workspace) []string { return nil }})
	if p := byID(s)["gh"]; p.Connection != Present {
		t.Errorf("gh connection with a github server: %+v", p)
	}
}

func TestStatusJSON_NeutralAndUntrusted(t *testing.T) {
	n := Build(resolve.Result{Reason: "outside any base"}, Deps{})
	if n.Identity != nil || n.Root != nil || n.Reason != "outside any base" || n.Scope == nil || n.Providers == nil {
		t.Errorf("neutral: %+v", n)
	}
	u := Build(resolve.Result{Reason: "manifest changed", Untrusted: &resolve.Workspace{Name: "acme", Root: "/r"}}, Deps{})
	if u.Identity == nil || *u.Identity != "acme" || u.Trusted == nil || *u.Trusted || u.Reason != "manifest changed" || len(u.Providers) != 0 || len(u.Scope) != 0 {
		t.Errorf("untrusted: %+v", u)
	}
}
