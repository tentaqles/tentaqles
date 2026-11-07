package cmd

import (
	"bytes"
	"encoding/json"
	"github.com/zalando/go-keyring"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// isolateHome points TQ_HOME and the OS home at temp dirs so tests never touch
// the real developer home (in particular ~/.gitconfig-tentaqles).
func isolateHome(t *testing.T) string {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	// Git for Windows reads HOME before USERPROFILE, and a Git Bash or Claude
	// Code session sets HOME: isolate both so no test reaches ~/.gitconfig.
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	// The test binary may itself run inside a tq-managed shell (a Claude Code
	// session, `tq run`), which carries the state of the last switch in
	// __TQ_STATE. `tq env` would then "restore" that shell's variables and
	// emit unsets for a neutral cwd, so start every test from a clean slate.
	unsetenv(t, envplan.StateVar)
	// And the workspace markers it carries: TQ_WS without a state is a lost state (ADR-0008).
	unsetenv(t, "TQ_WS")
	unsetenv(t, "TQ_WS_ROOT")
	keyring.MockInit() // never the real OS keychain in tests
	// Nor from a shell with the activation kill switch set.
	unsetenv(t, "TQ_ENABLED")
	return home
}

// unsetenv removes k from the process environment for the duration of the
// test (t.Setenv can only set, and an empty value is still "present" to
// os.LookupEnv).
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

// mkUntrusted registers a base and writes a workspace manifest by hand, without
// ever calling tq allow.
func mkUntrusted(t *testing.T, name string) string {
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
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.FileName),
		[]byte("schema: tentaqles-client-v2\nclient: "+name+"\ngit: { email: "+name+"@x.io }\nidentities: { gh: {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRun_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"run", "evil", "--", "cmd"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

func TestLogin_RefusesUntrusted(t *testing.T) {
	mkUntrusted(t, "evil")
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "evil", "gh"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for an untrusted workspace")
	}
	if !strings.Contains(err.Error(), "untrusted") {
		t.Fatalf("error must mention untrusted: %v", err)
	}
}

// --- the launcher ---------------------------------------------------------

// clearAgentMarkers unsets every agent marker (the test binary may run under Claude Code).
func clearAgentMarkers(t *testing.T) {
	t.Helper()
	for _, k := range agentMarkers {
		unsetenv(t, k)
	}
}

// launched is one call of the execCommand seam.
type launched struct {
	name string
	args []string
	env  map[string]string
}

// String names the call without its env: a failure message must never print
// the environment (it carries the developer's tokens).
func (l launched) String() string {
	return l.name + " " + strings.Join(l.args, " ")
}

// fakeExec replaces execCommand for the test and returns the recorded calls.
// prog is a launched command's program name: tq run resolves the command on the identity PATH.
func prog(name string) string {
	return strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
}

func fakeExec(t *testing.T) *[]launched {
	t.Helper()
	var calls []launched
	prev := execCommand
	// tq run resolves its command on the identity PATH; these exist on every machine.
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

// mkLaunchWS writes and trusts a workspace with claude and codex identities,
// in the old layout (the manifest in the root); it returns the root. A
// non-empty permMode writes the claude.permission_mode an older manifest
// carried, which tq no longer reads.
func mkLaunchWS(t *testing.T, name, permMode string) string {
	t.Helper()
	root, _ := mkLaunchWSLayout(t, name, permMode, false)
	return root
}

// mkLaunchWSLayout is mkLaunchWS with a choice of layout: protected writes
// the name-only marker in the root and the manifest under
// paths.ManifestPath(name) (ADR-0002). It returns the root and the file the
// trust hash is taken from. The client has the tentaqles plugin (the guard,
// ADR-0018) as a real install leaves it; mkLaunchWSGuard says otherwise.
func mkLaunchWSLayout(t *testing.T, name, permMode string, protected bool) (string, string) {
	t.Helper()
	return mkLaunchWSGuard(t, name, permMode, protected, guardOpts{})
}

// mkLaunchWSGuard is mkLaunchWSLayout with the client's guard as g says (guardplugin_test.go).
func mkLaunchWSGuard(t *testing.T, name, permMode string, protected bool, g guardOpts) (string, string) {
	t.Helper()
	root := mkUntrusted(t, name)
	ids := g.identities()
	body := "schema: tentaqles-client-v2\nclient: " + name + "\ngit: { email: tester@example.com }\nidentities: " + ids + "\n"
	if permMode != "" {
		body += "claude: { permission_mode: " + permMode + " }\n"
	}
	mp := filepath.Join(root, manifest.FileName)
	if protected {
		if err := os.WriteFile(mp, manifest.MarkerBytes(name), 0o644); err != nil {
			t.Fatal(err)
		}
		mp = paths.ManifestPath(name)
		if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Written before the signature: the record covers the plugin's files and the settings' keys.
	writeGuard(t, name, g)
	h, err := trust.HashFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow(name, h); err != nil {
		t.Fatal(err)
	}
	return root, mp
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
		// An agent's nested claude never gets bypass from the launcher.
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

// The launcher starts claude in bypass, also for an older manifest that
// still carries a claude.permission_mode (ignored, manifest.DroppedClaudeKeys).
func TestRunClaudeAlwaysBypass(t *testing.T) {
	for _, pm := range []string{"", "default", "acceptEdits", "plan", "auto", "dontAsk", "bypass"} {
		t.Run("mode="+pm, func(t *testing.T) {
			mkLaunchWS(t, "acme", pm)
			clearAgentMarkers(t)
			calls := fakeExec(t)
			if out, err := runTq(t, "run", "acme", "--", "claude", "-p", "hi"); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if len(*calls) != 1 || prog((*calls)[0].name) != "claude" {
				t.Fatalf("calls %+v", *calls)
			}
			if got := strings.Join((*calls)[0].args, " "); got != "--dangerously-skip-permissions -p hi" {
				t.Fatalf("args %q", got)
			}
			if (*calls)[0].env["TQ_WS"] != "acme" || (*calls)[0].env["CLAUDE_CONFIG_DIR"] == "" {
				t.Fatalf("identity env missing: TQ_WS=%q", (*calls)[0].env["TQ_WS"])
			}
		})
	}
}

func TestRunDotResolvesCwd(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	sub := filepath.Join(root, "app")
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

	// neutral folder: no identity, still the launcher
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "/other/client")
	calls = fakeExec(t)
	if out, err := runTq(t, "run", ".", "--", "claude"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	c := (*calls)[0]
	if _, ok := c.env["TQ_WS"]; ok {
		t.Fatalf("neutral run carries a workspace: TQ_WS=%q", c.env["TQ_WS"])
	}
	if _, ok := c.env["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("another client's config dir leaked")
	}
	if strings.Join(c.args, " ") != "--dangerously-skip-permissions" {
		t.Fatalf("args %q", c.args)
	}
}

// tq run . in an untrusted workspace refuses: nothing launches, with or
// without an identity (PRD v2 §8.3). A never-trusted folder names tq allow;
// a mismatch names the changed file and tq hooks verify
// (TestRun_RefusesOnIntegrityMismatch).
func TestRunDotUntrustedRefuses(t *testing.T) {
	root := mkUntrusted(t, "evil")
	clearAgentMarkers(t)
	chdir(t, root)
	calls := fakeExec(t)
	_, err := runTq(t, "run", ".", "--", "claude")
	if err == nil || !strings.Contains(err.Error(), "workspace evil is untrusted (run: tq allow evil)") {
		t.Fatalf("want the refusal, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may launch: %+v", *calls)
	}
}

// An agent's nested launch gets the identity env and no bypass flag. The agent
// runs from its own workspace's folder: tq run <its name> and tq run . both
// launch with its identity. Another name is refused
// (TestRunOtherClient_RefusedForAgents).
func TestRunFromAgentSession(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	clearAgentMarkers(t)
	t.Setenv("CLAUDECODE", "1")
	chdir(t, root)
	calls := fakeExec(t)
	if _, err := runTq(t, "run", "acme", "--", "claude", "-p", "x"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("tq run acme: want one launch, got %+v", *calls)
	}
	if c := (*calls)[0]; strings.Join(c.args, " ") != "-p x" || c.env["TQ_WS"] != "acme" {
		t.Fatalf("agent launch: %+v", c)
	}
	if _, err := runTq(t, "run", ".", "--", "claude", "-p", "x"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("tq run .: want a second launch, got %+v", *calls)
	}
	if c := (*calls)[1]; strings.Join(c.args, " ") != "-p x" || c.env["TQ_WS"] != "acme" {
		t.Fatalf("agent launch by dot: %+v", c)
	}
}

// The PowerShell tq function cannot pass a literal `--` (the parameter
// binder eats it), so tq run never parses the command's own flags as its
// own: everything after the command word belongs to the command, with or
// without `--`.
func TestRunCommandFlagsAreNotTqFlags(t *testing.T) {
	isolateHome(t)
	clearAgentMarkers(t)
	mkLaunchWS(t, "acme", "")
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

// mkWorkspacesInOneBase writes and trusts one workspace per name in one base,
// so they share one state folder and registry (mkLaunchWS isolates a new home
// on each call, which would drop the first). It returns each root by name.
func mkWorkspacesInOneBase(t *testing.T, names ...string) map[string]string {
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
	roots := map[string]string{}
	for _, name := range names {
		root := filepath.Join(base, name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "schema: tentaqles-client-v2\nclient: " + name + "\ngit: { email: " + name + "@example.com }\nidentities: { claude: {} }\n"
		mp := filepath.Join(root, manifest.FileName)
		if err := os.WriteFile(mp, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		h, err := trust.HashFile(mp)
		if err != nil {
			t.Fatal(err)
		}
		if err := trust.Allow(name, h); err != nil {
			t.Fatal(err)
		}
		roots[name] = root
	}
	return roots
}

// harnessChain is a process tree with claude.exe in it: this process <- bash <-
// claude.exe <- WindowsTerminal.exe. It is the process-tree detector's agent.
func harnessChain(pid int) (humanonly.Parent, bool) {
	switch pid {
	case os.Getpid():
		return humanonly.Parent{PID: 90, Name: "bash.exe", Cmdline: "bash", CmdlineKnown: true}, true
	case 90:
		return humanonly.Parent{PID: 80, Name: "claude.exe", Cmdline: "claude", CmdlineKnown: true}, true
	case 80:
		return humanonly.Parent{PID: 70, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}, true
	}
	return humanonly.Parent{}, false
}

// setAgentDetector makes one agent detector fire, from a human baseline: an
// environment marker (one name of agentMarkers, alone) or the process tree
// (humanonly.DetectorProcessTree, claude.exe in the chain, no marker). "" is
// the human baseline. The seams are restored at cleanup.
func setAgentDetector(t *testing.T, detector string) {
	t.Helper()
	clearAgentMarkers(t)
	prev := humanonly.ParentOf
	t.Cleanup(func() { humanonly.ParentOf = prev })
	humanonly.ParentOf = terminalChain
	switch {
	case detector == "":
	case detector == humanonly.DetectorProcessTree:
		humanonly.ParentOf = harnessChain
	case slices.Contains(agentMarkers, detector):
		t.Setenv(detector, "1")
	default:
		t.Fatalf("unknown agent detector %q", detector)
	}
}

// agentDetectors are every detector that finds an agent: each marker alone,
// then the process tree.
func agentDetectors() []string {
	return append(append([]string(nil), agentMarkers...), humanonly.DetectorProcessTree)
}

func processTreeAvailable() bool {
	for _, d := range humanonly.Detectors() {
		if d.Name == humanonly.DetectorProcessTree {
			return d.Available
		}
	}
	return false
}

// otherClientRefusal is the exact refusal of an agent session's tq run <other>
// (ADR-0020).
func otherClientRefusal(name string) string {
	return "tq run: " + name + " is another client. An agent session can only run under the client of its own folder."
}

func denialsOf(t *testing.T) []audit.Denial {
	t.Helper()
	ds, err := audit.ReadDenials()
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// leaks reports whether text holds value, written as is or as a JSON string
// (a Windows path is escaped inside the denial log).
func leaks(t *testing.T, text, value string) bool {
	t.Helper()
	esc, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(text, value) || strings.Contains(text, strings.Trim(string(esc), `"`))
}

// An agent session (any agent detector) launches only the identity of the
// folder it is in (FR-13): tq run <other> refuses with exit 1 and nothing
// launches, from another client's folder and from a neutral one. Each refusal
// is one identity denial in the log, and names the detector that fired.
func TestRunOtherClient_RefusedForAgents(t *testing.T) {
	for _, det := range agentDetectors() {
		t.Run(det, func(t *testing.T) {
			if det == humanonly.DetectorProcessTree && !processTreeAvailable() {
				t.Skip("no process-tree walker on this OS")
			}
			roots := mkWorkspacesInOneBase(t, "alpha", "beta")
			setAgentDetector(t, det)
			calls := fakeExec(t)
			for _, from := range []string{roots["alpha"], t.TempDir()} {
				chdir(t, from)
				before := len(denialsOf(t))
				_, err := runTq(t, "run", "beta", "--", "gh", "pr", "list")
				if err == nil || err.Error() != otherClientRefusal("beta") {
					t.Fatalf("from %s: tq run beta must refuse in an agent session, got %v", from, err)
				}
				if len(*calls) != 0 {
					t.Fatalf("from %s: another workspace launched: %+v", from, *calls)
				}
				ds := denialsOf(t)
				if len(ds) != before+1 {
					t.Fatalf("from %s: want one denial, log has %d more: %+v", from, len(ds)-before, ds)
				}
				if d := ds[len(ds)-1]; d.Category != "identity" || d.Rule != policy.OtherIdentityID ||
					d.Layer != audit.LayerHumanOnly || d.Subject != "beta" || !strings.Contains(d.Reason, det) {
					t.Fatalf("from %s: denial %+v", from, d)
				}
			}
		})
	}
}

// A neutral folder is no client's folder: an agent there refuses tq run of
// any client (FR-13).
func TestRunOtherClient_NeutralFolderRefused(t *testing.T) {
	mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "CLAUDECODE")
	chdir(t, t.TempDir())
	calls := fakeExec(t)
	_, err := runTq(t, "run", "beta", "--", "az", "group", "list")
	if err == nil || err.Error() != otherClientRefusal("beta") {
		t.Fatalf("tq run beta from a neutral folder must refuse, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a neutral folder launched beta: %+v", *calls)
	}
}

// A name that is no client is refused the same way, and the refusal names no
// other client: an agent learns nothing about the workspaces it may not run.
func TestRunOtherClient_UnknownName(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "CLAUDECODE")
	calls := fakeExec(t)
	for _, from := range []string{roots["alpha"], t.TempDir()} {
		chdir(t, from)
		_, err := runTq(t, "run", "ghost", "--", "claude")
		if err == nil || err.Error() != otherClientRefusal("ghost") {
			t.Fatalf("from %s: tq run ghost must refuse in an agent session, got %v", from, err)
		}
		if strings.Contains(err.Error(), "alpha") || strings.Contains(err.Error(), "beta") {
			t.Fatalf("from %s: the refusal names another client: %v", from, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("an unknown name launched: %+v", *calls)
	}
}

// With no marker and a process tree that cannot be followed to a terminal,
// the agent check is unverified: naming another client refuses, and nothing
// launches (ADR-0003).
func TestRunOtherClient_ProcessTreeUnverified(t *testing.T) {
	if !processTreeAvailable() {
		t.Skip("no process-tree walker on this OS")
	}
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	clearAgentMarkers(t)
	prev := humanonly.ParentOf
	t.Cleanup(func() { humanonly.ParentOf = prev })
	humanonly.ParentOf = func(int) (humanonly.Parent, bool) { return humanonly.Parent{}, false }
	chdir(t, roots["alpha"])
	calls := fakeExec(t)
	_, err := runTq(t, "run", "beta", "--", "claude")
	if err == nil || err.Error() != otherClientRefusal("beta") {
		t.Fatalf("an unverified tree must refuse tq run beta, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("an unverified tree launched beta: %+v", *calls)
	}
	ds := denialsOf(t)
	if len(ds) != 1 || !strings.Contains(ds[0].Reason, humanonly.DetectorProcessTree+": cannot establish a human ancestry") {
		t.Fatalf("want one process-tree denial, got %+v", ds)
	}
}

// A human's tq run <other> -- <cmd> launches from any folder, with that
// client's identity and no confirmation: the agent check adds a refusal for
// agents and nothing else (FR-13, REQ-5).
func TestRunOtherClient_HumanAllowed(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "")
	prevConfirm := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) {
		t.Error("tq run asked for a confirmation")
		return false, nil
	}
	t.Cleanup(func() { humanonly.Confirm = prevConfirm })
	calls := fakeExec(t)
	for i, from := range []string{roots["alpha"], t.TempDir()} {
		chdir(t, from)
		if out, err := runTq(t, "run", "beta", "--", "claude", "-p", "hi"); err != nil {
			t.Fatalf("human from %s: %v %s", from, err, out)
		}
		if len(*calls) != i+1 || (*calls)[i].env["TQ_WS"] != "beta" {
			t.Fatalf("human from %s: %+v", from, *calls)
		}
	}
	if ds := denialsOf(t); len(ds) != 0 {
		t.Fatalf("a human launch logged denials: %+v", ds)
	}
}

// The refusal reads from the process's own folder, not from a variable an
// agent may set: TQ_WS and TQ_WS_ROOT naming beta do not help alpha's agent.
func TestRunOtherClient_InheritedEnvIgnored(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "CLAUDECODE")
	t.Setenv("TQ_WS", "beta")
	t.Setenv("TQ_WS_ROOT", roots["beta"])
	chdir(t, roots["alpha"])
	calls := fakeExec(t)
	_, err := runTq(t, "run", "beta", "--", "claude")
	if err == nil || err.Error() != otherClientRefusal("beta") {
		t.Fatalf("an inherited TQ_WS must not allow beta, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("beta launched: %+v", *calls)
	}
}

// The refusal and its denial line hold no path under beta's identity folder
// and none of beta's manifest values (REQ-6, S-03).
func TestRunOtherClient_NoLeak(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "CLAUDECODE")
	chdir(t, roots["alpha"])
	fakeExec(t)
	_, err := runTq(t, "run", "beta", "--", "claude")
	if err == nil {
		t.Fatal("tq run beta must refuse")
	}
	raw, rerr := os.ReadFile(audit.DenialsPath())
	if rerr != nil {
		t.Fatal(rerr)
	}
	if n := strings.Count(string(raw), "\n"); n != 1 {
		t.Fatalf("want one denial line, got %d: %s", n, raw)
	}
	betaFolder := filepath.Join(paths.IdentitiesRoot(), "beta")
	for _, secret := range []string{betaFolder, roots["beta"], "beta@example.com"} {
		if leaks(t, err.Error(), secret) {
			t.Fatalf("the refusal leaks %q: %v", secret, err)
		}
		if leaks(t, string(raw), secret) {
			t.Fatalf("the denial line leaks %q: %s", secret, raw)
		}
	}
}

// The refusal is logged before it is returned; a denial log that cannot be
// written still refuses, and nothing launches.
func TestRunOtherClient_LogFailureStillRefuses(t *testing.T) {
	roots := mkWorkspacesInOneBase(t, "alpha", "beta")
	setAgentDetector(t, "CLAUDECODE")
	chdir(t, roots["alpha"])
	calls := fakeExec(t)
	// The denial log is a directory: the refusal cannot be appended.
	if err := os.MkdirAll(audit.DenialsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := runTq(t, "run", "beta", "--", "claude")
	if err == nil || !strings.HasPrefix(err.Error(), otherClientRefusal("beta")) {
		t.Fatalf("an unlogged refusal must still refuse, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("beta launched: %+v", *calls)
	}
}
