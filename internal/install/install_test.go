package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The test binary is the fake package manager and the fake CLI the tests put on a PATH: run
// under another name with a sidecar it behaves as the sidecar says (internal/testutil).
//
// The suite also points TQ_HOME, HOME and USERPROFILE at an empty folder, so nothing it loads
// (the catalog reads the user provider folder) reaches the developer's real state. Under Claude
// Code's marker paths.Home ignores TQ_HOME, so the override is allowed here.
func TestMain(m *testing.M) {
	if ok, code := testutil.FakeMain(); ok {
		os.Exit(code)
	}
	dir, err := os.MkdirTemp("", "tq-install-")
	if err != nil {
		panic(err)
	}
	for _, k := range []string{"HOME", "USERPROFILE"} {
		os.Setenv(k, dir)
	}
	os.Setenv("TQ_HOME", filepath.Join(dir, ".tentaqles"))
	paths.AllowHomeOverride = true
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func pathEnv(dirs ...string) []string {
	env := []string{"PATH=" + strings.Join(dirs, string(os.PathListSeparator))}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return env
}

func realDeps(goos string, env []string) Deps {
	return Deps{GOOS: goos, Env: env, LookPath: envplan.LookPath, Run: DefaultRun}
}

// tool is a provider whose only install method on linux is one method, so a test picks the
// package manager it fakes.
func tool(method string) providers.Provider {
	o := providers.InstallOS{}
	switch method {
	case "npm":
		o.Npm = "tool-pkg"
	case "apt":
		o.Apt = "tool-pkg"
	case "winget":
		o.Winget = "Tool.Pkg"
	case "scoop":
		o.Scoop = "tool-pkg"
	case "brew":
		o.Brew = "tool-pkg"
	case "pip":
		o.Pip = "tool-pkg"
	}
	return providers.Provider{ID: "tool", Docs: "https://example.com/tool", CLI: &providers.CLI{Command: "tool", VersionArgs: []string{"--version"}},
		Install: providers.Install{Linux: o, Windows: o, Macos: o}}
}

func azProvider(t *testing.T) providers.Provider {
	t.Helper()
	c, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := c.Get("az")
	if !ok {
		t.Fatal("az missing from the catalog")
	}
	return p
}

// The failure line is the package manager's error line: not its first progress line, a
// spinner frame, or a warning. Both streams are covered, and so are the fixed shapes of
// winget, npm, apt, brew and pip.
func TestInstall_FailureLineIsTheError(t *testing.T) {
	cases := []struct {
		name, method, prog, stdout, stderr, want string
	}{
		{
			name: "winget: progress and spinner on stdout, then the error", method: "winget", prog: "winget",
			stdout: "Found Tool [Tool.Pkg] Version 2.0.0\nThis application is licensed to you by its owner.\n" +
				"Downloading https://example.com/tool.msi\n  ██████████░░░░░░░░░░  4.00 MB / 9.00 MB\n" +
				"\r-\r\\\r|\r/\r-\r\\\r|\r/\n  ██████████████████████████████  9.00 MB / 9.00 MB\nSuccessfully verified installer hash\nStarting package install...\n" +
				"Installer failed with exit code: 1603\n",
			want: "tool: install failed: Installer failed with exit code: 1603",
		},
		{
			name: "winget: a verdict with no error word", method: "winget", prog: "winget",
			stdout: "Found Tool [Tool.Pkg] Version 2.0.0\nNo applicable installer found; see logs for more details.\n",
			want:   "tool: install failed: No applicable installer found; see logs for more details.",
		},
		{
			name: "winget: no package", method: "winget", prog: "winget",
			stdout: "\r-\r\\\r|\rNo package found matching input criteria.\n",
			want:   "tool: install failed: No package found matching input criteria.",
		},
		{
			name: "winget: no upgrade", method: "winget", prog: "winget",
			stdout: "Found Tool [Tool.Pkg] Version 2.0.0\nNo available upgrade found.\n",
			want:   "tool: install failed: No available upgrade found.",
		},
		{
			name: "scoop: ERROR after downloads", method: "scoop", prog: "scoop",
			stdout: "Installing 'tool-pkg' (1.0.0) [64bit] from 'main' bucket\nDownloading https://example.com/tool.zip (4.2 MB)...\n" +
				"Checking hash of tool.zip ... ok.\nERROR Could not find 'tool-pkg'.\n",
			want: "tool: install failed: ERROR Could not find 'tool-pkg'.",
		},
		{
			name: "brew: Warning first, then Error", method: "brew", prog: "brew",
			stderr: "Warning: tool-pkg 1.0 is already installed but outdated\nError: tool-pkg: no bottle available!\n",
			want:   "tool: install failed: Error: tool-pkg: no bottle available!",
		},
		{
			name: "pip: WARNING first, then ERROR", method: "pip", prog: "pip",
			stdout: "Collecting tool-pkg\n",
			stderr: "WARNING: Retrying (Retry(total=4)) after connection broken\nERROR: No matching distribution found for tool-pkg\n",
			want:   "tool: install failed: ERROR: No matching distribution found for tool-pkg",
		},
		{
			name: "npm: warnings first, then npm error", method: "npm", prog: "npm",
			stderr: "npm warn deprecated rimraf@3.0.2: Rimraf versions prior to v4 are no longer supported\nnpm warn config global `--global` is deprecated\n" +
				"npm error code EACCES\nnpm error syscall mkdir\nnpm error path /usr/lib/node_modules/tool-pkg\n",
			want: "tool: install failed: npm error code EACCES",
		},
		{
			name: "apt: reading lists on stdout, E: on stderr", method: "apt", prog: "apt-get",
			stdout: "Reading package lists...\nBuilding dependency tree...\n",
			stderr: "W: Skipping acquire of configured file 'main/binary-i386/Packages'\nE: Could not open lock file /var/lib/dpkg/lock-frontend - open (13: Permission denied)\n",
			want:   "tool: install failed: E: Could not open lock file /var/lib/dpkg/lock-frontend - open (13: Permission denied)",
		},
		{
			name: "an escape sequence in the line is removed", method: "npm", prog: "npm",
			stderr: "\x1b[33mnpm warn\x1b[0m something\n\x1b[31mnpm error\x1b[0m code E404\x07\n",
			want:   "tool: install failed: npm error code E404",
		},
		{
			name: "no error word: the first plain stderr line", method: "npm", prog: "npm",
			stdout: "added 1 package in 2s\n", stderr: "npm warn old\nthe registry refused the request\n",
			want: "tool: install failed: the registry refused the request",
		},
		{
			name: "no output at all: the exit status", method: "npm", prog: "npm",
			stderr: "  \r\n-\r\\\n",
			want:   "tool: install failed: exit status 1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin, log := t.TempDir(), filepath.Join(t.TempDir(), "pm.log")
			testutil.NewFakeProgram(t, bin, c.prog, testutil.FakeSpec{Log: log, Exit: 1, Stdout: c.stdout, Stderr: c.stderr})
			_, err := Install(tool(c.method), realDeps("linux", pathEnv(bin)))
			var f *Failure
			if !errors.As(err, &f) {
				t.Fatalf("err = %v, want a *Failure", err)
			}
			if err.Error() != c.want {
				t.Fatalf("line = %q, want %q", err.Error(), c.want)
			}
			if n := len(testutil.ReadFakeLog(t, log)); n != 1 {
				t.Fatalf("package manager ran %d times, want 1", n)
			}
		})
	}

	t.Run("a long line is cut", func(t *testing.T) {
		got := ErrorLine("npm error "+strings.Repeat("x", 500), "", errors.New("exit status 1"))
		if n := len([]rune(got)); n > 203 {
			t.Fatalf("line has %d runes", n)
		}
	})
}

// A package manager that hangs is stopped at the (lowered) limit with everything it started:
// the fake starts a child that holds the same pipes and would sleep for a minute.
func TestInstall_TimeoutStopsTheProcessTree(t *testing.T) {
	bin, pids := t.TempDir(), filepath.Join(t.TempDir(), "pids")
	prog := "winget"
	method := "winget"
	if runtime.GOOS != "windows" {
		prog, method = "npm", "npm"
	}
	testutil.NewFakeProgram(t, bin, prog, testutil.FakeSpec{PidFile: pids, Spawn: true, SleepMS: 60000})
	d := realDeps(runtime.GOOS, pathEnv(bin))
	d.Timeout = 3 * time.Second
	start := time.Now()
	_, err := Install(tool(method), d)
	if err == nil || !strings.HasPrefix(err.Error(), "tool: install failed: ") || !strings.Contains(err.Error(), "was stopped") {
		t.Fatalf("err = %v, want install failed ... was stopped", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("the hanging package manager held the install for %s", time.Since(start))
	}
	got := testutil.ReadPids(t, pids)
	if len(got) != 2 {
		t.Fatalf("pids recorded = %v, want the fake and its child", got)
	}
	for _, pid := range got {
		waitGone(t, pid)
	}
}

// waitGone fails the test when the process is still running a few seconds on.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still running after the limit", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// After a Windows install the CLI is looked up on the PATH a new process would have, read from
// the registry: the folder the package manager added is not on the running process's PATH.
func TestInstall_RefreshesPathFromRegistry(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the registry PATH refresh is Windows only")
	}
	p := azProvider(t)
	bin, stage, newDir := t.TempDir(), t.TempDir(), t.TempDir()
	log := filepath.Join(t.TempDir(), "pm.log")
	cli := testutil.NewFakeProgram(t, stage, "az", testutil.FakeSpec{Stdout: "azure-cli                         2.99.0 *\nsecond line\n"})
	testutil.NewFakeProgram(t, bin, "winget", testutil.FakeSpec{Log: log, Copy: []testutil.FakeCopy{
		{From: cli, To: filepath.Join(newDir, filepath.Base(cli))},
		{From: testutil.FakeSidecar(cli), To: testutil.FakeSidecar(filepath.Join(newDir, filepath.Base(cli)))},
	}})

	d := realDeps(runtime.GOOS, pathEnv(bin))
	d.RegistryPath = func() (string, error) { return newDir, nil }
	res, err := Install(p, d)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got, want := res.Line(), "az: installed azure-cli                         2.99.0 *"; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	want := []string{"install", "--id", "Microsoft.AzureCLI", "-e", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}
	if got := testutil.ReadFakeLog(t, log); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("winget argv = %v, want [%v]", got, want)
	}

	// Without the registry folder the same install finishes but the CLI cannot be found.
	_ = os.Remove(filepath.Join(newDir, filepath.Base(cli)))
	d.RegistryPath = func() (string, error) { return "", nil }
	_, err = Install(p, d)
	if err == nil || !strings.Contains(err.Error(), "az: install failed: winget finished but az is not on the PATH") {
		t.Fatalf("err = %v, want the CLI-not-on-PATH failure", err)
	}
}

// The verdict comes last: an error after more than 64 KiB of status lines is still the reason.
func TestInstall_LateErrorLineIsKept(t *testing.T) {
	filler := strings.Repeat("resolving step of the plan, nothing wrong here\n", 2000)
	if len(filler) < 70<<10 {
		t.Fatalf("filler is %d bytes", len(filler))
	}
	t.Run("stderr", func(t *testing.T) {
		bin := t.TempDir()
		testutil.NewFakeProgram(t, bin, "pip", testutil.FakeSpec{Exit: 1, Stderr: filler + "ERROR: no matching distribution\n"})
		_, err := Install(tool("pip"), realDeps("linux", pathEnv(bin)))
		if err == nil || err.Error() != "tool: install failed: ERROR: no matching distribution" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stdout", func(t *testing.T) {
		bin := t.TempDir()
		testutil.NewFakeProgram(t, bin, "winget", testutil.FakeSpec{Exit: 1, Stdout: filler + "Installer failed with exit code: 1603\n"})
		_, err := Install(tool("winget"), realDeps("linux", pathEnv(bin)))
		if err == nil || err.Error() != "tool: install failed: Installer failed with exit code: 1603" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the writer is bounded and keeps the start and the end", func(t *testing.T) {
		w := &limitWriter{}
		for i := 0; i < 5000; i++ {
			_, _ = w.Write([]byte("line " + strings.Repeat("x", 90) + "\n"))
		}
		_, _ = w.Write([]byte("the very last line\n"))
		s := w.String()
		if len(s) > headMax+tailMax+2 || !strings.HasPrefix(s, "line ") || !strings.HasSuffix(s, "the very last line\n") {
			t.Fatalf("kept %d bytes, start %q, end %q", len(s), s[:10], s[len(s)-20:])
		}
	})
}

// An azure-devops style note ("then: ...") follows the line; any other note is an install
// alternative and is not printed; a manual or no-CLI result prints no step.
func TestResult_OutputPrintsOnlyAThenNote(t *testing.T) {
	mk := func(note string) providers.Provider {
		o := providers.InstallOS{Apt: "x", Note: note}
		return providers.Provider{ID: "x", Docs: "https://example.com", CLI: &providers.CLI{Command: "x"}, Install: providers.Install{Linux: o}}
	}
	var ran [][]string
	d := Deps{GOOS: "linux", LookPath: lookIn("x"), Run: recordRun(&ran)}
	res, err := Install(mk("then: x extension add --name y"), d)
	if err != nil || res.Output() != "x: already present tool 1.2.3\nthen: x extension add --name y" {
		t.Fatalf("output = %q err=%v", res.Output(), err)
	}
	res, _ = Install(mk("curl -sL https://example.com/x | sudo bash"), d)
	if strings.Contains(res.Output(), "curl") || strings.Contains(res.Output(), "\n") {
		t.Fatalf("an install alternative was printed: %q", res.Output())
	}
	res, _ = Install(mk("then: x extension add --name y"), Deps{GOOS: "linux", LookPath: lookIn()})
	if res.Status != Manual || strings.Contains(res.Output(), "then:") {
		t.Fatalf("manual result = %+v, output %q", res, res.Output())
	}
}

// After one install the CLI is found on the registry PATH before any package manager runs: the
// same CLI under a second provider (azure-devops after az in one reach) and a retry from the
// same terminal print "already present" and do not run winget again.
func TestInstall_FoundOnRegistryPathBeforeRunning(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the registry PATH is Windows only")
	}
	c, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	az, _ := c.Get("az")
	devops, _ := c.Get("azure-devops")
	bin, stage, newDir := t.TempDir(), t.TempDir(), t.TempDir()
	log := filepath.Join(t.TempDir(), "pm.log")
	cli := testutil.NewFakeProgram(t, stage, "az", testutil.FakeSpec{Stdout: "azure-cli 2.99.0\n"})
	testutil.NewFakeProgram(t, bin, "winget", testutil.FakeSpec{Log: log, Copy: []testutil.FakeCopy{
		{From: cli, To: filepath.Join(newDir, filepath.Base(cli))},
		{From: testutil.FakeSidecar(cli), To: testutil.FakeSidecar(filepath.Join(newDir, filepath.Base(cli)))},
	}})
	d := realDeps(runtime.GOOS, pathEnv(bin))
	d.RegistryPath = func() (string, error) { return newDir, nil }

	res, err := Install(az, d)
	if err != nil || res.Status != Installed {
		t.Fatalf("az: %+v %v", res, err)
	}
	res, err = Install(devops, d)
	if err != nil || res.Line() != "azure-devops: already present azure-cli 2.99.0" {
		t.Fatalf("azure-devops: line %q err %v, want already present", res.Line(), err)
	}
	if res.Output() != res.Line()+"\nthen: az extension add --name azure-devops" {
		t.Fatalf("azure-devops output = %q, want the extension note after the line", res.Output())
	}
	res, err = Install(az, d)
	if err != nil || res.Status != Present {
		t.Fatalf("a retry: %+v %v, want already present", res, err)
	}
	if got := testutil.ReadFakeLog(t, log); len(got) != 1 {
		t.Fatalf("winget ran %d times, want once", len(got))
	}
}

func TestAppendPath(t *testing.T) {
	got := AppendPath([]string{"A=1", "Path=C:\\a;C:\\B\\"}, "c:\\b;C:\\new;;C:\\new")
	want := []string{"A=1", "Path=C:\\a;C:\\B\\;C:\\new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendPath = %v, want %v", got, want)
	}
	if got := AppendPath(nil, `C:\x`); !reflect.DeepEqual(got, []string{`Path=C:\x`}) {
		t.Fatalf("AppendPath with no PATH = %v", got)
	}
}

func lookIn(found ...string) func(string, []string) (string, error) {
	set := map[string]bool{}
	for _, f := range found {
		set[f] = true
	}
	return func(name string, _ []string) (string, error) {
		if set[name] {
			return "/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func recordRun(argvs *[][]string) func(context.Context, string, []string, []string) (string, string, error) {
	return func(_ context.Context, path string, args, _ []string) (string, string, error) {
		*argvs = append(*argvs, append([]string{filepath.Base(path)}, args...))
		return "tool 1.2.3\n", "", nil
	}
}

// The first method of the order whose package manager is on the PATH runs, however the catalog
// block lists them; a package manager that is not there is skipped; a url is never run.
func TestPick_OrderAndURL(t *testing.T) {
	p := providers.Provider{ID: "x", Docs: "https://example.com", CLI: &providers.CLI{Command: "x"},
		Install: providers.Install{Linux: providers.InstallOS{Pip: "xpip", Npm: "xnpm", Apt: "xapt", Brew: "xbrew", URL: "https://example.com/x.sh", Note: "curl x | sh"}}}
	cases := []struct {
		found  []string
		method string
		prog   string
	}{
		{[]string{"pip", "npm", "apt-get", "brew"}, "brew", "brew"},
		{[]string{"pip", "npm", "apt-get"}, "apt", "apt-get"},
		{[]string{"pip", "npm"}, "npm", "npm"},
		{[]string{"pip3"}, "pip", "pip3"},
	}
	for _, c := range cases {
		plan, ok, err := Pick(p, Deps{GOOS: "linux", LookPath: lookIn(c.found...)})
		if err != nil || !ok || plan.Method != c.method || filepath.Base(plan.Program) != c.prog {
			t.Errorf("found %v: plan %+v ok=%v err=%v, want method %s via %s", c.found, plan, ok, err, c.method, c.prog)
		}
	}
	if _, ok, _ := Pick(p, Deps{GOOS: "linux", LookPath: lookIn()}); ok {
		t.Error("a plan with no package manager on the PATH")
	}
	onlyURL := providers.Provider{ID: "y", CLI: &providers.CLI{Command: "y"}, Install: providers.Install{Linux: providers.InstallOS{URL: "https://example.com/y.sh"}}}
	var ran [][]string
	d := Deps{GOOS: "linux", LookPath: lookIn("curl", "sh", "bash"), Run: recordRun(&ran)}
	res, err := Install(onlyURL, d)
	if err != nil || res.Status != Manual || len(ran) != 0 {
		t.Fatalf("url-only provider: res=%+v err=%v ran=%v, want manual and nothing run", res, err, ran)
	}
	w := providers.Provider{ID: "w", CLI: &providers.CLI{Command: "w"}, Install: providers.Install{Windows: providers.InstallOS{Scoop: "wscoop", Winget: "W.Winget"}}}
	plan, ok, _ := Pick(w, Deps{GOOS: "windows", LookPath: lookIn("scoop", "winget")})
	if !ok || plan.Method != "winget" {
		t.Errorf("windows plan = %+v, want winget before scoop", plan)
	}
	plan, ok, _ = Pick(w, Deps{GOOS: "windows", LookPath: lookIn("scoop", "winget"), Masked: func(n string) bool { return n == "winget" }})
	if !ok || plan.Method != "scoop" {
		t.Errorf("masked winget: plan = %+v, want scoop", plan)
	}
}

// Every method's argv ends with the catalog's id as one element, and a hostile id is never run.
func TestMethodArgs_OneIDElement(t *testing.T) {
	for _, m := range Order {
		args, ok := MethodArgs(m, "pkg-id")
		n := 0
		for _, a := range args {
			if a == "pkg-id" {
				n++
			}
			if strings.Contains(a, " ") {
				t.Errorf("%s: argument %q holds a space", m, a)
			}
		}
		if !ok || n != 1 || args[len(args)-1] != "pkg-id" && m != "winget" {
			t.Errorf("%s: argv %v holds the id %d times as a whole element, want once", m, args, n)
		}
	}
	if _, ok := MethodArgs("choco", "x"); ok {
		t.Error("choco is not a method")
	}
	for _, bad := range []string{"-g evil", "--source=evil", "a b", "a;b", "$(x)", "a&b", "../x"} {
		p := providers.Provider{ID: "z", CLI: &providers.CLI{Command: "z"}, Install: providers.Install{Linux: providers.InstallOS{Npm: bad}}}
		var ran [][]string
		_, err := Install(p, Deps{GOOS: "linux", LookPath: func(n string, _ []string) (string, error) {
			if n == "npm" {
				return "/bin/npm", nil
			}
			return "", errors.New("no")
		}, Run: recordRun(&ran)})
		if err == nil || len(ran) != 0 {
			t.Errorf("id %q: err=%v ran=%v, want a refusal and nothing run", bad, err, ran)
		}
	}
}
