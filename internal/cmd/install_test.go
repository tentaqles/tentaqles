package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/install"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// azBody is an identity whose cloud is Azure: az and claude are in its scope.
const azBody = "identities: { az: {}, claude: {} }\ncloud: { provider: azure }\n"

// hostPM is the package manager program the catalog's install block for the host OS names for az.
func hostPM() string {
	switch runtime.GOOS {
	case "windows":
		return "winget"
	case "darwin":
		return "brew"
	}
	return "apt-get"
}

// installFixture is a trusted workspace named acme (body is the manifest after the schema
// line), the current folder, and a PATH holding only a fake package manager that logs its argv
// and its environment and, when it runs, installs a fake az (printing azVersion) into the same
// folder. The fake CLI is staged elsewhere, so az is not on the PATH before the install.
type installFixture struct {
	root, bin string
	log, env  string // the package manager's argv log and environment log
	azEnv     string // the fake az's environment log
	cli       string // the staged fake az
}

const azVersion = "azure-cli                         2.99.0 *"

func newInstallFixture(t *testing.T, body string) *installFixture {
	t.Helper()
	root, _ := setupScoped(t, "acme", body)
	chdir(t, root)
	installRegistryPathOff(t)
	logs := testutil.TempDir(t)
	f := &installFixture{root: root, bin: testutil.TempDir(t), log: filepath.Join(logs, "pm.log"),
		env: filepath.Join(logs, "pm.env"), azEnv: filepath.Join(logs, "az.env")}
	stage := testutil.TempDir(t)
	f.cli = testutil.NewFakeProgram(t, stage, "az", testutil.FakeSpec{Stdout: azVersion + "\nsecond line\n", EnvLog: f.azEnv, EnvNames: []string{"AZURE_CONFIG_DIR"}})
	dst := filepath.Join(f.bin, filepath.Base(f.cli))
	testutil.NewFakeProgram(t, f.bin, hostPM(), testutil.FakeSpec{Log: f.log, EnvLog: f.env, EnvNames: []string{"AZURE_CONFIG_DIR", "GH_CONFIG_DIR"},
		Copy: []testutil.FakeCopy{{From: f.cli, To: dst}, {From: testutil.FakeSidecar(f.cli), To: testutil.FakeSidecar(dst)}}})
	t.Setenv("PATH", f.bin)
	if runtime.GOOS == "windows" {
		t.Setenv("SystemRoot", os.Getenv("SystemRoot"))
	}
	return f
}

// installRegistryPathOff keeps a test from reading the real registry.
func installRegistryPathOff(t *testing.T) {
	t.Helper()
	prev := installRegistryPath
	installRegistryPath = func() (string, error) { return "", nil }
	t.Cleanup(func() { installRegistryPath = prev })
}

// installExit is what the stubbed exitFunc panics with, so a test sees the exit code.
type installExit int

// runInstallCode runs `tentaqles install args...` and returns its output and error, and the
// code the command exited with through exitFunc (0 when it did not).
func runInstallCode(t *testing.T, args ...string) (out string, code int, err error) {
	t.Helper()
	prev := exitFunc
	exitFunc = func(c int) { panic(installExit(c)) }
	t.Cleanup(func() { exitFunc = prev })
	var buf bytes.Buffer
	root := NewRoot()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"install"}, args...))
	func() {
		defer func() {
			if r := recover(); r != nil {
				c, ok := r.(installExit)
				if !ok {
					panic(r)
				}
				code = int(c)
			}
		}()
		err = root.Execute()
	}()
	return buf.String(), code, err
}

// runInstall is runInstallCode for a test that only asks whether the call was refused: an exit
// of the command reads as an error holding what it printed.
func runInstall(t *testing.T, args ...string) (out string, err error) {
	t.Helper()
	out, code, err := runInstallCode(t, args...)
	if err == nil && code != 0 {
		err = fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return out, err
}

// runMainInstall runs `tentaqles install args...` through RunMain, the path the binary takes,
// and returns what it wrote to stdout and stderr and its exit code.
func runMainInstall(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	prev := exitFunc
	exitFunc = func(c int) { panic(installExit(c)) }
	t.Cleanup(func() { exitFunc = prev })
	var o, e bytes.Buffer
	func() {
		defer func() {
			if r := recover(); r != nil {
				c, ok := r.(installExit)
				if !ok {
					panic(r)
				}
				code = int(c)
			}
		}()
		code = RunMain(append([]string{"install"}, args...), &o, &e)
	}()
	return o.String(), e.String(), code
}

// wantArgs is the argv the catalog's az entry gives its package manager on the host OS.
func wantArgs(t *testing.T) []string {
	t.Helper()
	c, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	az, _ := c.Get("az")
	h := install.OSHints(az, runtime.GOOS)
	pkg := map[string]string{"winget": h.Winget, "brew": h.Brew, "apt-get": h.Apt}[hostPM()]
	method := map[string]string{"winget": "winget", "brew": "brew", "apt-get": "apt"}[hostPM()]
	if pkg == "" {
		t.Fatalf("az has no %s package for %s", method, runtime.GOOS)
	}
	args, _ := install.MethodArgs(method, pkg)
	return args
}

// The package manager runs with the catalog's package id and the identity's environment, then
// the version command runs with it too.
func TestInstall_InScope(t *testing.T) {
	f := newInstallFixture(t, azBody)
	out, err := runInstall(t, "az")
	if err != nil {
		t.Fatalf("install az: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(out), "az: installed "+azVersion; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	got := testutil.ReadFakeLog(t, f.log)
	if len(got) != 1 || !reflect.DeepEqual(got[0], wantArgs(t)) {
		t.Fatalf("package manager argv = %v, want exactly [%v]", got, wantArgs(t))
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(strings.Join(got[0], " "), "install --id Microsoft.AzureCLI -e") {
		t.Fatalf("winget argv = %v, want install --id Microsoft.AzureCLI -e ...", got[0])
	}
	// The identity's environment: az's own config folder is the identity's, in both runs.
	idRoot := paths.IdentitiesRoot()
	for name, log := range map[string]string{"package manager": f.env, "version check": f.azEnv} {
		envs := testutil.ReadFakeEnv(t, log)
		if len(envs) != 1 {
			t.Fatalf("%s ran %d times, want once", name, len(envs))
		}
		if dir := envs[0]["AZURE_CONFIG_DIR"]; dir == "" || !strings.HasPrefix(dir, idRoot) || !strings.Contains(dir, "acme") {
			t.Errorf("%s: AZURE_CONFIG_DIR = %q, want a folder of identity acme under %s", name, dir, idRoot)
		}
	}
}

func TestInstall_PresentManualNoCLI(t *testing.T) {
	f := newInstallFixture(t, azBody)
	// az is already on the PATH, next to the package manager.
	testutil.NewFakeProgram(t, f.bin, "az", testutil.FakeSpec{Stdout: azVersion + "\n"})
	out, err := runInstall(t, "az")
	if err != nil {
		t.Fatalf("install az: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(out), "az: already present "+azVersion; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 0 {
		t.Fatalf("a package manager ran for a CLI that is present: %v", got)
	}

	// claude has only a url under install: never run, the docs page is printed. slack has no CLI.
	g := newInstallFixture(t, azBody+"scope: [slack]\n")
	out, err = runInstall(t, "claude")
	if err != nil {
		t.Fatalf("install claude: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(out), "claude: manual, download from https://docs.claude.com/claude-code"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	out, err = runInstall(t, "slack")
	if err != nil {
		t.Fatalf("install slack: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(out), "slack: no CLI, nothing to install"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	// az with no package manager on the PATH is manual too.
	t.Setenv("PATH", t.TempDir())
	out, err = runInstall(t, "az")
	if err != nil {
		t.Fatalf("install az: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(out), "az: manual, download from https://learn.microsoft.com/cli/azure"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if got := testutil.ReadFakeLog(t, g.log); len(got) != 0 {
		t.Fatalf("a package manager ran: %v", got)
	}
}

func TestInstall_NoIdentity(t *testing.T) {
	// A neutral folder.
	isolateHome(t)
	chdir(t, testutil.TempDir(t))
	_, err := runInstall(t, "az")
	if err == nil || !strings.Contains(err.Error(), "needs a client identity") || !strings.Contains(err.Error(), "not inside a trusted client") {
		t.Fatalf("neutral folder: err = %v, want it to name the reason", err)
	}

	// A client whose manifest changed after it was trusted.
	f := newInstallFixture(t, azBody)
	mp := paths.ManifestPath("acme")
	raw, rerr := os.ReadFile(mp)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if werr := os.WriteFile(mp, append(raw, []byte("# edited\n")...), 0o600); werr != nil {
		t.Fatal(werr)
	}
	if h, _ := trust.HashFile(mp); trust.Check("acme", h).OK {
		t.Fatal("the edited manifest is still trusted")
	}
	_, err = runInstall(t, "az")
	if err == nil || !strings.Contains(err.Error(), "workspace acme is ") {
		t.Fatalf("untrusted folder: err = %v, want it to name the workspace and the reason", err)
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 0 {
		t.Fatalf("a package manager ran in an untrusted folder: %v", got)
	}

	// An id the catalog does not hold.
	newInstallFixture(t, azBody)
	_, err = runInstall(t, "no-such-provider")
	if err == nil || !strings.Contains(err.Error(), `unknown provider "no-such-provider"`) || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("unknown id: err = %v, want it to name the catalog", err)
	}
}

func TestInstall_OutOfScopeRefused(t *testing.T) {
	for _, guard := range []string{"", "guard: v2\n"} {
		f := newInstallFixture(t, guard+azBody)
		out, code, err := runInstallCode(t, "aws")
		want := "aws is outside the scope of identity acme: az, claude"
		if err != nil || code != 1 || strings.TrimSpace(out) != want {
			t.Fatalf("guard %q: err = %v, exit %d, out %q, want exit 1 and exactly %q", guard, err, code, out, want)
		}
		if got := testutil.ReadFakeLog(t, f.log); len(got) != 0 {
			t.Fatalf("a package manager ran for an out-of-scope provider: %v", got)
		}
		ds, rerr := audit.ReadDenials()
		if rerr != nil {
			t.Fatal(rerr)
		}
		if len(ds) != 1 || ds[0].Category != "identity" || ds[0].Subject != "aws" || ds[0].Identity != "acme" || ds[0].Layer != audit.LayerCommand {
			t.Fatalf("guard %q: denial log = %+v, want one identity line for aws", guard, ds)
		}
	}
}

// The lines REQ-4 and REQ-7 give reach the user as they are worded, with nothing in front,
// through RunMain, and the denial is logged before the exit.
func TestInstall_LinesThroughRunMain(t *testing.T) {
	f := newInstallFixture(t, azBody)
	stdout, stderr, code := runMainInstall(t, "aws")
	if want := "aws is outside the scope of identity acme: az, claude\n"; stdout+stderr != want || code != 1 {
		t.Fatalf("out of scope: stdout %q stderr %q exit %d, want %q and exit 1", stdout, stderr, code, want)
	}
	if ds, err := audit.ReadDenials(); err != nil || len(ds) != 1 || ds[0].Category != "identity" {
		t.Fatalf("denial log = %+v %v, want one identity line", ds, err)
	}

	// The package manager fails after a warning: the line is its error line.
	pm := filepath.Join(f.bin, testutil.FakeFile(hostPM()))
	testutil.WriteFakeSpec(t, pm, testutil.FakeSpec{Log: f.log, Exit: 1, Stderr: "Warning: slow mirror\nError: boom\n"})
	stdout, stderr, code = runMainInstall(t, "az")
	if want := "az: install failed: Error: boom\n"; stdout+stderr != want || code != 1 {
		t.Fatalf("failure: stdout %q stderr %q exit %d, want %q and exit 1", stdout, stderr, code, want)
	}
}

// azure-devops uses az: it reports the install of az, then the extension step as a note that tq
// prints and never runs.
func TestInstall_AzureDevOpsNote(t *testing.T) {
	f := newInstallFixture(t, "identities: { azure-devops: {}, claude: {} }\n")
	out, err := runInstall(t, "azure-devops")
	if err != nil {
		t.Fatalf("install azure-devops: %v\n%s", err, out)
	}
	want := "azure-devops: installed " + azVersion + "\nthen: az extension add --name azure-devops"
	if strings.TrimSpace(out) != want {
		t.Fatalf("output = %q, want %q", strings.TrimSpace(out), want)
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 1 {
		t.Fatalf("package manager runs = %v, want one (the note is never run)", got)
	}
}

func TestInstall_NoShellNoExtraArgs(t *testing.T) {
	f := newInstallFixture(t, azBody)
	for _, args := range [][]string{
		{"aws & calc"},
		{"az", "--source", "evil"},
		{"az", "--", "--override", "x"},
		{"az", "extra"},
		{"az;calc"},
		{"$(calc)"},
		{},
	} {
		if out, err := runInstall(t, args...); err == nil {
			t.Errorf("install %q succeeded: %s", args, out)
		}
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 0 {
		t.Fatalf("a package manager ran for a refused call: %v", got)
	}
	if _, err := os.Stat(filepath.Join(f.bin, filepath.Base(f.cli))); err == nil {
		t.Fatal("az was installed by a refused call")
	}
	// What does run is exactly the catalog's method: no other element in the argv.
	if out, err := runInstall(t, "az"); err != nil {
		t.Fatalf("install az: %v\n%s", err, out)
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 1 || !reflect.DeepEqual(got[0], wantArgs(t)) {
		t.Fatalf("package manager argv = %v, want exactly [%v]", got, wantArgs(t))
	}
}

// A user provider file can add a provider or extend what the guard denies; it cannot name a
// package tq runs. A new provider is not installable, an override of az keeps az's built-in
// method.
func TestInstall_UserProviderHasNoInstall(t *testing.T) {
	f := newInstallFixture(t, azBody)
	dir := providers.UserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("evil.yaml", "id: evil\nname: Evil\ncategory: other\ndocs: https://evil.example\ncli:\n  command: evil\n  version_args: [\"--version\"]\n"+
		"install:\n  windows: { winget: \"Evil.Pkg\" }\n  macos: { brew: \"evil-pkg\" }\n  linux: { apt: \"evil-pkg\", npm: \"evil-pkg\" }\nmatch:\n  executables: [evil]\n")
	write("az.yaml", "id: az\nname: Azure\ncategory: cloud\ndocs: https://evil.example\ncli:\n  command: evil-az\n  version_args: [\"--version\"]\n"+
		"install:\n  windows: { winget: \"Evil.AzureCLI\" }\n  macos: { brew: \"evil-azure-cli\" }\n  linux: { apt: \"evil-azure-cli\" }\n")

	_, err := runInstall(t, "evil")
	if err == nil || !strings.Contains(err.Error(), "not in the built-in provider catalog") {
		t.Fatalf("a user-only provider: err = %v, want it refused as not built in", err)
	}
	if got := testutil.ReadFakeLog(t, f.log); len(got) != 0 {
		t.Fatalf("the user provider's package was run: %v", got)
	}

	out, err := runInstall(t, "az")
	if err != nil {
		t.Fatalf("install az: %v\n%s", err, out)
	}
	got := testutil.ReadFakeLog(t, f.log)
	if len(got) != 1 || !reflect.DeepEqual(got[0], wantArgs(t)) {
		t.Fatalf("package manager argv = %v, want the built-in %v", got, wantArgs(t))
	}
	if !strings.Contains(strings.TrimSpace(out), "az: installed ") {
		t.Fatalf("line = %q: the built-in command az was not the one verified", out)
	}
}

// The hook allows tentaqles install of an in-scope provider, spelled either way, and keeps
// denying a human-only verb, alone or in the same call.
func TestPreToolUse_InstallAllowed(t *testing.T) {
	root, _ := setupScoped(t, "acme", "guard: v2\n"+azBody)
	decide := func(command string) (code int, rule, reason string) {
		t.Helper()
		code, out, _ := runHook(t, []string{"claude-hook", "pre-tool-use", "--json"}, hookPayloadFor(t, root, command))
		var d struct {
			Block  bool   `json:"block"`
			Rule   string `json:"rule"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Fatalf("%s: not a decision: %q (%v)", command, out, err)
		}
		return code, d.Rule, d.Reason
	}
	for _, c := range []string{"tentaqles install az", "tq install az"} {
		if code, rule, reason := decide(c); code != 0 || rule != "" {
			t.Errorf("%s: code=%d rule=%q %s, want allowed", c, code, rule, reason)
		}
	}
	for _, c := range []string{"tq allow acme", "tq install az && tq allow acme"} {
		code, rule, reason := decide(c)
		if code != 2 || rule != "tq/human-only-command" || !strings.Contains(reason, "BLOCKED [self-protection/tq/human-only-command]") {
			t.Errorf("%s: code=%d rule=%q reason=%q, want BLOCKED [self-protection/tq/human-only-command]", c, code, rule, reason)
		}
	}
}

// Azure DevOps and Azure share the az CLI: one reach installs it once, the second provider finds
// it (on Windows through the registry PATH, since winget's PATH change is not in this process)
// and prints the extension step after its line.
func TestReach_SharedCLIInstallsOnce(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test", Git: "azure-devops", Clouds: []string{"azure"}})
	reachInstall = installScope
	logs, bin, stage, newDir := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	log := filepath.Join(logs, "pm.log")
	dstDir := bin
	if runtime.GOOS == "windows" {
		dstDir = newDir // a folder the running process's PATH does not hold
		prev := installRegistryPath
		installRegistryPath = func() (string, error) { return newDir, nil }
		t.Cleanup(func() { installRegistryPath = prev })
		t.Setenv("SystemRoot", os.Getenv("SystemRoot"))
	} else {
		installRegistryPathOff(t)
	}
	cli := testutil.NewFakeProgram(t, stage, "az", testutil.FakeSpec{Stdout: azVersion + "\n"})
	dst := filepath.Join(dstDir, filepath.Base(cli))
	testutil.NewFakeProgram(t, bin, hostPM(), testutil.FakeSpec{Log: log,
		Copy: []testutil.FakeCopy{{From: cli, To: dst}, {From: testutil.FakeSidecar(cli), To: testutil.FakeSidecar(dst)}}})
	t.Setenv("PATH", bin)

	out, err := runReach(t, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("reach: %v\n%s", err, out)
	}
	for _, want := range []string{"az: installed " + azVersion, "azure-devops: already present " + azVersion, "then: az extension add --name azure-devops"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "install failed") {
		t.Errorf("a provider failed:\n%s", out)
	}
	if got := testutil.ReadFakeLog(t, log); len(got) != 1 {
		t.Errorf("package manager runs = %v, want one", got)
	}
}

var reachAnswersAzGh = reach.Answers{Email: "me@acme.test", Git: "github", Clouds: []string{"azure"}}

// ghPackage is the package id the catalog gives gh's package manager on the host OS.
func ghPackage(t *testing.T) string {
	t.Helper()
	c, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	gh, _ := c.Get("gh")
	h := install.OSHints(gh, runtime.GOOS)
	pkg := map[string]string{"winget": h.Winget, "brew": h.Brew, "apt-get": h.Apt}[hostPM()]
	if pkg == "" {
		t.Fatalf("gh has no package for %s", runtime.GOOS)
	}
	return pkg
}

// reach installs the CLI of each provider in the new client's scope, one line each after the
// trust is signed; a failure does not stop the rest nor undo the client; --no-install runs
// nothing.
func TestReach_InstallsAndContinues(t *testing.T) {
	clients := isolateReach(t, &reachAnswersAzGh)
	installRegistryPathOff(t)
	reachInstall = installScope

	logs := testutil.TempDir(t)
	bin := testutil.TempDir(t)
	log := filepath.Join(logs, "pm.log")
	stage := testutil.TempDir(t)
	cli := testutil.NewFakeProgram(t, stage, "az", testutil.FakeSpec{Stdout: azVersion + "\n"})
	dst := filepath.Join(bin, filepath.Base(cli))
	// The package manager installs az, and fails for gh with an error after a warning.
	testutil.NewFakeProgram(t, bin, hostPM(), testutil.FakeSpec{Log: log, FailArg: ghPackage(t), FailStderr: "Warning: slow mirror\nError: gh: no bottle available\n",
		Copy: []testutil.FakeCopy{{From: cli, To: dst}, {From: testutil.FakeSidecar(cli), To: testutil.FakeSidecar(dst)}}})
	t.Setenv("PATH", bin)
	if runtime.GOOS == "windows" {
		t.Setenv("SystemRoot", os.Getenv("SystemRoot"))
	}

	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("reach: %v\n%s", err, out)
	}
	for _, want := range []string{"az: installed " + azVersion, "gh: install failed: Error: gh: no bottle available", "claude: manual, download from https://docs.claude.com/claude-code"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Warning: slow mirror") {
		t.Errorf("a warning line was printed as the reason:\n%s", out)
	}
	if strings.Index(out, "Trusted acme") > strings.Index(out, "az: installed") {
		t.Errorf("installs ran before the trust was signed:\n%s", out)
	}
	if !strings.Contains(out, "acme is ready.") {
		t.Errorf("a failed install stopped reach:\n%s", out)
	}
	if got := testutil.ReadFakeLog(t, log); len(got) != 2 {
		t.Errorf("package manager runs = %v, want one for az and one for gh", got)
	}
	h, _ := trust.HashFile(paths.ManifestPath("acme"))
	if v := trust.Check("acme", h); !v.OK {
		t.Fatalf("the client is not trusted after a failed install: %s", v.Reason)
	}

	// --no-install runs nothing.
	if rerr := os.Remove(dst); rerr != nil {
		t.Fatal(rerr)
	}
	before := len(testutil.ReadFakeLog(t, log))
	out, err = runReach(t, filepath.Join(clients, "beta"), "--no-banner", "--no-install")
	if err != nil {
		t.Fatalf("reach --no-install: %v\n%s", err, out)
	}
	if got := len(testutil.ReadFakeLog(t, log)); got != before {
		t.Errorf("--no-install ran a package manager")
	}
	if strings.Contains(out, "installed") || strings.Contains(out, "install failed") || strings.Contains(out, "manual, download") {
		t.Errorf("--no-install printed an install line:\n%s", out)
	}
	if _, serr := manifest.Load(paths.ManifestPath("beta")); serr != nil {
		t.Fatal(serr)
	}
}
