package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"github.com/tentaqles/tentaqles/cli/internal/registry"
	"github.com/tentaqles/tentaqles/cli/internal/trust"
)

func has(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func deps(env map[string]string, cwd string, git map[string]string) Deps {
	return Deps{
		Env: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Cwd: cwd,
		RunGit: func(args ...string) (string, error) {
			return git[args[len(args)-1]], nil
		},
		// git is present; every other CLI is missing.
		LookPath: func(n string) (string, error) {
			if n == "git" {
				return "/usr/bin/git", nil
			}
			return "", os.ErrNotExist
		},
	}
}

func TestRun_NoBases(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	fs := Run(&registry.Config{}, deps(nil, t.TempDir(), nil))
	if !has(fs, "no-bases") {
		t.Fatalf("%+v", fs)
	}
}

func TestRun_UntrustedAndDriftAndBypassCloud(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {}, az: {} }\nclaude: { permission_mode: bypass }\n"), 0o600)

	fs := Run(cfg, deps(map[string]string{"TQ_WS": "other"}, root, nil))
	for _, code := range []string{"untrusted", "bypass-cloud", "git-include-missing", "git-useconfigonly"} {
		if !has(fs, code) {
			t.Fatalf("missing %s in %+v", code, fs)
		}
	}
	h, _ := trust.HashFile(mp)
	trust.Allow(h)
	fs = Run(cfg, deps(map[string]string{"TQ_WS": "other"}, root, nil))
	if has(fs, "untrusted") || !has(fs, "env-drift") {
		t.Fatalf("%+v", fs)
	}
	if Exit(fs) != 1 {
		t.Fatal("errors must exit 1")
	}
}

func TestRun_HookMissing(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	cfg.AddBase(base)
	fs := Run(cfg, deps(map[string]string{}, filepath.Join(base, "x"), map[string]string{"user.useConfigOnly": "true"}))
	if !has(fs, "hook-missing") {
		t.Fatalf("%+v", fs)
	}
}

func TestRun_GitMissing_SkipsGitConfigChecks(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	cfg.AddBase(base)
	d := deps(map[string]string{}, base, nil)
	d.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	fs := Run(cfg, d)
	if !has(fs, "git-missing") {
		t.Fatalf("expected git-missing: %+v", fs)
	}
	if has(fs, "git-include-missing") || has(fs, "git-useconfigonly") {
		t.Fatalf("git config checks must be skipped when git is absent: %+v", fs)
	}
}

func TestRun_GitWorkspaceFileTampered(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := t.TempDir()
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {} }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	trust.Allow(h)
	wf := filepath.Join(root, ".gitconfig-tentaqles")

	// A well-formed tq file is clean.
	os.WriteFile(wf, []byte("# managed by tq\n[user]\n\tname = A\n\temail = a@b\n"), 0o644)
	if fs := Run(cfg, deps(nil, root, nil)); has(fs, "git-ws-file-tampered") {
		t.Fatalf("clean file flagged: %+v", fs)
	}

	// An extra section is tampering.
	os.WriteFile(wf, []byte("# managed by tq\n[user]\n\temail = a@b\n[core]\n\tsshCommand = evil\n"), 0o644)
	if fs := Run(cfg, deps(nil, root, nil)); !has(fs, "git-ws-file-tampered") {
		t.Fatalf("expected git-ws-file-tampered: %+v", fs)
	}

	// A missing header is tampering.
	os.WriteFile(wf, []byte("[user]\n\temail = a@b\n"), 0o644)
	if fs := Run(cfg, deps(nil, root, nil)); !has(fs, "git-ws-file-tampered") {
		t.Fatalf("expected git-ws-file-tampered for missing header: %+v", fs)
	}
}
