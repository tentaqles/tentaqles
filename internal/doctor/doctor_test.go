package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
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
	base := testutil.TempDir(t)
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
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	fs := Run(cfg, deps(map[string]string{}, filepath.Join(base, "x"), map[string]string{"user.useConfigOnly": "true"}))
	if !has(fs, "hook-missing") {
		t.Fatalf("%+v", fs)
	}
}

func TestRun_GitMissing_SkipsGitConfigChecks(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
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
	base := testutil.TempDir(t)
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

func TestRun_BundleDrift(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@acme.com }\nidentities: { claude: {} }\nclaude: { bundle: { mcp: [github] } }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	trust.Allow(h)

	cat := &bundle.Catalog{
		Marketplaces: map[string]bundle.Marketplace{},
		Skills:       map[string]bundle.Skill{},
		MCP: map[string]bundle.MCPServer{
			"github": {"command": "gh-mcp"},
		},
	}
	if err := cat.Save(); err != nil {
		t.Fatal(err)
	}

	fs := Run(cfg, deps(map[string]string{}, root, map[string]string{"user.useConfigOnly": "true"}))
	if !has(fs, "bundle-drift") {
		t.Fatalf("expected bundle-drift, got %+v", fs)
	}
}

// --- migration drift codes (tq migrate) -----------------------------------

func findingsWith(fs []Finding, code string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

func isolatedHome(t *testing.T) string {
	t.Helper()
	h := testutil.TempDir(t)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", h)
	}
	t.Setenv("HOME", h)
	t.Setenv("TQ_HOME", filepath.Join(h, ".tentaqles"))
	return h
}

func TestRun_GlobalEmailSet(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)

	git := map[string]string{"user.useConfigOnly": "true", "user.email": "me@corp.com"}
	fs := Run(cfg, deps(nil, base, git))
	got := findingsWith(fs, "global-email-set")
	if len(got) != 1 {
		t.Fatalf("expected one global-email-set, got %+v", fs)
	}
	if got[0].Level != "error" {
		t.Fatalf("global-email-set must be an error: %+v", got[0])
	}
	if !strings.Contains(got[0].Msg, "me@corp.com") {
		t.Fatalf("message must name the address: %q", got[0].Msg)
	}

	delete(git, "user.email")
	if fs := Run(cfg, deps(nil, base, git)); has(fs, "global-email-set") {
		t.Fatalf("no global user.email must be clean: %+v", fs)
	}
}

func TestRun_IncludeIfUnmanagedAndIncludeOrphan(t *testing.T) {
	home := isolatedHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: {}\n"), 0o600)
	h, _ := trust.HashFile(mp)
	trust.Allow(h)
	os.WriteFile(gitcfg.WorkspaceFile(root), []byte("# managed by tq\n[user]\n\temail = a@b\n"), 0o644)
	// tq's own include file exists, so it must not be reported as an orphan.
	os.WriteFile(gitcfg.IncludeFile(), []byte("# managed by tq\n"), 0o644)

	missing := filepath.ToSlash(filepath.Join(home, "gone", ".gitconfig-tentaqles"))
	git := map[string]string{
		"user.useConfigOnly": "true",
		"include.path":       filepath.ToSlash(gitcfg.IncludeFile()) + "\n" + missing,
		// git --null form: <origin>\0<key>\n<value>\0
		`^includeif\.`: "file:" + filepath.Join(home, ".gitconfig") + "\x00" +
			"includeif.gitdir:" + filepath.ToSlash(root) + "/.path\n" +
			filepath.ToSlash(gitcfg.WorkspaceFile(root)) + "\x00",
	}
	fs := Run(cfg, deps(nil, root, git))

	un := findingsWith(fs, "includeif-unmanaged")
	if len(un) != 1 {
		t.Fatalf("expected one includeif-unmanaged, got %+v", fs)
	}
	if un[0].Workspace != "acme" || un[0].Level != "warn" {
		t.Fatalf("%+v", un[0])
	}
	if !strings.Contains(un[0].Msg, "gitdir:"+filepath.ToSlash(root)+"/") {
		t.Fatalf("message must name the condition: %q", un[0].Msg)
	}

	orph := findingsWith(fs, "include-orphan")
	if len(orph) != 1 {
		t.Fatalf("expected exactly one include-orphan (only the missing file), got %+v", orph)
	}
	if !strings.Contains(orph[0].Msg, missing) {
		t.Fatalf("message must name the missing path: %q", orph[0].Msg)
	}
}

func TestRun_LegacyActive(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	git := map[string]string{"user.useConfigOnly": "true"}

	fs := Run(cfg, deps(map[string]string{"TQ_ENABLED": "0"}, base, git))
	got := findingsWith(fs, "legacy-active")
	if len(got) != 1 || got[0].Level != "warn" {
		t.Fatalf("expected one warn legacy-active, got %+v", fs)
	}
	if fs := Run(cfg, deps(map[string]string{"TQ_ENABLED": "1"}, base, git)); has(fs, "legacy-active") {
		t.Fatalf("TQ_ENABLED=1 is not legacy: %+v", fs)
	}
	if fs := Run(cfg, deps(nil, base, git)); has(fs, "legacy-active") {
		t.Fatalf("unset TQ_ENABLED is not legacy: %+v", fs)
	}
}

// mkLink makes link point at target: a directory junction on Windows (no
// elevation needed), a symlink elsewhere.
func mkLink(t *testing.T, link, target string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(link), 0o755)
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skipf("mklink /J unavailable: %v (%s)", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func TestRun_IdentityDirLinked(t *testing.T) {
	isolatedHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	cfg.AddBase(base)
	root := filepath.Join(base, "acme")
	os.MkdirAll(root, 0o755)
	mp := filepath.Join(root, manifest.FileName)
	os.WriteFile(mp, []byte("schema: tentaqles-client-v2\nclient: acme\ngit: { email: a@b }\nidentities: { claude: {}, gh: {} }\n"), 0o600)
	h, _ := trust.HashFile(mp)
	trust.Allow(h)
	os.WriteFile(gitcfg.WorkspaceFile(root), []byte("# managed by tq\n[user]\n\temail = a@b\n"), 0o644)

	// gh is a real directory; claude is a link to an external one.
	os.MkdirAll(paths.IdentityDir("acme", "gh"), 0o755)
	target := filepath.Join(testutil.TempDir(t), "claude-external")
	os.MkdirAll(target, 0o755)
	link := paths.IdentityDir("acme", "claude")
	mkLink(t, link, target)

	fs := Run(cfg, deps(nil, root, map[string]string{"user.useConfigOnly": "true"}))
	got := findingsWith(fs, "identity-dir-linked")
	if len(got) != 1 {
		t.Fatalf("expected exactly one identity-dir-linked (claude), got %+v", got)
	}
	if got[0].Level != "warn" || got[0].Workspace != "acme" {
		t.Fatalf("%+v", got[0])
	}
	if !strings.Contains(got[0].Msg, link) {
		t.Fatalf("message must name the identity dir: %q", got[0].Msg)
	}
	if got[0].Fix == "" {
		t.Fatalf("identity-dir-linked needs a fix hint: %+v", got[0])
	}
}
