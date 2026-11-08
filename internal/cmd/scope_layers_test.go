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
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The reference scenario, an Azure-only client and the AWS CLI, at each layer, in its own test:
// identity company-x declares Azure as its only cloud. Layer 4, the hook, is
// TestPreToolUse_ScopeReferenceScenario.
//
//   - layer 1 (advisory): the session-start preamble states the scope;
//   - layer 2 (PATH): aws is not on the identity PATH tq builds, az is;
//   - layer 3 (harness config): tq settings render leaves no AWS MCP server in the identity's
//     Claude home, even one configured there before.

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

// TestEnv_ScopeReferenceScenario_PATH: on the PATH tq env builds for
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

// The preamble states the client's tools in one line.
func TestSessionStart_ScopeLine(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	_, out, _ := runHook(t, []string{"claude-hook", "session-start"}, `{"cwd":`+jsonPath(t, root)+`}`)
	want := "Tools: az, azure-devops, claude. Other providers' CLIs are not on this terminal's PATH."
	if n := strings.Count(out, "\nTools: "); n != 1 || !strings.Contains(out, want) {
		t.Fatalf("preamble lacks the scope line:\n%s", out)
	}
}
