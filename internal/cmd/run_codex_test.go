package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// codexWS is a trusted acme workspace with a codex identity, under an
// isolated home.
func codexWS(t *testing.T) *resolve.Workspace {
	t.Helper()
	isolateHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "elsewhere"))
	m := &manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: "acme",
		Identities: map[string]manifest.Identity{"codex": {}, "gh": {}},
	}
	w := &resolve.Workspace{Name: "acme", Hash: "hash-codex", Root: filepath.Join(t.TempDir(), "acme"), Manifest: m}
	if err := trust.Allow(w.Hash); err != nil {
		t.Fatal(err)
	}
	return w
}

// withCodexMode makes the mode resolver return preset name (the WS1 engine
// is a stub on this branch), for every workspace and cwd, or only below sub.
func withCodexMode(t *testing.T, name string, under string) {
	t.Helper()
	tab, ok := guardcfg.Preset(name)
	if !ok {
		t.Fatalf("no preset %s", name)
	}
	prev := codexModeAt
	codexModeAt = func(ws *resolve.Workspace, cwd string) (string, func(*policy.Rule) policy.Action) {
		if under != "" && !strings.HasPrefix(filepath.Clean(cwd), filepath.Clean(under)) {
			return prev(ws, cwd)
		}
		return name, func(r *policy.Rule) policy.Action {
			c := guardcfg.CategoryOf(r)
			if c == "" || guardcfg.Locked(c) {
				return r.Action
			}
			return tab.Get(c).Apply(r.Action)
		}
	}
	t.Cleanup(func() { codexModeAt = prev })
}

func TestCodexArgsFromMode(t *testing.T) {
	cases := []struct {
		name, mode, sandbox string
		args                []string
		want                string
	}{
		{"default", "", "", []string{"hi"}, "-a on-request -s workspace-write hi"},
		{"default keeps exec", "", "", []string{"exec", "hi"}, "-a on-request -s workspace-write exec hi"},
		{"strict", "strict", "", []string{"hi"}, "-a on-request -s workspace-write hi"},
		{"relaxed", "relaxed", "", []string{"hi"}, "-a never -s workspace-write hi"},
		{"relaxed read-only", "relaxed", "read-only", []string{"hi"}, "-a never -s read-only hi"},
		{"relaxed full access", "relaxed", "danger-full-access", []string{"hi"}, "--dangerously-bypass-approvals-and-sandbox hi"},
		{"user approval wins", "relaxed", "", []string{"-a", "on-request", "hi"}, "-s workspace-write -a on-request hi"},
		{"user yolo wins", "", "", []string{"--yolo"}, "--yolo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := codexWS(t)
			if tc.sandbox != "" {
				w.Manifest.Guard.Codex = &manifest.GuardCodex{Sandbox: tc.sandbox}
			}
			if tc.mode != "" {
				withCodexMode(t, tc.mode, "")
			}
			got := codexArgs(w, w.Root, tc.args)
			if strings.Join(got, " ") != tc.want {
				t.Fatalf("got %q want %q", strings.Join(got, " "), tc.want)
			}
		})
	}
}

// Per-project modes travel in the launch flags, which follow the cwd.
func TestCodexArgsFollowCwd(t *testing.T) {
	w := codexWS(t)
	withCodexMode(t, "relaxed", filepath.Join(w.Root, "demo-todo"))
	if got := strings.Join(codexArgs(w, filepath.Join(w.Root, "demo-todo", "src"), []string{"hi"}), " "); got != "-a never -s workspace-write hi" {
		t.Fatalf("project: %q", got)
	}
	if got := strings.Join(codexArgs(w, w.Root, []string{"hi"}), " "); got != "-a on-request -s workspace-write hi" {
		t.Fatalf("root: %q", got)
	}
}

func TestCodexArgsNoCodexIdentity(t *testing.T) {
	w := codexWS(t)
	delete(w.Manifest.Identities, "codex")
	if got := codexArgs(w, w.Root, []string{"hi"}); strings.Join(got, " ") != "hi" {
		t.Fatalf("got %v", got)
	}
}

// The unattended profile is gone: a tq-written profile file is never
// passed any more, even for an old projects-only setup.
func TestCodexArgsNoLegacyProfile(t *testing.T) {
	w := codexWS(t)
	w.Manifest.Guard.Unattended = &manifest.Unattended{Projects: []string{"demo-todo"}}
	if got := strings.Join(codexArgs(w, filepath.Join(w.Root, "demo-todo"), []string{"hi"}), " "); strings.Contains(got, "--profile") {
		t.Fatalf("got %q", got)
	}
}

// An untrusted workspace compiles to default, whatever its manifest says.
func TestCodexPlanUntrustedIsDefault(t *testing.T) {
	w := codexWS(t)
	withCodexMode(t, "relaxed", "")
	if err := trust.Deny(w.Hash); err != nil {
		t.Fatal(err)
	}
	if p := codexPlan(w, w.Root); p.Mode != guardcfg.ModeDefault || p.Approval != "on-request" {
		t.Fatalf("untrusted: %+v", p)
	}
}

// guard.disable and guard.actions feed the compiled rules; a locked rule
// keeps its deny whatever they say.
func TestCodexRuleSetHonoursActions(t *testing.T) {
	w := codexWS(t)
	w.Manifest.Guard.Disable = []string{"tq/iac-destroy", "tq/env-file-shell"}
	w.Manifest.Guard.Actions = map[string]policy.Action{"tq/iac-apply": policy.Deny, "tq/trust-store-write": policy.Ask}
	got := map[string]policy.Action{}
	for _, r := range codexRuleSet(w) {
		got[r.ID] = r.Action
	}
	if _, ok := got["tq/iac-destroy"]; ok {
		t.Error("a disabled rule is left out")
	}
	if got["tq/env-file-shell"] != policy.Deny {
		t.Error("a locked rule cannot be disabled")
	}
	if got["tq/iac-apply"] != policy.Deny {
		t.Error("guard.actions changes the rule's own action")
	}
	if got["tq/trust-store-write"] != policy.Deny {
		t.Error("guard.actions cannot loosen a locked rule")
	}
}

func renderOut(t *testing.T, w *resolve.Workspace, dry bool) string {
	t.Helper()
	cmd := &cobra.Command{}
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	renderCodex(cmd, w, dry)
	return out.String()
}

func TestRenderCodexWritesModeFiles(t *testing.T) {
	w := codexWS(t)
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	userCfg := "model = \"gpt-5\"\n"
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(userCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	if out := renderOut(t, w, true); !strings.Contains(out, "would change") {
		t.Fatalf("dry run: %q", out)
	}
	if b, _ := os.ReadFile(cfg); string(b) != userCfg {
		t.Fatal("dry run wrote the file")
	}

	if out := renderOut(t, w, false); !strings.Contains(out, "updated") {
		t.Fatalf("render: %q", out)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), `approval_policy = "on-request"`) || !strings.HasSuffix(string(b), userCfg) {
		t.Fatalf("config.toml:\n%s", b)
	}
	rules, err := codexcfg.ReadRules(dir)
	if err != nil {
		t.Fatal(err)
	}
	dec := map[string]string{}
	for _, r := range rules {
		dec[r.ID] = r.Decision
	}
	if dec["tq/gh-repo-delete"] != codexcfg.DecisionForbidden || dec["tq/iac-apply"] != codexcfg.DecisionPrompt {
		t.Fatalf("default rules: %v", dec)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("hooks.json must carry tq's PreToolUse entry")
	}
	if out := renderOut(t, w, false); out != "" {
		t.Fatalf("an up-to-date render is silent: %q", out)
	}

	withCodexMode(t, "relaxed", "")
	renderOut(t, w, false)
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `approval_policy = "never"`) {
		t.Fatalf("relaxed:\n%s", b)
	}
}

func TestRenderCodexReportsConflicts(t *testing.T) {
	w := codexWS(t)
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("sandbox_mode = \"read-only\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := renderOut(t, w, false)
	if !strings.Contains(out, "not changed") || !strings.Contains(out, "sandbox_mode") {
		t.Fatalf("conflict: %q", out)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("the hook is written despite the config conflict")
	}
}

func TestRenderCodexNoopWithoutCodexIdentity(t *testing.T) {
	w := codexWS(t)
	delete(w.Manifest.Identities, "codex")
	if out := renderOut(t, w, false); out != "" {
		t.Fatalf("expected silence, got %q", out)
	}
	if _, err := os.Stat(paths.IdentityDir("acme", "codex")); !os.IsNotExist(err) {
		t.Fatal("must not create a codex identity dir")
	}
}

// The files tq writes into CODEX_HOME hold the persistent mode: a render run
// from a launched shell never sees the one-launch override (review r0,
// Minor 3). The launch flags still do.
func TestRenderCodexIgnoresLaunchOverride(t *testing.T) {
	w := codexWS(t)
	if err := os.MkdirAll(paths.IdentityDir("acme", "codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(guardcfg.EnvMode, guardcfg.ModeRelaxed)
	t.Setenv("TENTAQLES_HEADLESS", "1")
	var seen []string
	prev := codexModeAt
	codexModeAt = func(ws *resolve.Workspace, cwd string) (string, func(*policy.Rule) policy.Action) {
		seen = append(seen, os.Getenv(guardcfg.EnvMode)+"|"+os.Getenv("TENTAQLES_HEADLESS"))
		return prev(ws, cwd)
	}
	t.Cleanup(func() { codexModeAt = prev })

	renderOut(t, w, false)
	if len(seen) == 0 || seen[len(seen)-1] != "|" {
		t.Fatalf("render resolved with the launch env: %q", seen)
	}
	if os.Getenv(guardcfg.EnvMode) != guardcfg.ModeRelaxed || os.Getenv("TENTAQLES_HEADLESS") != "1" {
		t.Fatal("the launch env must be restored after a render")
	}
	seen = nil
	codexArgs(w, w.Root, []string{"hi"})
	if len(seen) != 1 || seen[0] != guardcfg.ModeRelaxed+"|1" {
		t.Fatalf("launch flags resolve with the launch env: %q", seen)
	}
}

// A workspace that stops isolating codex gets tq's files removed from the
// old identity dir; the user's own config stays (review r0, Minor 5).
func TestRenderCodexRemovesWhenNoLongerIsolated(t *testing.T) {
	w := codexWS(t)
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.toml")
	userCfg := "model = \"gpt-5\"\n"
	if err := os.WriteFile(cfg, []byte(userCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	renderOut(t, w, false)
	if _, err := codexcfg.ReadRules(dir); err != nil {
		t.Fatal(err)
	}

	delete(w.Manifest.Identities, "codex")
	if out := renderOut(t, w, true); !strings.Contains(out, "would change") {
		t.Fatalf("dry run of the removal: %q", out)
	}
	if _, err := codexcfg.ReadRules(dir); err != nil {
		t.Fatal("a dry run removes nothing")
	}
	if out := renderOut(t, w, false); !strings.Contains(out, "updated") {
		t.Fatalf("removal: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(codexcfg.RulesFile))); !os.IsNotExist(err) {
		t.Fatalf("tq.rules must be gone: %v", err)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); ok {
		t.Fatal("tq's hook entry must be gone")
	}
	if b, _ := os.ReadFile(cfg); string(b) != userCfg {
		t.Fatalf("config.toml keeps the user's part only:\n%s", b)
	}
	if out := renderOut(t, w, false); out != "" {
		t.Fatalf("nothing left to remove: %q", out)
	}

	// No identity dir at all: nothing to do.
	w2 := codexWS(t)
	delete(w2.Manifest.Identities, "codex")
	w2.Name = "other"
	if out := renderOut(t, w2, false); out != "" {
		t.Fatalf("no identity dir: %q", out)
	}
	if _, err := os.Stat(paths.IdentityDir("other", "codex")); !os.IsNotExist(err) {
		t.Fatal("the removal path must not create the identity dir")
	}
}
