package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

func codexWS(t *testing.T, u *manifest.Unattended, bypass bool) *resolve.Workspace {
	t.Helper()
	isolateHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "elsewhere"))
	m := &manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: "acme",
		Identities: map[string]manifest.Identity{"codex": {}, "gh": {}},
	}
	m.Guard.Unattended = u
	w := &resolve.Workspace{Name: "acme", Hash: "hash-codex", Root: filepath.Join(t.TempDir(), "acme"), Manifest: m}
	if bypass {
		if err := trust.AllowBypass(w.Hash); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func TestCodexArgs(t *testing.T) {
	proj := &manifest.Unattended{Projects: []string{"demo-todo"}}
	cases := []struct {
		name   string
		u      *manifest.Unattended
		bypass bool
		sub    string
		args   []string
		want   string
	}{
		{"covered project with bypass", proj, true, "demo-todo", []string{"exec", "hi"}, "--profile tq-unattended exec hi"},
		{"nested folder of a covered project", proj, true, "demo-todo/src", []string{"hi"}, "--profile tq-unattended hi"},
		{"no bypass trust", proj, false, "demo-todo", []string{"hi"}, "hi"},
		{"workspace root is not covered", proj, true, "", []string{"hi"}, "hi"},
		{"explicit -p wins", proj, true, "demo-todo", []string{"-p", "x"}, "-p x"},
		{"explicit --profile= wins", proj, true, "demo-todo", []string{"--profile=x"}, "--profile=x"},
		{"workspace-wide uses top-level keys", &manifest.Unattended{Workspace: true}, true, "demo-todo", []string{"hi"}, "hi"},
		{"not configured", nil, true, "demo-todo", []string{"hi"}, "hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := codexWS(t, tc.u, tc.bypass)
			got := codexArgs(w, filepath.Join(w.Root, filepath.FromSlash(tc.sub)), tc.args)
			if strings.Join(got, " ") != tc.want {
				t.Fatalf("got %q want %q", strings.Join(got, " "), tc.want)
			}
		})
	}
}

func TestRenderCodexWritesAndRestores(t *testing.T) {
	w := codexWS(t, &manifest.Unattended{Workspace: true}, true)
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	userCfg := "model = \"gpt-5\"\n"
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(userCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	renderCodex(cmd, w, true)
	if !strings.Contains(out.String(), "would change") {
		t.Fatalf("dry run: %q", out.String())
	}
	if b, _ := os.ReadFile(cfg); string(b) != userCfg {
		t.Fatal("dry run wrote the file")
	}

	renderCodex(cmd, w, false)
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), "approval_policy = \"never\"") || !strings.HasSuffix(string(b), userCfg) {
		t.Fatalf("config.toml:\n%s", b)
	}

	w.Manifest.Guard.Unattended = nil
	renderCodex(cmd, w, false)
	if b, _ := os.ReadFile(cfg); string(b) != userCfg {
		t.Fatalf("off must restore the user file:\n%q", b)
	}
}

func TestRenderCodexNoopWithoutCodexIdentity(t *testing.T) {
	w := codexWS(t, &manifest.Unattended{Workspace: true}, true)
	delete(w.Manifest.Identities, "codex")
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	renderCodex(cmd, w, false)
	if out.Len() != 0 {
		t.Fatalf("expected silence, got %q", out.String())
	}
	if _, err := os.Stat(paths.IdentityDir("acme", "codex")); !os.IsNotExist(err) {
		t.Fatal("must not create a codex identity dir")
	}
}
