package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// isolateHome points TQ_HOME and the OS home at temp dirs so tests never touch the real
// developer home (in particular ~/.gitconfig-tentaqles), and clears the identity a surrounding
// tq-managed shell may carry.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	unsetenv(t, envplan.StateVar)
	unsetenv(t, "TQ_WS")
	unsetenv(t, "TQ_WS_ROOT")
	unsetenv(t, "TQ_ENABLED")
	unsetenv(t, "CLAUDECODE")
	unsetenv(t, "CLAUDE_CONFIG_DIR")
	return home
}

// unsetenv removes k from the process environment for the duration of the test (t.Setenv can
// only set, and an empty value is still "present" to os.LookupEnv).
func unsetenv(t *testing.T, k string) {
	t.Helper()
	v, ok := os.LookupEnv(k)
	if !ok {
		return
	}
	if err := os.Unsetenv(k); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv(k, v) })
}

// newBase registers a fresh clients folder in a fresh home and returns it.
func newBase(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	base := testutil.TempDir(t)
	cfg := &registry.Config{}
	if _, err := cfg.AddBase(base); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return base
}

// writeClient writes client name in base as reach leaves it on disk: the marker in the folder and
// the manifest (body after the schema and client lines) under paths.ManifestPath. It returns the
// folder.
func writeClient(t *testing.T, base, name, body string) string {
	t.Helper()
	root := filepath.Join(base, name)
	writeTB(t, filepath.Join(root, manifest.FileName), string(manifest.MarkerBytes(name)))
	writeTB(t, paths.ManifestPath(name), "schema: tentaqles-client-v2\nclient: "+name+"\n"+body)
	return root
}

// launched is one call of the execCommand seam.
type launched struct {
	name string
	args []string
	env  map[string]string
}

// String names the call without its env: a failure message must never print the environment.
func (l launched) String() string { return l.name + " " + strings.Join(l.args, " ") }

// prog is a launched command's program name: tentaqles run resolves the command on the identity PATH.
func prog(name string) string { return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)) }

// fakeExec replaces execCommand for the test and returns the recorded calls.
func fakeExec(t *testing.T) *[]launched {
	t.Helper()
	var calls []launched
	prev := execCommand
	bin := testutil.TempDir(t)
	for _, n := range []string{"claude", "codex", "npm", "gh"} {
		if runtime.GOOS == "windows" {
			n += ".exe"
		}
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	execCommand = func(name string, args []string, env []string) error {
		m := map[string]string{}
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			m[strings.ToUpper(k)] = v
		}
		calls = append(calls, launched{name, append([]string(nil), args...), m})
		return nil
	}
	t.Cleanup(func() { execCommand = prev })
	return &calls
}

// mkLaunchWS writes client name with a claude identity and its guard plugin installed, as reach
// leaves it; it returns the folder.
func mkLaunchWS(t *testing.T, name string) string {
	t.Helper()
	return mkWorkspacesInOneBase(t, name)[name]
}

// mkWorkspacesInOneBase writes one client per name in one clients folder, each with a claude
// identity and its guard plugin. It returns each folder by name.
func mkWorkspacesInOneBase(t *testing.T, names ...string) map[string]string {
	t.Helper()
	base := newBase(t)
	roots := map[string]string{}
	for _, name := range names {
		roots[name] = writeClient(t, base, name, "git: { email: "+name+"@example.com }\nidentities: { claude: {} }\n")
		writeGuard(t, name, guardOpts{})
	}
	return roots
}

func runTq(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestLaunchClaudeArgs(t *testing.T) {
	cases := []struct {
		in    []string
		agent bool
		want  string
	}{
		{[]string{"-p", "hi"}, false, "--dangerously-skip-permissions -p hi"},
		{nil, false, "--dangerously-skip-permissions"},
		{[]string{"--permission-mode", "plan", "-p", "hi"}, false, "--permission-mode plan -p hi"},
		{[]string{"--permission-mode=acceptEdits"}, false, "--permission-mode=acceptEdits"},
		{[]string{"--dangerously-skip-permissions", "-p", "x"}, false, "--dangerously-skip-permissions -p x"},
		{[]string{"--version"}, false, "--version"},
		{[]string{"-h"}, false, "-h"},
		{[]string{"mcp", "list"}, false, "mcp list"},
		{[]string{"update"}, false, "update"},
		// A nested claude, started from a Claude Code session, never gets bypass from the launcher.
		{[]string{"-p", "hi"}, true, "-p hi"},
		// A prompt that happens to say "update" is not a subcommand.
		{[]string{"-p", "update the docs"}, false, "--dangerously-skip-permissions -p update the docs"},
	}
	for _, c := range cases {
		got := launchClaudeArgs(c.in, c.agent)
		if strings.Join(got, " ") != c.want {
			t.Errorf("launchClaudeArgs(%q, agent=%v) = %q; want %q", c.in, c.agent, got, c.want)
		}
	}
}

// tentaqles run <client> -- claude starts claude with the client's identity: its own Claude config home.
func TestRunClaudeHasTheClientsIdentity(t *testing.T) {
	mkLaunchWS(t, "acme")
	calls := fakeExec(t)
	if out, err := runTq(t, "run", "acme", "--", "claude", "-p", "hi"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(*calls) != 1 || prog((*calls)[0].name) != "claude" {
		t.Fatalf("calls %+v", *calls)
	}
	c := (*calls)[0]
	if got := strings.Join(c.args, " "); got != "--dangerously-skip-permissions -p hi" {
		t.Fatalf("args %q", got)
	}
	if c.env["TQ_WS"] != "acme" || !samePathT(c.env["CLAUDE_CONFIG_DIR"], paths.IdentityDir("acme", "claude")) {
		t.Fatalf("identity env: TQ_WS=%q CLAUDE_CONFIG_DIR=%q", c.env["TQ_WS"], c.env["CLAUDE_CONFIG_DIR"])
	}
}

func samePathT(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// tentaqles run . is the client of the folder (any subfolder), and in a neutral folder no client: no
// other client's Claude config home leaks into it.
func TestRunDotResolvesCwd(t *testing.T) {
	root := mkLaunchWS(t, "acme")
	sub := filepath.Join(root, "app", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	calls := fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if (*calls)[0].env["TQ_WS"] != "acme" {
		t.Fatalf("want acme's identity: TQ_WS=%q", (*calls)[0].env["TQ_WS"])
	}

	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "/other/client")
	calls = fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	c := (*calls)[0]
	if _, ok := c.env["TQ_WS"]; ok {
		t.Fatalf("neutral run carries a client: TQ_WS=%q", c.env["TQ_WS"])
	}
	if _, ok := c.env["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("another client's config dir leaked")
	}
}

// From a Claude Code session in the client's folder, tentaqles run <its name> and tentaqles run . launch with
// its identity and without the bypass flag.
func TestRunFromAgentSession(t *testing.T) {
	root := mkLaunchWS(t, "acme")
	t.Setenv("CLAUDECODE", "1")
	chdir(t, root)
	calls := fakeExec(t)
	for i, arg := range []string{"acme", "."} {
		if _, err := runTq(t, "run", arg, "--", "claude", "-p", "x"); err != nil {
			t.Fatal(err)
		}
		if c := (*calls)[i]; strings.Join(c.args, " ") != "-p x" || c.env["TQ_WS"] != "acme" {
			t.Fatalf("agent launch %s: %+v", arg, c)
		}
	}
}

// Everything after the command word belongs to the command, with or without `--`.
func TestRunCommandFlagsAreNotTqFlags(t *testing.T) {
	mkLaunchWS(t, "acme")
	for _, args := range [][]string{
		{"run", "acme", "--", "npm", "test", "-p", "x", "--watch"},
		{"run", "acme", "npm", "test", "-p", "x", "--watch"},
	} {
		calls := fakeExec(t)
		if out, err := runTq(t, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if c := (*calls)[0]; prog(c.name) != "npm" || strings.Join(c.args, " ") != "test -p x --watch" {
			t.Fatalf("%v: %v", args, c)
		}
	}
}

// otherClientRefusal is the exact refusal of a session's tentaqles run <other>.
func otherClientRefusal(name string) string {
	return "tentaqles run: " + name + " is another client. A Claude Code session can only run under the client of its own folder."
}

// A Claude Code session launches only the client of the folder it is in: tentaqles run <other> refuses
// and nothing launches, from another client's folder and from a neutral one.
func TestRunOtherClient_RefusedForAgents(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	t.Setenv("CLAUDECODE", "1")
	calls := fakeExec(t)
	for _, from := range []string{roots["alpha"], t.TempDir()} {
		chdir(t, from)
		_, err := runTq(t, "run", "beta", "--", "gh", "pr", "list")
		if err == nil || err.Error() != otherClientRefusal("beta") {
			t.Fatalf("from %s: tentaqles run beta must refuse in a session, got %v", from, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("another client launched: %+v", *calls)
	}
}

// A neutral folder is no client's folder: a session there refuses tentaqles run of any client.
func TestRunOtherClient_NeutralFolderRefused(t *testing.T) {
	mkWorkspacesInOneBase(t, "alpha", "beta")
	t.Setenv("CLAUDECODE", "1")
	chdir(t, t.TempDir())
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "beta", "--", "az", "group", "list"); err == nil || err.Error() != otherClientRefusal("beta") {
		t.Fatalf("tentaqles run beta from a neutral folder must refuse, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a neutral folder launched beta: %+v", *calls)
	}
}

// The refusal reads the folder, not a variable a session may set: TQ_WS naming beta does not
// help alpha's session.
func TestRunOtherClient_InheritedEnvIgnored(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("TQ_WS", "beta")
	t.Setenv("TQ_WS_ROOT", roots["beta"])
	chdir(t, roots["alpha"])
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "beta", "--", "claude"); err == nil || err.Error() != otherClientRefusal("beta") {
		t.Fatalf("an inherited TQ_WS must not allow beta, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("beta launched: %+v", *calls)
	}
}

// The developer's own terminal runs any client from any folder.
func TestRunOtherClient_HumanAllowed(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	calls := fakeExec(t)
	for i, from := range []string{roots["alpha"], t.TempDir()} {
		chdir(t, from)
		if out, err := runTq(t, "run", "beta", "--", "claude", "-p", "hi"); err != nil {
			t.Fatalf("from %s: %v %s", from, err, out)
		}
		if len(*calls) != i+1 || (*calls)[i].env["TQ_WS"] != "beta" {
			t.Fatalf("from %s: %+v", from, *calls)
		}
	}
}

// A CLI of a provider outside the client's scope does not run through tentaqles run, however it is
// spelled; one in scope does.
func TestRun_OutOfScopeCLIRefused(t *testing.T) {
	base := newBase(t)
	writeClient(t, base, "acme", "git: { email: a@acme.test }\nidentities: { az: {} }\ncloud: { provider: azure }\n")
	calls := fakeExec(t)
	tools := testutil.TempDir(t)
	for _, n := range []string{"az", "aws"} {
		if runtime.GOOS == "windows" {
			n += ".exe"
		}
		writeTB(t, filepath.Join(tools, n), "#!/bin/sh\n")
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"aws", "aws.exe"} {
		if _, err := runTq(t, "run", "acme", "--", name, "s3", "ls"); err == nil || !strings.Contains(err.Error(), "outside its scope") {
			t.Fatalf("tentaqles run acme -- %s: %v", name, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("an out-of-scope CLI launched: %+v", *calls)
	}
	if out, err := runTq(t, "run", "acme", "--", "az", "group", "list"); err != nil || len(*calls) != 1 {
		t.Fatalf("az in scope: %v %s %+v", err, out, *calls)
	}
}
