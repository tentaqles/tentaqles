package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/shell"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/zalando/go-keyring"
)

// Round 2 of the §6.1 scenario at layer 2 (ADR-0008): through a folder shared with other
// tools (the mask answers), with aws.exe spelled out, and through cmd.exe's real hook with a
// 3.5 KB and a 9 KB PATH; and layer 4 judging an MCP server by its config entry.

func fakeIn(t *testing.T, dir string, names ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if runtime.GOOS != "windows" {
			n = strings.TrimSuffix(strings.TrimSuffix(n, ".exe"), ".cmd")
		}
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\necho REAL-"+n+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEnv_ScopeReferenceScenario_SharedFolder: aws.exe sits in a folder with node and git; the
// folder stays, tq settings render writes the mask, and on the identity PATH aws, aws.exe and
// tq run all reach the stub (command not found, exit 127), never the real one.
func TestEnv_ScopeReferenceScenario_SharedFolder(t *testing.T) {
	root, _ := setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
	shared := filepath.Join(testutil.TempDir(t), "shared")
	fakeIn(t, shared, "aws.exe", "node.exe", "git.exe")
	runEnv(t, "settings", "render", "company-x")
	sys := ""
	if runtime.GOOS == "windows" {
		sys = string(os.PathListSeparator) + filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	t.Setenv("PATH", shared+sys)
	chdir(t, root)
	sh := "bash"
	if runtime.GOOS == "windows" {
		sh = "pwsh"
	}
	set, _ := envJSON(t, sh)
	idPath := set["PATH"]
	mask := envplan.MaskDir("company-x")
	if !strings.HasPrefix(idPath, mask+string(os.PathListSeparator)) || !strings.Contains(idPath, shared) {
		t.Fatalf("identity PATH = %q", idPath)
	}
	env := []string{"PATH=" + idPath, "PATHEXT=.COM;.EXE;.BAT;.CMD"}
	for _, n := range []string{"aws", "aws.exe"} {
		if p, err := envplan.LookPath(n, env); err != nil || filepath.Dir(p) != mask {
			t.Fatalf("%s resolves to %q (%v), want the mask", n, p, err)
		}
	}
	if p, err := envplan.LookPath("node", env); err != nil || filepath.Dir(p) != shared {
		t.Fatalf("node: %q %v", p, err)
	}
	// Run it: the stub answers.
	stub, _ := envplan.LookPath("aws", env)
	c := exec.Command(stub, "ec2", "run-instances")
	if runtime.GOOS != "windows" {
		c = exec.Command("/bin/sh", stub, "ec2", "run-instances")
	}
	out, err := c.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 127 || !strings.Contains(string(out), "aws: command not found (outside the scope of identity company-x)") || strings.Contains(string(out), "REAL-") {
		t.Fatalf("aws: %v %q", err, out)
	}
	// tq run, spelled any way: refused, nothing launched.
	calls := fakeExec(t)
	for _, n := range []string{"aws", "aws.exe", filepath.Join(shared, "aws.exe"), "AWS.EXE"} {
		if err := runCmdErr(t, "run", ".", "--", n, "ec2"); err == nil || !strings.Contains(err.Error(), "command not found on the PATH of identity company-x") {
			t.Fatalf("tq run -- %s: %v", n, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("launched: %+v", *calls)
	}
}

// TestEnv_ScopeReferenceScenario_CmdExe: cmd.exe through its real hook. A 3.5 KB PATH: aws is
// command-not-found (dropped folder) or the stub (shared folder), AWS_PROFILE unset, and
// leaving restores PATH, AWS_PROFILE and TQ_WS exactly. A 9 KB PATH (over cmd.exe's line): the
// identity still applies, PATH is left as it is with a notice, and leaving restores it all.
func TestEnv_ScopeReferenceScenario_CmdExe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe")
	}
	root, _ := setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
	fx := testutil.TempDir(t)
	awsOnly := filepath.Join(fx, "awsonly")
	shared := filepath.Join(fx, "shared")
	azure := filepath.Join(fx, "azure")
	fakeIn(t, awsOnly, "aws.exe", "aws_completer.exe")
	fakeIn(t, shared, "aws.exe", "node.exe")
	fakeIn(t, azure, "az.cmd")
	runEnv(t, "settings", "render", "company-x")
	// The child tq (this test binary, TQ_TEST_BE_TQ=1) has its own in-memory keychain: it
	// verifies with the same key through the fallback file.
	key, err := keyring.Get(trust.Service, trust.KeyAccount)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trust.KeyFile(), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hook, err := shell.HookFor("cmd", self)
	if err != nil {
		t.Fatal(err)
	}
	dir := testutil.TempDir(t)
	hookFile := filepath.Join(dir, "hook.cmd")
	if err := os.WriteFile(hookFile, []byte(strings.ReplaceAll(hook, "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no powershell to compare the result")
	}
	cmp := filepath.Join(dir, "cmp.ps1")
	if err := os.WriteFile(cmp, []byte("param($tag) $p=[Environment]::GetEnvironmentVariable('PATH'); $p0=[Environment]::GetEnvironmentVariable('P0'); "+
		"$tag + ': TQ_WS=[' + $env:TQ_WS + '] AWS_PROFILE=[' + $env:AWS_PROFILE + '] PATH_EQ=' + ($p -ceq $p0) + ' STATE_LEN=' + ([string]$env:__TQ_STATE).Length + ' MASK=' + ($p -like '*path-mask*') + ' AWSONLY=' + ($p -like '*awsonly*')\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	cmdExe := filepath.Join(sys, "cmd.exe")
	for _, tc := range []struct {
		name  string
		extra int // vendor entries appended
	}{{"3.5KB", 50}, {"9KB", 120}} {
		var es []string
		for i := 0; i < tc.extra; i++ {
			es = append(es, fmt.Sprintf(`C:\Program Files\Some Vendor\Some Product Name\Version 12.3.4\bin%d`, i))
		}
		path := strings.Join(append([]string{awsOnly, shared, azure, sys, filepath.Dir(sys)}, es...), ";")
		script := strings.Join([]string{
			"@echo off",
			`set "TQ_TEST_BE_TQ=1"`,
			`set "AWS_PROFILE=fake-prod"`,
			`set "__TQ_STATE="`,
			`set "TQ_WS="`,
			`cd /d "` + root + `"`,
			`call "` + hookFile + `"`,
			`"` + ps + `" -NoProfile -ExecutionPolicy Bypass -File "` + cmp + `" inside`,
			`call aws ec2 run-instances`,
			`echo AWS_EXIT=%ERRORLEVEL%`,
			`cd /d "` + dir + `"`,
			`call "` + hookFile + `"`,
			`"` + ps + `" -NoProfile -ExecutionPolicy Bypass -File "` + cmp + `" left`,
		}, "\r\n") + "\r\n"
		bat := filepath.Join(dir, "run-"+tc.name+".cmd")
		if err := os.WriteFile(bat, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		c := exec.Command(cmdExe, "/c", bat)
		c.Env = append(envWithout("PATH", "P0", "__TQ_STATE", "TQ_WS"), "PATH="+path, "P0="+path)
		out, _ := c.CombinedOutput()
		s := string(out)
		if len(path) < 3500 {
			t.Fatalf("%s: PATH is %d bytes", tc.name, len(path))
		}
		if !strings.Contains(s, "inside: TQ_WS=[company-x] AWS_PROFILE=[]") {
			t.Errorf("%s: identity not applied:\n%s", tc.name, s)
		}
		if !strings.Contains(s, "left: TQ_WS=[] AWS_PROFILE=[fake-prod] PATH_EQ=True STATE_LEN=0") {
			t.Errorf("%s: not restored on leaving:\n%s", tc.name, s)
		}
		if strings.Contains(s, "REAL-") {
			t.Errorf("%s: a real aws ran:\n%s", tc.name, s)
		}
		if tc.extra < 100 {
			if !strings.Contains(s, "MASK=True AWSONLY=False") || !strings.Contains(s, "AWS_EXIT=127") || !strings.Contains(s, "aws: command not found") {
				t.Errorf("%s: PATH layer not applied:\n%s", tc.name, s)
			}
		} else if !strings.Contains(s, "tq: PATH over cmd.exe's 8191-character line") {
			t.Errorf("%s: no notice for the long PATH:\n%s", tc.name, s)
		}
	}
}

func envWithout(keys ...string) []string {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[strings.ToUpper(k)] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	return out
}

// TestPreToolUse_MCPJudgedByConfigEntry: layer 4 reads the server's entry in the identity's
// .claude.json: a server named cloud that runs awslabs' package, and one named gh running the
// GitHub server, are denied; a database server is not.
func TestPreToolUse_MCPJudgedByConfigEntry(t *testing.T) {
	root, _ := setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
	claudeDir := paths.IdentityDir("company-x", "claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cj := `{"mcpServers":{"cloud":{"command":"uvx","args":["awslabs.aws-api-mcp-server"],"env":{"AWS_PROFILE":"prod"}},` +
		`"helper":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]},"mydb":{"command":"uvx","args":["postgres-mcp"]}}}`
	if err := os.WriteFile(filepath.Join(claudeDir, ".claude.json"), []byte(cj), 0o600); err != nil {
		t.Fatal(err)
	}
	// Round 3: a benign user-scope memory and a project .mcp.json memory that runs AWS Labs'
	// server: the name is denied whichever the harness picks. A plugin's vercel server too.
	if err := os.WriteFile(filepath.Join(claudeDir, ".claude.json"), []byte(strings.Replace(cj, `"mydb":`,
		`"memory":{"command":"npx","args":["-y","@modelcontextprotocol/server-memory"]},"mydb":`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"memory":{"command":"uvx","args":["awslabs.core-mcp-server"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plug := filepath.Join(testutil.TempDir(t), "deployer")
	if err := os.MkdirAll(filepath.Join(claudeDir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plug, 0o700); err != nil {
		t.Fatal(err)
	}
	ij := `{"version":2,"plugins":{"deployer@mkt":[{"installPath":` + jsonPath(t, plug) + `}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"), []byte(ij), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plug, ".mcp.json"), []byte(`{"mcpServers":{"site":{"type":"http","url":"https://mcp.vercel.com"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tool string
		deny bool
	}{{"mcp__cloud__call_aws", true}, {"mcp__helper__create_issue", true}, {"mcp__mydb__query", false},
		{"mcp__memory__call_aws", true}, {"mcp__plugin_deployer_site__deploy", true}} {
		code, category, rule, errOut := hookDecision(t, root, c.tool, `{}`)
		if c.deny && (code != 2 || category != policy.CategoryIdentity || rule != policy.OutOfScopeMCPID) {
			t.Errorf("%s: code=%d %s/%s %s", c.tool, code, category, rule, errOut)
		}
		if !c.deny && code != 0 {
			t.Errorf("%s denied: %s", c.tool, errOut)
		}
	}
}

// TestSettingsRender_HybridMCP: the owner's hybrid list end to end. An unknown vendor's server
// under a neutral name is moved to the backup and denied; playwright, context7, memory, the
// in-scope Azure server and a name in the manifest's mcp.allow stay and pass.
func TestSettingsRender_HybridMCP(t *testing.T) {
	root, _ := setupScoped(t, "company-x", "guard: v2\nmcp: { allow: [team-tool] }\n"+companyXBody)
	claudeDir := paths.IdentityDir("company-x", "claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cj := `{"mcpServers":{` +
		`"helper":{"command":"npx","args":["-y","@acmecloud/mcp-server"]},` +
		`"playwright":{"command":"npx","args":["@playwright/mcp@latest"]},` +
		`"context7":{"type":"http","url":"https://mcp.context7.com/mcp"},` +
		`"memory":{"command":"npx","args":["-y","@modelcontextprotocol/server-memory"]},` +
		`"azure":{"command":"npx","args":["-y","@azure/mcp@latest","server","start"]},` +
		`"team-tool":{"command":"node","args":["C:/work/tools/team.js"]}}}`
	if err := os.WriteFile(filepath.Join(claudeDir, ".claude.json"), []byte(cj), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runEnv(t, "settings", "render", "company-x")
	if !strings.Contains(out, "removed MCP server helper") {
		t.Fatalf("render output:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(claudeDir, ".claude.json"))
	var got struct {
		MCP map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"playwright", "context7", "memory", "azure", "team-tool"} {
		if got.MCP[n] == nil {
			t.Errorf("%s removed", n)
		}
	}
	if got.MCP["helper"] != nil {
		t.Error("helper kept")
	}
	for _, c := range []struct {
		tool string
		deny bool
	}{{"mcp__helper__x", true}, {"mcp__playwright__browser_click", false}, {"mcp__context7__resolve", false},
		{"mcp__memory__read_graph", false}, {"mcp__azure__list", false}, {"mcp__team-tool__run", false}} {
		code, _, rule, errOut := hookDecision(t, root, c.tool, `{}`)
		if c.deny && (code != 2 || rule != policy.OutOfScopeMCPID) {
			t.Errorf("%s: code=%d rule=%s %s", c.tool, code, rule, errOut)
		}
		if !c.deny && code != 0 {
			t.Errorf("%s denied: %s", c.tool, errOut)
		}
	}
}
