package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
	"github.com/tentaqles/tentaqles/cli/internal/testutil"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func containsFinding(fs []Finding, code string) bool { return has(fs, code) }

// setupTrustedWorkspace registers an isolated TQ_HOME + base and writes a
// trusted manifest with a claude identity and the given git email. It returns
// the config and the workspace root.
func setupTrustedWorkspace(t *testing.T, name, email string) (*registry.Config, string) {
	t.Helper()
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(root, manifest.FileName)
	body := "schema: tentaqles-client-v2\nclient: " + name + "\ngit: { email: " + email + " }\nidentities: { claude: {} }\n"
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(h); err != nil {
		t.Fatal(err)
	}
	return cfg, root
}

func TestRunForCwd_EmailDrift(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{
		Cwd: ws,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x"}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(dir string, args ...string) (string, error) { return "me@other.com", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil },
	}
	r := RunForCwd(cfg, d)
	if r.Result.Workspace == nil {
		t.Fatal("expected workspace")
	}
	if !contains(r.Codes(), "git-email-drift") {
		t.Fatalf("codes %v", r.Codes())
	}
	if r.ActualEmail != "me@other.com" {
		t.Fatal(r.ActualEmail)
	}
}

func TestRunForCwd_ClaudeConfigDrift(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: ws,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x", "CLAUDE_CONFIG_DIR": "/somewhere/else"}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(string, ...string) (string, error) { return "dev@acme.com", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	r := RunForCwd(cfg, d)
	if !contains(r.Codes(), "claude-config-drift") {
		t.Fatalf("codes %v", r.Codes())
	}
	if contains(r.Codes(), "git-email-drift") {
		t.Fatal("email matches, no drift")
	}
}

func TestRunForCwd_ClaudeConfigMatch(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	res := resolve.Resolve(ws, cfg)
	want := envplan.Desired(res.Workspace)["CLAUDE_CONFIG_DIR"]
	if want == "" {
		t.Fatal("expected a CLAUDE_CONFIG_DIR for a claude identity")
	}
	d := Deps{Cwd: ws,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x", "CLAUDE_CONFIG_DIR": want}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(string, ...string) (string, error) { return "dev@acme.com", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	if r := RunForCwd(cfg, d); contains(r.Codes(), "claude-config-drift") {
		t.Fatalf("expected no drift, got %v", r.Codes())
	}
}

func TestRunForCwd_Neutral(t *testing.T) {
	cfg, _ := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: testutil.TempDir(t), Env: func(string) (string, bool) { return "", false },
		RunGitIn: func(string, ...string) (string, error) { return "", nil }, LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	r := RunForCwd(cfg, d)
	if r.Result.Workspace != nil || r.Result.Reason == "" {
		t.Fatal("expected neutral with reason")
	}
	if len(r.Findings) != 0 {
		t.Fatalf("neutral cwd must produce no findings, got %v", r.Codes())
	}
}

func TestRun_UsesRunForCwd(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: ws, Env: func(string) (string, bool) { return "", false },
		RunGit: func(...string) (string, error) { return "", nil }, RunGitIn: func(string, ...string) (string, error) { return "", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	all := Run(cfg, d)
	want := RunForCwd(cfg, d).Codes()
	for _, c := range want {
		if !containsFinding(all, c) {
			t.Fatalf("Run lacks cwd finding %s", c)
		}
	}
}

func TestRunForCwd_GitRunsInCwdNotWorkspaceRoot(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	sub := filepath.Join(ws, "nested", "repo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	var gotDir string
	d := Deps{Cwd: sub,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x"}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(dir string, args ...string) (string, error) {
			gotDir = dir
			return "me@other.com", nil
		},
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	r := RunForCwd(cfg, d)
	if r.Result.Workspace == nil {
		t.Fatal("expected workspace for a nested dir")
	}
	if gotDir != sub {
		t.Fatalf("git must run in the cwd %q, ran in %q", sub, gotDir)
	}
	if !contains(r.Codes(), "git-email-drift") {
		t.Fatalf("codes %v", r.Codes())
	}
}

func TestRunForCwd_GitAbsentSkipsEmailCheck(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: ws,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x"}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(string, ...string) (string, error) { return "me@other.com", nil },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	r := RunForCwd(cfg, d)
	if contains(r.Codes(), "git-email-drift") {
		t.Fatalf("git absent must skip the email check: %v", r.Codes())
	}
	if r.ActualEmail != "" {
		t.Fatalf("ActualEmail must stay empty when git is absent, got %q", r.ActualEmail)
	}
}

func TestRunForCwd_ClaudeConfigUnsetWithTQWS(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: ws,
		Env: func(k string) (string, bool) {
			m := map[string]string{"TQ_WS": "acme", envplan.StateVar: "x"}
			v, ok := m[k]
			return v, ok
		},
		RunGitIn: func(string, ...string) (string, error) { return "dev@acme.com", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	r := RunForCwd(cfg, d)
	if !contains(r.Codes(), "claude-config-drift") {
		t.Fatalf("unset CLAUDE_CONFIG_DIR with TQ_WS set must drift: %v", r.Codes())
	}
	if contains(r.Codes(), "hook-missing") {
		t.Fatalf("hook-missing must not double-report: %v", r.Codes())
	}
}

func TestRunForCwd_UntrustedIsStructural(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	// Rewrite the manifest so its hash no longer matches the trusted one.
	mp := filepath.Join(ws, manifest.FileName)
	if err := os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: dev@acme.com }\nidentities: { claude: {} }\n# edited after tq allow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Deps{Cwd: ws, Env: func(string) (string, bool) { return "", false },
		RunGitIn: func(string, ...string) (string, error) { return "", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	r := RunForCwd(cfg, d)
	if r.Result.Workspace != nil || r.Result.Untrusted == nil || r.Result.Untrusted.Name != "acme" {
		t.Fatalf("expected structural Untrusted, got %+v", r.Result)
	}
	if !contains(r.Codes(), "untrusted") {
		t.Fatalf("codes %v", r.Codes())
	}
}

// Fix round 2, finding 9: env-drift and hook-missing describe the same stale
// shell, so hook-missing is suppressed once env-drift fired for the same cwd.
func TestRunForCwd_HookMissingSuppressedByEnvDrift(t *testing.T) {
	cfg, ws := setupTrustedWorkspace(t, "acme", "dev@acme.com")
	d := Deps{Cwd: ws,
		Env:      func(string) (string, bool) { return "", false },
		RunGitIn: func(string, ...string) (string, error) { return "dev@acme.com", nil },
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil }}
	r := RunForCwd(cfg, d)
	if !contains(r.Codes(), "env-drift") {
		t.Fatalf("expected env-drift: %v", r.Codes())
	}
	if contains(r.Codes(), "hook-missing") {
		t.Fatalf("hook-missing must not double-report alongside env-drift: %v", r.Codes())
	}
}
