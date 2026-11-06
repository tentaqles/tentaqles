package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The PRD v2 §6.1 reference scenario at layers 1, 2 and 3 (ADR-0008), each in its own test:
// identity company-x declares Azure as its only cloud. Layer 4, the hook, is
// TestPreToolUse_ScopeReferenceScenario (ADR-0006).
//
//   - layer 1 (advisory): the session-start preamble states the scope;
//   - layer 2 (PATH): aws is not on the identity PATH tq builds, az is;
//   - layer 3 (harness config): tq settings render leaves no AWS MCP server in the identity's
//     Claude and Codex homes, even one configured there before.

// scenarioGlobalPATH lays out a machine's global PATH with fakes: the AWS CLI's install
// folder, the Azure CLI's, and returns it in the OS spelling.
func scenarioGlobalPATH(t *testing.T) (path, awsDir, azDir string) {
	t.Helper()
	base := testutil.TempDir(t)
	awsDir = filepath.Join(base, "Amazon", "AWSCLIV2")
	azDir = filepath.Join(base, "Azure", "CLI2", "wbin")
	exe := func(dir, name string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			name += map[bool]string{true: ".cmd", false: ".exe"}[name == "az"]
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe(awsDir, "aws")
	exe(awsDir, "aws_completer")
	exe(azDir, "az")
	return strings.Join([]string{awsDir, azDir}, string(os.PathListSeparator)), awsDir, azDir
}

func envJSON(t *testing.T, sh string) (set map[string]string, unset []string) {
	t.Helper()
	var v struct {
		Set   map[string]string `json:"set"`
		Unset []string          `json:"unset"`
	}
	if err := json.Unmarshal([]byte(runEnv(t, "env", "--shell", sh, "--json")), &v); err != nil {
		t.Fatal(err)
	}
	return v.Set, v.Unset
}

// TestEnv_ScopeReferenceScenario_PATH is §6.1 step 2: on the PATH tq env builds for
// company-x, a shell call to aws finds nothing (command-not-found) while az still resolves;
// the AWS variables are unset. tq run from outside the folder resolves on the same PATH.
func TestEnv_ScopeReferenceScenario_PATH(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	global, awsDir, azDir := scenarioGlobalPATH(t)
	t.Setenv("PATH", global)
	t.Setenv("AWS_PROFILE", "prod")
	chdir(t, root)

	sh := "bash"
	if runtime.GOOS == "windows" {
		sh = "pwsh"
	}
	set, unset := envJSON(t, sh)
	idPath, ok := set["PATH"]
	if !ok {
		t.Fatalf("tq env sets no PATH: %v", set)
	}
	env := []string{"PATH=" + idPath, "PATHEXT=.COM;.EXE;.BAT;.CMD"}
	if p, err := envplan.LookPath("aws", env); !errors.Is(err, envplan.ErrNotInPath) {
		t.Fatalf("aws resolves on company-x's PATH: %q %v", p, err)
	}
	if p, err := envplan.LookPath("az", env); err != nil || filepath.Dir(p) != azDir {
		t.Fatalf("az does not resolve from its folder: %q %v", p, err)
	}
	if strings.Contains(idPath, awsDir) {
		t.Fatalf("identity PATH holds the AWS CLI folder: %q", idPath)
	}
	if !hasString(unset, "AWS_PROFILE") {
		t.Fatalf("AWS_PROFILE not unset: %v", unset)
	}

	// Git Bash on Windows: the hook passes the shell's own spelling and tq answers in it.
	if runtime.GOOS == "windows" {
		syn := envplan.MSYSSyntax("", "")
		t.Setenv(envplan.ShellPathVar, syn.FromOS(awsDir)+":"+syn.FromOS(azDir))
		bset, _ := envJSON(t, "bash")
		if got, want := bset["PATH"], syn.FromOS(azDir); got != want {
			t.Fatalf("Git Bash PATH = %q, want %q", got, want)
		}
		t.Setenv(envplan.ShellPathVar, "")
	}

	// tq run . -- aws and tq run company-x -- aws resolve on the identity PATH: not found,
	// and nothing is launched. az is launched from its folder.
	calls := fakeExec(t)
	for _, target := range []string{".", "company-x"} {
		err := runCmdErr(t, "run", target, "--", "aws", "ec2", "run-instances")
		if err == nil || !strings.Contains(err.Error(), "command not found on the PATH of identity company-x") {
			t.Fatalf("tq run %s -- aws: %v", target, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("aws was launched: %+v", *calls)
	}
	if err := runCmdErr(t, "run", ".", "--", "az", "vm", "list"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || filepath.Dir((*calls)[0].name) != azDir || strings.Contains((*calls)[0].env["PATH"], awsDir) {
		t.Fatalf("az launch = %+v", *calls)
	}
	if _, ok := (*calls)[0].env["AWS_PROFILE"]; ok {
		t.Fatal("AWS_PROFILE reaches the launched command")
	}
}

func hasString(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func runCmdErr(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRoot()
	root.SetOut(&strings.Builder{})
	root.SetErr(&strings.Builder{})
	root.SetArgs(args)
	return root.Execute()
}

// Leaving company-x restores PATH and the unset variables exactly.
func TestEnv_ScopeLayerRestore(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	global, _, _ := scenarioGlobalPATH(t)
	t.Setenv("PATH", global)
	t.Setenv("AWS_PROFILE", "prod")
	sh := "bash"
	if runtime.GOOS == "windows" {
		sh = "pwsh"
	}
	chdir(t, root)
	set, unset := envJSON(t, sh)
	// Apply what the shell would, then leave for a neutral folder.
	for _, k := range unset {
		_ = os.Unsetenv(k)
	}
	for k, v := range set {
		t.Setenv(k, v)
	}
	chdir(t, testutil.TempDir(t))
	back, _ := envJSON(t, sh)
	if back["PATH"] != global || back["AWS_PROFILE"] != "prod" {
		t.Fatalf("leaving: PATH %q AWS_PROFILE %q", back["PATH"], back["AWS_PROFILE"])
	}
}

// TestSettingsRender_ScopeReferenceScenario_MCP is §6.1 step 3: an AWS MCP server configured
// in company-x's Claude home (user and project scope) and Codex home is gone after tq
// settings render; the Azure server and an unrelated one stay, the removed ones are kept
// beside the config, and the PATH mask is written.
func TestSettingsRender_ScopeReferenceScenario_MCP(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	claudeDir := paths.IdentityDir("company-x", "claude")
	codexDir := paths.IdentityDir("company-x", "codex")
	write := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rootKey, _ := json.Marshal(filepath.ToSlash(root))
	claudeJSON := `{"mcpServers":{"aws-api":{"command":"uvx","args":["awslabs.aws-api-mcp-server@latest"]},` +
		`"azure":{"command":"npx","args":["-y","@azure/mcp@latest"]},"mydb":{"command":"uvx","args":["postgres-mcp"]}},` +
		`"projects":{` + string(rootKey) + `:{"mcpServers":{"aws":{"type":"http","url":"https://knowledge-mcp.global.api.aws"}}}}}`
	write(filepath.Join(claudeDir, ".claude.json"), claudeJSON)
	write(filepath.Join(codexDir, "config.toml"), "[mcp_servers.aws]\ncommand = \"uvx\"\n\n[mcp_servers.azure]\ncommand = \"npx\"\n")

	out := runEnv(t, "settings", "render", "company-x")
	if !strings.Contains(out, "removed MCP server aws-api") || !strings.Contains(out, "PATH mask written") {
		t.Fatalf("render output:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(claudeDir, ".claude.json"))
	var cj struct {
		MCP      map[string]any `json:"mcpServers"`
		Projects map[string]struct {
			MCP map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &cj); err != nil {
		t.Fatal(err)
	}
	for name := range cj.MCP {
		if strings.Contains(name, "aws") {
			t.Fatalf("an AWS MCP server is still registered: %s", name)
		}
	}
	if cj.MCP["azure"] == nil || cj.MCP["mydb"] == nil {
		t.Fatalf("in-scope or unrelated servers removed: %v", cj.MCP)
	}
	for p, pr := range cj.Projects {
		if len(pr.MCP) != 0 {
			t.Fatalf("project %s keeps %v", p, pr.MCP)
		}
	}
	toml, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if strings.Contains(string(toml), "mcp_servers.aws") || !strings.Contains(string(toml), "[mcp_servers.azure]") {
		t.Fatalf("codex config.toml:\n%s", toml)
	}
	for _, d := range []string{claudeDir, codexDir} {
		if _, err := os.Stat(filepath.Join(d, mcpscope.RemovedFile)); err != nil {
			t.Fatalf("removed entries not kept in %s: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(envplan.MaskDir("company-x"), "aws")); err != nil {
		t.Fatalf("PATH mask not written: %v", err)
	}
	// Trust survives the render (the MCP lists and the mask are not covered).
	if got := runEnv(t, "settings", "render", "company-x"); strings.Contains(got, "skipped") || strings.Contains(got, "removed MCP") {
		t.Fatalf("second render:\n%s", got)
	}
}

// TestSessionStart_ScopeLine is §6.1 step 1 (advisory): the preamble states the scope in one
// line.
func TestSessionStart_ScopeLine(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	_, out, _ := runHook(t, []string{"claude-hook", "session-start"}, `{"cwd":`+jsonPath(t, root)+`}`)
	want := "Scope: az, azure-devops, claude. Identity company-x may use these providers only"
	if n := strings.Count(out, "\nScope: "); n != 1 || !strings.Contains(out, want) {
		t.Fatalf("preamble lacks the scope line:\n%s", out)
	}
}
