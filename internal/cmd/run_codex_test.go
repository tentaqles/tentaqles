package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/codexcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
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
	if err := trust.Allow(w.Name, w.Hash); err != nil {
		t.Fatal(err)
	}
	return w
}

// The launcher injects Codex's full-bypass flag (PRD v2 §7.2); a flag the
// developer typed about asking or the sandbox passes through as typed.
func TestCodexArgsBypass(t *testing.T) {
	w := codexWS(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"hi"}, "--dangerously-bypass-approvals-and-sandbox hi"},
		{[]string{"exec", "hi"}, "--dangerously-bypass-approvals-and-sandbox exec hi"},
		{[]string{"-a", "never", "hi"}, "-a never hi"},
		{[]string{"-s", "read-only"}, "-s read-only"},
		{[]string{"--yolo"}, "--yolo"},
	} {
		if got := strings.Join(codexArgs(w, tc.args), " "); got != tc.want {
			t.Errorf("%v: got %q want %q", tc.args, got, tc.want)
		}
	}
}

func TestCodexArgsNoCodexIdentity(t *testing.T) {
	w := codexWS(t)
	delete(w.Manifest.Identities, "codex")
	if got := codexArgs(w, []string{"hi"}); strings.Join(got, " ") != "hi" {
		t.Fatalf("got %v", got)
	}
	if got := codexArgs(nil, []string{"hi"}); strings.Join(got, " ") != "hi" {
		t.Fatalf("neutral: got %v", got)
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

func TestRenderCodexWritesPinnedFiles(t *testing.T) {
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
	if b, _ := os.ReadFile(cfg); string(b) != userCfg {
		t.Fatalf("tq writes nothing into config.toml:\n%s", b)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("hooks.json must carry tq's PreToolUse entry")
	}
	if _, err := os.Stat(filepath.Join(dir, "rules", "tq.rules")); !os.IsNotExist(err) {
		t.Fatal("no rules file is written any more")
	}
	if out := renderOut(t, w, false); out != "" {
		t.Fatalf("an up-to-date render is silent: %q", out)
	}
}

// The guard-mode rules file an older tq wrote is removed on the next
// render; the user's own rules files stay.
func TestRenderCodexRemovesOldRulesFile(t *testing.T) {
	w := codexWS(t)
	dir := paths.IdentityDir("acme", "codex")
	rp := filepath.Join(dir, "rules", "tq.rules")
	if err := os.MkdirAll(filepath.Dir(rp), 0o700); err != nil {
		t.Fatal(err)
	}
	old := "# >>> managed by tq (guard mode): edit with tq mode, not by hand\nprefix_rule(pattern = [\"terraform\", \"apply\"], decision = \"prompt\", justification = \"x\")\n# <<< managed by tq (guard mode)\n"
	if err := os.WriteFile(rp, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "rules", "default.rules")
	if err := os.WriteFile(mine, []byte("prefix_rule(pattern = [\"ls\"])\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := renderOut(t, w, false); !strings.Contains(out, "tq.rules") {
		t.Fatalf("render: %q", out)
	}
	if _, err := os.Stat(rp); !os.IsNotExist(err) {
		t.Fatal("tq's old rules file must be gone")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Fatal("the user's rules file must stay")
	}
}

// TestRenderCodexRemovesOldConfigBlock: the config.toml block an older tq
// wrote is removed on the next render; a key the user set outside it stays.
func TestRenderCodexRemovesOldConfigBlock(t *testing.T) {
	w := codexWS(t)
	dir := paths.IdentityDir("acme", "codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "sandbox_mode = \"read-only\"\n"
	old := "# >>> managed by tq: edit with tq settings render, not by hand\napproval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n# <<< managed by tq\n" + mine
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := renderOut(t, w, false); !strings.Contains(out, "config.toml") || strings.Contains(out, "not changed") {
		t.Fatalf("render: %q", out)
	}
	if b, _ := os.ReadFile(cfg); string(b) != mine {
		t.Fatalf("config.toml keeps the user's key only:\n%s", b)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("the hook is written")
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

// A workspace that stops isolating codex gets tq's files removed from the
// old identity dir; the user's own config stays.
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
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("rendered")
	}

	delete(w.Manifest.Identities, "codex")
	if out := renderOut(t, w, true); !strings.Contains(out, "would change") {
		t.Fatalf("dry run of the removal: %q", out)
	}
	if _, _, ok := codexcfg.FindHook(dir, "PreToolUse"); !ok {
		t.Fatal("a dry run removes nothing")
	}
	if out := renderOut(t, w, false); !strings.Contains(out, "updated") {
		t.Fatalf("removal: %q", out)
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

// tq run starts codex with the full-bypass flag; a flag the user typed wins.
func TestLaunchCodexFlagsAreTheBypass(t *testing.T) {
	mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "codex", "exec", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.name != "codex" || strings.Join(c.args, " ") != "--dangerously-bypass-approvals-and-sandbox exec x" || c.env["TQ_WS"] != "acme" {
		t.Fatalf("codex launch: %+v", c)
	}
	calls = fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "codex", "-a", "never", "exec", "x"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; strings.Join(c.args, " ") != "-a never exec x" {
		t.Fatalf("user approval wins: %+v", c)
	}
}
