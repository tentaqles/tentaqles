package envplan

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The PATH and environment layer (layer 2 of the PRD v2 Â§6.1 scenario, ADR-0007): identity
// company-x declares Azure as its only cloud; on the PATH tq builds for it, aws resolves to
// nothing (its install folder is dropped) or to tq's stub (in a folder shared with other
// tools), az and everything else resolve as before, and leaving the folder puts the shell
// back exactly.

// companyX is the Â§6.1 identity: Azure (az, and azure-devops for git) and Claude.
func companyX(t testing.TB, v2 bool) *resolve.Workspace {
	t.Helper()
	m := &manifest.Manifest{Schema: "tentaqles-client-v2", Client: "company-x",
		Identities: map[string]manifest.Identity{"az": {}, "claude": {}},
		Git:        manifest.Git{Provider: "azure-devops", Email: "dev@company-x.example"},
		Cloud:      map[string]any{"provider": "azure"}}
	if v2 {
		m.Guard = "v2"
	}
	return &resolve.Workspace{Name: "company-x", Root: filepath.Join(testutil.TempDir(t), "company-x"), Manifest: m}
}

// fakeExe writes a fake executable: on Windows name is written as given (aws.exe, az.cmd);
// on POSIX the Windows suffix is dropped and the file made executable.
func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		for _, s := range []string{".exe", ".cmd", ".bat"} {
			name = strings.TrimSuffix(name, s)
		}
	}
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\necho fake " + name + "\n"
	if strings.HasSuffix(name, ".cmd") {
		body = "@echo off\r\necho fake " + name + "\r\n"
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func plainFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// globalDirs is a machine's PATH as the fakes lay it out: the AWS CLI's own install folder
// (aws, aws_completer and a library), the Azure CLI's (az as a batch file and a bash script),
// and a tools folder.
func globalDirs(t *testing.T) (awsDir, azDir, toolsDir string) {
	t.Helper()
	base := testutil.TempDir(t)
	awsDir = filepath.Join(base, "Amazon", "AWSCLIV2")
	fakeExe(t, awsDir, "aws.exe")
	fakeExe(t, awsDir, "aws_completer.exe")
	plainFile(t, awsDir, "python311.dll")
	azDir = filepath.Join(base, "Azure", "CLI2", "wbin")
	fakeExe(t, azDir, "az.cmd")
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(azDir, "az"), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	toolsDir = filepath.Join(base, "tools")
	fakeExe(t, toolsDir, "git.exe")
	return
}

func join(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }

func pathEnv(p string) []string {
	env := []string{"PATH=" + p}
	if runtime.GOOS == "windows" {
		env = append(env, "PATHEXT=.COM;.EXE;.BAT;.CMD")
	}
	return env
}

// TestPathLayer_ScenarioAwsNotOnPath is the Â§6.1 scenario at layer 2: the AWS CLI folder is
// dropped from company-x's PATH, so aws resolves to nothing, while az and git still resolve.
func TestPathLayer_ScenarioAwsNotOnPath(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, toolsDir := globalDirs(t)
	l := LayerFor(companyX(t, true))
	if l == nil {
		t.Fatal("guard: v2 must give a layer")
	}
	got, rep := l.FilterPath(join(awsDir, azDir, toolsDir), NativeSyntax())
	if want := join(azDir, toolsDir); got != want {
		t.Fatalf("identity PATH = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(rep.Dropped, []string{awsDir}) || len(rep.Shared) != 0 || rep.MaskMissing {
		t.Fatalf("report = %+v", rep)
	}
	if p, err := LookPath("aws", pathEnv(got)); !errors.Is(err, ErrNotInPath) {
		t.Fatalf("aws resolves on the identity PATH: %q %v", p, err)
	}
	if p, err := LookPath("aws_completer", pathEnv(got)); err == nil {
		t.Fatalf("aws_completer resolves: %q", p)
	}
	for _, n := range []string{"az", "git"} {
		if _, err := LookPath(n, pathEnv(got)); err != nil {
			t.Fatalf("%s no longer resolves: %v", n, err)
		}
	}
	// The same PATH on the global side still holds aws: the fakes are real.
	if _, err := LookPath("aws", pathEnv(join(awsDir, azDir))); err != nil {
		t.Fatalf("fake aws not found on the global PATH: %v", err)
	}
	// Idempotent: filtering the identity PATH again changes nothing.
	if again, _ := l.FilterPath(got, NativeSyntax()); again != got {
		t.Fatalf("not idempotent: %q then %q", got, again)
	}
}

// Without guard: v2 there is no layer: the PATH and the environment are what v1 makes them.
func TestPathLayer_V1HasNoLayer(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	ws := companyX(t, false)
	if LayerFor(ws) != nil || LayerFor(nil) != nil {
		t.Fatal("a layer without guard: v2")
	}
	awsDir, azDir, _ := globalDirs(t)
	cur := map[string]string{"PATH": join(awsDir, azDir), "AWS_PROFILE": "prod"}
	w, n := Plan(ws, State{}, Env{Lookup: env(cur), Names: []string{"PATH", "AWS_PROFILE"}, Syntax: NativeSyntax(), PathOK: true})
	if _, set := w.Set["PATH"]; set || len(w.Unset) != 0 || n != nil {
		t.Fatalf("v1 plan touches PATH or unsets: %+v %v", w, n)
	}
	if !reflect.DeepEqual(w.Set, Desired(ws)) {
		t.Fatalf("v1 plan = %v, want Desired", w.Set)
	}
}

// A folder that holds an out-of-scope CLI beside other tools (npm's global folder, scoop's
// shims, WinGet's Links, /usr/local/bin, ~/.local/bin) is kept; the mask folder goes first
// and its stub answers for aws with command-not-found, exit 127.
func TestPathLayer_SharedDirIsMasked(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	_, azDir, toolsDir := globalDirs(t)
	shared := filepath.Join(testutil.TempDir(t), "npm")
	fakeExe(t, shared, "aws.cmd") // an npm-installed wrapper, say
	fakeExe(t, shared, "node.exe")
	fakeExe(t, shared, "prettier.cmd")
	l := LayerFor(companyX(t, true))

	// No mask folder yet: the shared folder stays, aws still resolves there, and the
	// report says the mask is missing (tq env prints the notice).
	got, rep := l.FilterPath(join(shared, azDir, toolsDir), NativeSyntax())
	if got != join(shared, azDir, toolsDir) || !rep.MaskMissing || !reflect.DeepEqual(rep.Shared, []string{shared}) {
		t.Fatalf("without the mask: %q %+v", got, rep)
	}

	if ch, err := l.WriteMask(); err != nil || !ch {
		t.Fatalf("WriteMask = %v, %v", ch, err)
	}
	if ch, err := l.WriteMask(); err != nil || ch {
		t.Fatalf("second WriteMask changed something: %v, %v", ch, err)
	}
	l = LayerFor(companyX(t, true))
	got, rep = l.FilterPath(join(shared, azDir, toolsDir), NativeSyntax())
	if want := join(l.MaskDir(), shared, azDir, toolsDir); got != want || rep.MaskMissing {
		t.Fatalf("identity PATH = %q, want %q (%+v)", got, want, rep)
	}
	p, err := LookPath("aws", pathEnv(got))
	if err != nil || filepath.Dir(p) != l.MaskDir() {
		t.Fatalf("aws resolves to %q (%v), want the mask stub", p, err)
	}
	for _, n := range []string{"node", "prettier", "az", "git"} {
		if p, err := LookPath(n, pathEnv(got)); err != nil || filepath.Dir(p) == l.MaskDir() {
			t.Fatalf("%s: %q %v", n, p, err)
		}
	}
	for _, n := range []string{"az", "azd", "claude", "git", "node"} {
		if fileExists(filepath.Join(l.MaskDir(), n)) {
			t.Fatalf("the mask hides the in-scope or unrelated %s", n)
		}
	}
	runStub(t, got, "aws", "aws: command not found (outside the scope of identity company-x)")
	if again, _ := l.FilterPath(got, NativeSyntax()); again != got {
		t.Fatalf("not idempotent: %q then %q", got, again)
	}
}

// runStub runs name through the OS shell on path and checks the stub's answer: exit 127 and
// "command not found" on stderr. On Windows it also runs it from Git Bash when installed,
// which resolves the extensionless stub, not the batch file.
func runStub(t *testing.T, path, name, msg string) {
	t.Helper()
	environ := func() []string {
		var out []string
		for _, kv := range os.Environ() {
			if k, _, _ := splitKV(kv); envKey(k) != envKey("PATH") {
				out = append(out, kv)
			}
		}
		return append(out, "PATH="+path)
	}
	check := func(what string, c *exec.Cmd) {
		c.Env = environ()
		out, err := c.CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 127 {
			t.Fatalf("%s: %s: err=%v out=%q, want exit 127", what, name, err, out)
		}
		if !strings.Contains(string(out), strings.SplitN(msg, " (", 2)[0]) {
			t.Fatalf("%s: %s: output %q lacks %q", what, name, out, msg)
		}
	}
	if runtime.GOOS == "windows" {
		check("cmd", exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/c", name))
		if bash := `C:\Program Files\Git\bin\bash.exe`; fileExists(bash) {
			check("git bash", exec.Command(bash, "-c", name))
		}
		return
	}
	check("sh", exec.Command("/bin/sh", "-c", name))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// A mask folder that holds a file tq did not write (a git in front of the real one) is never
// put on the PATH; neither is one missing a stub.
func TestPathLayer_TamperedMaskIsNotUsed(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	shared := filepath.Join(testutil.TempDir(t), "bin")
	fakeExe(t, shared, "aws.exe")
	fakeExe(t, shared, "node.exe")
	l := LayerFor(companyX(t, true))
	if _, err := l.WriteMask(); err != nil {
		t.Fatal(err)
	}
	plainFile(t, l.MaskDir(), "git.cmd")
	l = LayerFor(companyX(t, true))
	got, rep := l.FilterPath(shared, NativeSyntax())
	if got != shared || !rep.MaskMissing {
		t.Fatalf("a tampered mask was used: %q %+v", got, rep)
	}
	if err := os.Remove(filepath.Join(l.MaskDir(), "git.cmd")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(l.MaskDir(), "aws")); err != nil {
		t.Fatal(err)
	}
	if LayerFor(companyX(t, true)).MaskReady() {
		t.Fatal("a mask missing a stub is ready")
	}
	if had, err := RemoveMask("company-x"); err != nil || !had || fileExists(l.MaskDir()) {
		t.Fatalf("RemoveMask = %v %v", had, err)
	}
}

// Entries tq cannot judge are kept as they are: empty, relative, missing, a file.
func TestPathLayer_KeepsWhatItCannotJudge(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, _, _ := globalDirs(t)
	l := LayerFor(companyX(t, true))
	missing := filepath.Join(testutil.TempDir(t), "nope")
	completer := filepath.Join(awsDir, "aws_completer")
	if runtime.GOOS == "windows" {
		completer += ".exe"
	}
	in := join("", ".", missing, completer, awsDir)
	got, _ := l.FilterPath(in, NativeSyntax())
	if want := join("", ".", missing, completer); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Git Bash on Windows spells PATH /c/Users/...:/usr/bin; tq reads it paired with the OS
// spelling the MSYS runtime gives it, and answers in the shell's spelling. The pairing is
// plain data, so the test runs on every OS.
func TestPathSyntax_MSYS(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, toolsDir := globalDirs(t)
	shell := "/opt/aws/bin:/usr/bin:/opt/az:/opt/tools"
	osp := strings.Join([]string{awsDir, `C:\Program Files\Git\usr\bin`, azDir, toolsDir}, ";")
	syn := MSYSSyntax(shell, osp)
	l := LayerFor(companyX(t, true))
	got, rep := l.FilterPath(shell, syn)
	if got != "/usr/bin:/opt/az:/opt/tools" || !reflect.DeepEqual(rep.Dropped, []string{"/opt/aws/bin"}) {
		t.Fatalf("got %q %+v", got, rep)
	}
	// Unpaired spellings: /x/rest is X:\rest, anything else is unknown (kept).
	un := MSYSSyntax("/c/a:/d", "only-one")
	if g := un.ToOS("/c/Users/x"); g != `C:\`+filepath.FromSlash("Users/x") {
		t.Fatalf("ToOS(/c/Users/x) = %q", g)
	}
	if g := un.ToOS("/usr/bin"); g != "" {
		t.Fatalf("ToOS(/usr/bin) = %q, want unknown", g)
	}
	if g := un.FromOS(`C:\Users\me\.tentaqles\identities\x\path-mask`); runtime.GOOS == "windows" && g != "/c/Users/me/.tentaqles/identities/x/path-mask" {
		t.Fatalf("FromOS = %q", g)
	}
}

func TestMergePath(t *testing.T) {
	cases := []struct{ name, orig, set, cur, want string }{
		{"unchanged", "/a:/aws:/b", "/mask:/a:/b", "/mask:/a:/b", "/a:/aws:/b"},
		{"prepended", "/a:/aws:/b", "/mask:/a:/b", "/home/me/bin:/mask:/a:/b", "/home/me/bin:/a:/aws:/b"},
		{"appended", "/a:/aws:/b", "/a:/b", "/a:/b:/opt/x", "/a:/aws:/b:/opt/x"},
		{"removed", "/a:/aws:/b", "/a:/b", "/b", "/aws:/b"},
	}
	for _, c := range cases {
		if got := MergePath(c.orig, c.set, c.cur, ":"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// shellSim applies Ops to a map, the way the eval'd shell lines do.
func shellSim(m map[string]string, ops Ops) {
	for _, k := range ops.Unset {
		delete(m, k)
	}
	for k, v := range ops.Set {
		m[k] = v
	}
}

func names(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Entering company-x filters PATH and unsets the out-of-scope variables (AWS_PROFILE, a
// TF_TOKEN_* prefix entry, GITHUB_TOKEN: gh is not in scope) while the in-scope ones stay;
// leaving restores the shell exactly, in both PATH separators.
func TestLayer_EnterAndLeaveRestoresExactly(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, toolsDir := globalDirs(t)
	ws := companyX(t, true)
	msys := "/x/aws:/x/az:/x/tools"
	cases := []struct {
		syn  PathSyntax
		path string
	}{
		{NativeSyntax(), join(awsDir, azDir, toolsDir)},
		{MSYSSyntax(msys, strings.Join([]string{awsDir, azDir, toolsDir}, ";")), msys},
	}
	for _, c := range cases {
		syn, path := c.syn, c.path
		orig := map[string]string{"PATH": path, "AWS_PROFILE": "prod", "TF_TOKEN_app_terraform_io": "t",
			"GITHUB_TOKEN": "g", "AZURE_TENANT_ID": "tenant", "HOME": "/home/me"}
		sh := map[string]string{}
		for k, v := range orig {
			sh[k] = v
		}
		step := func(w *resolve.Workspace, prev State) State {
			e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
			want, _ := Plan(w, prev, e)
			ops, _ := DiffWant(want, e.Lookup, prev, func() string {
				if w == nil {
					return ""
				}
				return w.Name
			}())
			shellSim(sh, ops)
			return DecodeState(sh[StateVar])
		}
		st := step(ws, State{})
		for _, k := range []string{"AWS_PROFILE", "TF_TOKEN_app_terraform_io", "GITHUB_TOKEN"} {
			if _, set := sh[k]; set {
				t.Fatalf("%s: %s survives in the identity environment", syn.Sep, k)
			}
		}
		if sh["AZURE_TENANT_ID"] != "tenant" || sh["HOME"] != "/home/me" || sh["TQ_WS"] != "company-x" {
			t.Fatalf("%s: in-scope or unrelated variables changed: %v", syn.Sep, sh)
		}
		if strings.Contains(sh["PATH"], "aws") {
			t.Fatalf("%s: identity PATH holds the AWS folder: %q", syn.Sep, sh["PATH"])
		}
		// The next prompt in the same folder changes nothing.
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
		w2, _ := Plan(ws, st, e)
		if ops, _ := DiffWant(w2, e.Lookup, st, "company-x"); ops.Changed {
			t.Fatalf("%s: a second prompt changes %v / %v", syn.Sep, ops.Set, ops.Unset)
		}
		// Leave: exact restore.
		step(nil, st)
		if !reflect.DeepEqual(sh, orig) {
			t.Fatalf("%s: after leaving:\n got %v\nwant %v", syn.Sep, sh, orig)
		}
	}
}

// A PATH entry the user adds inside the identity survives leaving it (MergePath), and the
// AWS folder comes back.
func TestLayer_UserPathChangeSurvivesLeave(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, toolsDir := globalDirs(t)
	ws := companyX(t, true)
	sh := map[string]string{"PATH": join(awsDir, azDir, toolsDir)}
	syn := NativeSyntax()
	run := func(w *resolve.Workspace, name string, prev State) State {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
		want, _ := Plan(w, prev, e)
		ops, _ := DiffWant(want, e.Lookup, prev, name)
		shellSim(sh, ops)
		return DecodeState(sh[StateVar])
	}
	st := run(ws, "company-x", State{})
	mine := filepath.Join(testutil.TempDir(t), "mybin")
	sh["PATH"] = join(mine, sh["PATH"])
	st = run(ws, "company-x", st)
	if !strings.HasPrefix(sh["PATH"], mine) || strings.Contains(sh["PATH"], awsDir) {
		t.Fatalf("in-session PATH = %q", sh["PATH"])
	}
	run(nil, "", st)
	if want := join(mine, awsDir, azDir, toolsDir); sh["PATH"] != want {
		t.Fatalf("after leaving PATH = %q, want %q", sh["PATH"], want)
	}
}

// Moving straight from company-x to an AWS identity builds the second PATH from the PATH
// before tq, so aws is back; and an AWS variable is no longer unset.
func TestLayer_SwitchIdentityRebuildsFromOriginal(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, _ := globalDirs(t)
	cx := companyX(t, true)
	awsWS := &resolve.Workspace{Name: "beta", Root: filepath.Join(testutil.TempDir(t), "beta"), Manifest: &manifest.Manifest{
		Schema: "tentaqles-client-v2", Client: "beta", Guard: "v2", Identities: map[string]manifest.Identity{"aws": {}}}}
	sh := map[string]string{"PATH": join(awsDir, azDir), "AWS_PROFILE": "prod"}
	syn := NativeSyntax()
	run := func(w *resolve.Workspace, prev State) State {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: syn, PathOK: true}
		want, _ := Plan(w, prev, e)
		ops, _ := DiffWant(want, e.Lookup, prev, w.Name)
		shellSim(sh, ops)
		return DecodeState(sh[StateVar])
	}
	st := run(cx, State{})
	if strings.Contains(sh["PATH"], awsDir) {
		t.Fatal("aws on company-x's PATH")
	}
	run(awsWS, st)
	if !strings.Contains(sh["PATH"], awsDir) || sh["AWS_PROFILE"] != "prod" {
		t.Fatalf("beta: PATH %q AWS_PROFILE %q", sh["PATH"], sh["AWS_PROFILE"])
	}
	if strings.Contains(sh["PATH"], azDir) {
		t.Fatalf("beta's PATH holds the Azure CLI folder: %q", sh["PATH"])
	}
}

// A launch (tq run, an editor) gets the same layer: Environ filters PATH, keeps its key's
// spelling (Path on Windows), removes the out-of-scope variables and restores what an
// earlier identity of the shell changed.
func TestEnviron_V2Layer(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, toolsDir := globalDirs(t)
	key := "PATH"
	if runtime.GOOS == "windows" {
		key = "Path"
	}
	base := []string{key + "=" + join(awsDir, azDir, toolsDir), "AWS_ACCESS_KEY_ID=x", "AZURE_TENANT_ID=t", "HOME=/h"}
	out := Environ(companyX(t, true), base)
	got := map[string]string{}
	for _, kv := range out {
		k, v, _ := splitKV(kv)
		got[k] = v
	}
	if got[key] != join(azDir, toolsDir) {
		t.Fatalf("%s = %q", key, got[key])
	}
	if _, ok := got["AWS_ACCESS_KEY_ID"]; ok {
		t.Fatal("AWS_ACCESS_KEY_ID survives the launch environment")
	}
	if got["AZURE_TENANT_ID"] != "t" || got["TQ_WS"] != "company-x" {
		t.Fatalf("launch env = %v", got)
	}
	if p, err := LookPath("aws", out); err == nil {
		t.Fatalf("aws resolves in the launch env: %s", p)
	}
	// v1: untouched PATH, variables kept.
	v1 := Environ(companyX(t, false), base)
	if !containsAll(v1, base) {
		t.Fatalf("v1 launch env changed: %v", v1)
	}
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// The variables unset come from the match lists of the providers outside the scope; a
// variable an in-scope provider claims (AZURE_CONFIG_DIR is az's and azure-devops's) and one
// tq sets itself are never unset.
func TestLayer_UnsetNames(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	l := LayerFor(companyX(t, true))
	got := l.UnsetNames([]string{"AWS_PROFILE", "AZURE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "TF_TOKEN_x", "TF_TOKEN_", "GH_TOKEN", "PATH", "HOME", "aws_profile"},
		map[string]string{"GH_TOKEN": "set by tq"})
	want := []string{"AWS_PROFILE", "TF_TOKEN_x", "aws_profile"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnsetNames = %v, want %v", got, want)
	}
}
