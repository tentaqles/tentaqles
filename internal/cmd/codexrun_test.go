package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tentaqles/tentaqles/internal/codexconfig"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// A Codex session in an identity's folder starts only with the tentaqles hooks in the Codex config it
// runs with: these tests write that config the way tentaqles settings render leaves it.

// mkCodexWS writes client name with a codex identity (no claude). With trusted, its Codex config is
// rendered, which is what settings render writes: the hooks, trusted.
func mkCodexWS(t *testing.T, name string, trusted bool) string {
	t.Helper()
	root := writeClient(t, newBase(t), name, "git: { email: tester@example.com }\nidentities: { codex: {} }\n")
	if trusted {
		renderCodexFixture(t, name)
	}
	return root
}

func renderCodexFixture(t *testing.T, name string) {
	t.Helper()
	if _, err := codexconfig.Render(paths.IdentityDir(name, "codex"), codexconfig.Managed{Command: `"/opt/tq/tentaqles"`}); err != nil {
		t.Fatal(err)
	}
}

// editCodex decodes the Codex config of client name, applies edit, and writes it back.
func editCodex(t *testing.T, name string, edit func(root map[string]any)) {
	t.Helper()
	p := codexconfig.ConfigPath(paths.IdentityDir(name, "codex"))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	root := map[string]any{}
	if err := toml.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	edit(root)
	out, err := toml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	writeTB(t, p, string(out))
}

func TestRunCodex_RefusesWithoutTheHooks(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		want  []string
		fix   string
	}{
		{"missing", func(t *testing.T) {}, []string{"the tentaqles hooks are not in"}, "tentaqles settings render acme"},
		{"disabled under features", func(t *testing.T) {
			renderCodexFixture(t, "acme")
			editCodex(t, "acme", func(root map[string]any) { root["features"] = map[string]any{"hooks": false} })
		}, []string{"turned off", "[features]"}, "tentaqles settings render acme"},
		{"state entry disabled", func(t *testing.T) {
			renderCodexFixture(t, "acme")
			editCodex(t, "acme", func(root map[string]any) {
				for _, s := range root["hooks"].(map[string]any)["state"].(map[string]any) {
					s.(map[string]any)["enabled"] = false
				}
			})
		}, []string{"turned off"}, "tentaqles settings render acme"},
		{"trust for another hash", func(t *testing.T) {
			renderCodexFixture(t, "acme")
			editCodex(t, "acme", func(root map[string]any) {
				for _, s := range root["hooks"].(map[string]any)["state"].(map[string]any) {
					s.(map[string]any)["trusted_hash"] = "sha256:" + strings.Repeat("0", 64)
				}
			})
		}, []string{"changed since Codex trusted them"}, "tentaqles settings render acme"},
		{"never trusted", func(t *testing.T) {
			renderCodexFixture(t, "acme")
			editCodex(t, "acme", func(root map[string]any) { delete(root["hooks"].(map[string]any), "state") })
		}, []string{"Codex has not trusted"}, "tentaqles settings render acme"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mkCodexWS(t, "acme", false)
			c.setup(t)
			clearAgentMarkers(t)
			calls := fakeExec(t)
			for _, args := range [][]string{{"run", "acme", "--", "codex"}, {"run", "acme", "--", "codex", "exec", "hi"}} {
				_, err := runTq(t, args...)
				if err == nil || !strings.Contains(err.Error(), "codex won't start a session for acme without the tentaqles guard: ") {
					t.Fatalf("%v: want the codex refusal, got %v", args, err)
				}
				for _, w := range c.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("%v: refusal lacks %q: %v", args, w, err)
					}
				}
				// The refusal ends with the fix that runs last: one line, or the list after "fix, in this order:".
				if !strings.HasSuffix(err.Error(), c.fix) || !strings.Contains(err.Error(), "\n  fix") {
					t.Errorf("%v: the fix is not %q: %v", args, c.fix, err)
				}
			}
			if len(*calls) != 0 {
				t.Fatalf("nothing may launch without the hooks: %+v", *calls)
			}
		})
	}
}

// A codex session that starts with the hooks trusted runs, with exactly the arguments given: the
// launcher adds no flag to codex.
func TestRunCodex_TrustedSessionStarts(t *testing.T) {
	mkCodexWS(t, "acme", true)
	clearAgentMarkers(t)
	calls := fakeExec(t)
	for _, args := range [][]string{{"exec", "hi"}, {"resume", "--last"}, {"review"}, {}} {
		*calls = nil
		full := append([]string{"run", "acme", "--", "codex"}, args...)
		if out, err := runTq(t, full...); err != nil {
			t.Fatalf("%v: %v %s", full, err, out)
		}
		if len(*calls) != 1 || prog((*calls)[0].name) != "codex" {
			t.Fatalf("%v: calls %+v", full, *calls)
		}
		if !equalStrings((*calls)[0].args, args) {
			t.Fatalf("%v: codex got %q, want exactly %q", full, (*calls)[0].args, args)
		}
		if (*calls)[0].env["CODEX_HOME"] != paths.IdentityDir("acme", "codex") {
			t.Fatalf("codex runs with CODEX_HOME=%q", (*calls)[0].env["CODEX_HOME"])
		}
	}
}

// Subcommands that run no session, and the help and version flags, start without the hooks: the fix
// line itself runs. Any other subcommand, including one this list does not know, needs them.
func TestRunCodex_OnlySessionsNeedTheHooks(t *testing.T) {
	mkCodexWS(t, "acme", false)
	clearAgentMarkers(t)
	calls := fakeExec(t)
	for _, args := range [][]string{{"login"}, {"logout"}, {"login", "status"}, {"completion", "bash"}, {"--version"}, {"-V"}, {"--help"}, {"exec", "--help"}, {"plugin", "list"}, {"features", "list"}} {
		*calls = nil
		if out, err := runTq(t, append([]string{"run", "acme", "--", "codex"}, args...)...); err != nil {
			t.Fatalf("%v: want it to start without the hooks: %v %s", args, err, out)
		}
		if len(*calls) != 1 {
			t.Fatalf("%v: calls %+v", args, *calls)
		}
	}
	for _, args := range [][]string{{"exec"}, {"app-server"}, {"app-server", "--listen", "stdio"}, {"resume"}, {"fork", "--last"}, {"review"}, {"sandbox", "macos", "--", "ls"}, {"some-new-subcommand"}, {"-m", "o3"}} {
		*calls = nil
		_, err := runTq(t, append([]string{"run", "acme", "--", "codex"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "without the tentaqles guard") || len(*calls) != 0 {
			t.Fatalf("%v: want it refused without the hooks, got %v (calls %+v)", args, err, *calls)
		}
	}
}

// An option that turns the hooks off refuses a codex session even with the hooks trusted; the same option
// on a non-session command is harmless.
func TestRunCodex_RefusesHooksOffOptions(t *testing.T) {
	mkCodexWS(t, "acme", true)
	clearAgentMarkers(t)
	calls := fakeExec(t)
	refused := [][]string{
		{"--disable", "hooks"},
		{"--disable=hooks"},
		{"--disable", "codex_hooks"},
		{"--disable=codex_hooks"},
		{"-c", "features.hooks=false"},
		{"-c=features.codex_hooks=false"},
		{"-cfeatures.hooks=false"},
		{"--config", "features.codex_hooks=false"},
		{"--config=features.hooks=false"},
		{"--config", "features={hooks=false}"},
		{"-c", "features = { codex_hooks = false, other = true }"},
		{"exec", "--disable", "hooks", "hi"},
	}
	for _, opt := range refused {
		*calls = nil
		args := append([]string{"run", "acme", "--", "codex"}, opt...)
		_, err := runTq(t, args...)
		if err == nil || !strings.Contains(err.Error(), "turns the Codex hooks off for this session") || len(*calls) != 0 {
			t.Fatalf("%v: want it refused, got %v (calls %+v)", opt, err, *calls)
		}
	}
	allowed := [][]string{
		{"--disable", "mcp"},
		{"-c", "model=\"o3\""},
		{"-c", "features.hooks=true"},
		{"-c", "features.memories=false"},
		{"-c", "model_providers.hooks=false"},
	}
	for _, opt := range allowed {
		*calls = nil
		if out, err := runTq(t, append([]string{"run", "acme", "--", "codex"}, opt...)...); err != nil {
			t.Fatalf("%v: want it allowed: %v %s", opt, err, out)
		}
	}
	// On a command that starts no session, the option is nobody's business but codex's: the subcommand
	// comes first, so it is the one the launcher reads.
	*calls = nil
	if out, err := runTq(t, "run", "acme", "--", "codex", "login", "--disable", "hooks"); err != nil || len(*calls) != 1 {
		t.Fatalf("login with the option: %v %s (calls %+v)", err, out, *calls)
	}
	// An option before the subcommand hides it, so the launcher reads a session and asks for the guard.
	*calls = nil
	if _, err := runTq(t, "run", "acme", "--", "codex", "--disable", "hooks", "login"); err == nil || len(*calls) != 0 {
		t.Fatalf("an option before login must read as a session: %v (calls %+v)", err, *calls)
	}
}

// A codex in a neutral folder is unchanged: no client, no guard.
func TestRunCodex_NeutralFolderUnchanged(t *testing.T) {
	mkCodexWS(t, "acme", false)
	chdir(t, t.TempDir())
	clearAgentMarkers(t)
	calls := fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "codex", "exec", "hi"); err != nil || len(*calls) != 1 {
		t.Fatalf("neutral folder: %v %s (calls %+v)", err, out, *calls)
	}
}

// The session-start preamble names CODEX_HOME for a codex client, and CLAUDE_CONFIG_DIR stays for claude.
func TestSessionStartNamesCodexHome(t *testing.T) {
	root := mkCodexWS(t, "acme", true)
	payload := `{"cwd":` + jsonPathLit(root) + `}`
	var out bytes.Buffer
	sessionStart(strings.NewReader(payload), &out, "CODEX_HOME")
	want := "Identity: acme · CODEX_HOME=" + paths.IdentityDir("acme", "codex") + "\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("preamble lacks %q:\n%s", want, out.String())
	}
	if strings.Contains(out.String(), "CLAUDE_CONFIG_DIR") {
		t.Fatalf("a codex preamble names CLAUDE_CONFIG_DIR:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Language: English (United States) (en-US). ") {
		t.Fatalf("the language line is missing:\n%s", out.String())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
