package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/tentaqles/tentaqles/internal/codexconfig"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// settings render writes a client's Codex config when codex is in its scope, and leaves it alone
// otherwise.
func TestSettingsRenderCodex(t *testing.T) {
	mkCodexWS(t, "acme", false)
	home := paths.IdentityDir("acme", "codex")
	clearAgentMarkers(t)

	out, err := runTq(t, "settings", "render", "acme")
	if err != nil || !strings.Contains(out, "acme: codex config written ("+codexconfig.ConfigPath(home)+")") {
		t.Fatalf("first render: %v\n%s", err, out)
	}
	if got, _ := codexconfig.HookTrust(home); got != codexconfig.Trusted {
		t.Fatalf("HookTrust after settings render = %v, want trusted", got)
	}
	if cfg := readFileOrEmpty(t, codexconfig.ConfigPath(home)); !strings.Contains(cfg, `developer_instructions = "Language: English (United States) (en-US). Write every report`) {
		t.Fatalf("the language line is missing:\n%s", cfg)
	}

	out, err = runTq(t, "settings", "render", "acme")
	if err != nil || !strings.Contains(out, "acme: codex config up to date (") {
		t.Fatalf("second render: %v\n%s", err, out)
	}
}

// The hook command is the quoted absolute path of tentaqles, so a path with spaces stays one word.
func TestSettingsRenderCodexQuotesTheProgramPath(t *testing.T) {
	program := filepath.Join(t.TempDir(), "my tools", "tentaqles.exe")
	if err := os.MkdirAll(filepath.Dir(program), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := hookProgramPath
	hookProgramPath = func() string { return program }
	t.Cleanup(func() { hookProgramPath = prev })

	mkCodexWS(t, "acme", false)
	home := paths.IdentityDir("acme", "codex")
	clearAgentMarkers(t)
	if out, err := runTq(t, "settings", "render", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	root := map[string]any{}
	if err := toml.Unmarshal([]byte(readFileOrEmpty(t, codexconfig.ConfigPath(home))), &root); err != nil {
		t.Fatal(err)
	}
	want := `"` + program + `" codex-hook pre-tool-use`
	if got := firstPreToolUseCommand(root); got != want {
		t.Fatalf("the hook command is %q, want %q", got, want)
	}
	if got, _ := codexconfig.HookTrust(home); got != codexconfig.Trusted {
		t.Fatalf("HookTrust = %v, want trusted", got)
	}
}

func TestCodexHookProgram(t *testing.T) {
	prev := hookProgramPath
	t.Cleanup(func() { hookProgramPath = prev })

	hookProgramPath = func() string { return "" }
	if _, err := codexHookProgram(); err == nil {
		t.Error("no tentaqles program: want an error, not a bare command")
	}
	hookProgramPath = func() string { return filepath.FromSlash("C:/50%/tentaqles.exe") }
	if _, err := codexHookProgram(); err == nil || !strings.Contains(err.Error(), "percent sign") {
		t.Errorf("a percent sign in the path: want a refusal, got %v", err)
	}
	dir := t.TempDir()
	hookProgramPath = func() string { return filepath.Join(dir, "tentaqles") }
	got, err := codexHookProgram()
	if err != nil || got != `"`+filepath.Join(dir, "tentaqles")+`"` {
		t.Errorf("codexHookProgram = %q, %v", got, err)
	}
}

// A client without codex in its scope gets no Codex config and no line about one.
func TestSettingsRenderWithoutCodexWritesNothing(t *testing.T) {
	writeClient(t, newBase(t), "acme", "git: { email: tester@example.com }\nidentities: { claude: {} }\n")
	clearAgentMarkers(t)
	out, err := runTq(t, "settings", "render", "acme")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "codex") {
		t.Errorf("a client without codex mentions it:\n%s", out)
	}
	if _, err := os.Stat(paths.IdentityDir("acme", "codex")); !os.IsNotExist(err) {
		t.Errorf("a codex folder was made for a client without codex: %v", err)
	}
}

// A dry run writes nothing, and says it did not check the Codex config.
func TestSettingsRenderCodexDryRun(t *testing.T) {
	mkCodexWS(t, "acme", false)
	clearAgentMarkers(t)
	out, err := runTq(t, "settings", "render", "--dry-run", "acme")
	if err != nil || !strings.Contains(out, "codex config not checked (dry run;") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(codexconfig.ConfigPath(paths.IdentityDir("acme", "codex"))); !os.IsNotExist(err) {
		t.Fatalf("a dry run wrote the Codex config: %v", err)
	}
}

// The language line follows the client's language.
func TestSettingsRenderCodexLanguage(t *testing.T) {
	writeClient(t, newBase(t), "acme", "git: { email: tester@example.com }\nlanguage: pt-BR\nidentities: { codex: {} }\n")
	clearAgentMarkers(t)
	if out, err := runTq(t, "settings", "render", "acme"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg := readFileOrEmpty(t, codexconfig.ConfigPath(paths.IdentityDir("acme", "codex")))
	line := (&manifest.Manifest{Language: "pt-BR"}).LanguageLine()
	if !strings.Contains(cfg, line) {
		t.Fatalf("developer_instructions lacks %q:\n%s", line, cfg)
	}
}

// firstPreToolUseCommand is the command of the first handler of the first PreToolUse group, or "".
func firstPreToolUseCommand(root map[string]any) string {
	hooks, _ := root["hooks"].(map[string]any)
	groups := asList(hooks["PreToolUse"])
	if len(groups) == 0 {
		return ""
	}
	g, _ := groups[0].(map[string]any)
	handlers := asList(g["hooks"])
	if len(handlers) == 0 {
		return ""
	}
	h, _ := handlers[0].(map[string]any)
	cmd, _ := h["command"].(string)
	return cmd
}

// asList is v as a list of tables, whichever slice type the TOML decoder used.
func asList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []map[string]any:
		out := make([]any, 0, len(x))
		for _, m := range x {
			out = append(out, m)
		}
		return out
	}
	return nil
}

func readFileOrEmpty(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}
