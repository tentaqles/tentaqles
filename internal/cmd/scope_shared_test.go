package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/shell"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The reference scenario at the PATH: through a folder shared with other
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
// folder stays, tentaqles settings render writes the mask, and on the identity PATH aws, aws.exe and
// tentaqles run all reach the stub (command not found, exit 127), never the real one.
func TestEnv_ScopeReferenceScenario_SharedFolder(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
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
	names := []string{"aws"}
	if runtime.GOOS == "windows" {
		names = append(names, "aws.exe")
	}
	for _, n := range names {
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
	// tentaqles run, spelled any way: refused, nothing launched.
	calls := fakeExec(t)
	for _, n := range []string{"aws", "aws.exe", filepath.Join(shared, "aws.exe"), "AWS.EXE"} {
		if err := runCmdErr(t, "run", ".", "--", n, "ec2"); err == nil || !strings.Contains(err.Error(), "command not found on the PATH of identity company-x") {
			t.Fatalf("tentaqles run -- %s: %v", n, err)
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
	root, _ := setupScoped(t, "company-x", companyXBody)
	fx := testutil.TempDir(t)
	awsOnly := filepath.Join(fx, "awsonly")
	shared := filepath.Join(fx, "shared")
	azure := filepath.Join(fx, "azure")
	fakeIn(t, awsOnly, "aws.exe", "aws_completer.exe")
	fakeIn(t, shared, "aws.exe", "node.exe")
	fakeIn(t, azure, "az.cmd")
	runEnv(t, "settings", "render", "company-x")
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
