package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guardplugin"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

// fakeClaude puts a fake claude first on the PATH. It logs its argv and the CLAUDE_CONFIG_DIR it
// was started with, and does what spec says (the test binary run as a program, testutil.FakeMain).
func fakeClaude(t *testing.T, spec testutil.FakeSpec) string {
	t.Helper()
	bin := testutil.TempDir(t)
	spec.Log = filepath.Join(bin, "claude.argv.log")
	spec.EnvLog = filepath.Join(bin, "claude.env.log")
	spec.EnvNames = []string{"CLAUDE_CONFIG_DIR"}
	testutil.NewFakeProgram(t, bin, "claude", spec)
	t.Setenv("PATH", bin)
	return bin
}

// The plugin's two commands, exactly (S-04): the constant marketplace and plugin names, nothing
// from the manifest, the environment or the folder.
var wantPluginCalls = [][]string{
	{"plugin", "marketplace", "add", "tentaqles/tentaqles"},
	{"plugin", "install", "tentaqles@tentaqles"},
}

// reach installs the plugin into the client's Claude config once the trust is signed, with that
// client's CLAUDE_CONFIG_DIR, and signs the record again (U-01).
func TestReach_InstallsThePlugin(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	reachGuardPlugin = installGuardPlugin
	// The install writes the plugin as a real one does (the plugin, installed_plugins.json,
	// enabledPlugins).
	bin := fakeClaude(t, testutil.FakeSpec{CopyArg: "install", Copy: guardCopies(t, "acme")})
	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if g := guardplugin.CheckGuardHook(paths.IdentityDir("acme", "claude")); !g.OK {
		t.Fatalf("the installed plugin is not a guard: %s", g.Why)
	}
	if got := testutil.ReadFakeLog(t, filepath.Join(bin, "claude.argv.log")); !reflect.DeepEqual(got, wantPluginCalls) {
		t.Fatalf("claude calls = %v, want %v", got, wantPluginCalls)
	}
	envs := testutil.ReadFakeEnv(t, filepath.Join(bin, "claude.env.log"))
	if len(envs) != len(wantPluginCalls) {
		t.Fatalf("claude env records = %d", len(envs))
	}
	for _, e := range envs {
		if e["CLAUDE_CONFIG_DIR"] != paths.IdentityDir("acme", "claude") {
			t.Fatalf("CLAUDE_CONFIG_DIR = %q, want the client's own", e["CLAUDE_CONFIG_DIR"])
		}
	}
	if !strings.Contains(out, "The tentaqles plugin is installed") {
		t.Fatalf("the install is not said:\n%s", out)
	}
}

// --no-plugin runs no claude, says the plugin is not installed and gives the line to run.
func TestReach_NoPluginFlag(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	reachGuardPlugin = installGuardPlugin
	bin := fakeClaude(t, testutil.FakeSpec{})
	dir := filepath.Join(clients, "acme")
	out, err := runReach(t, dir, "--no-banner", "--no-plugin")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := testutil.ReadFakeLog(t, filepath.Join(bin, "claude.argv.log")); len(got) != 0 {
		t.Fatalf("--no-plugin ran claude: %v", got)
	}
	if !strings.Contains(out, "--no-plugin") || !strings.Contains(out, guardplugin.GuardPluginLine("acme")) {
		t.Fatalf("the skipped install is not said, with its line:\n%s", out)
	}
}

// A failed install never fails reach: the client is made, the reason and the exact line are
// printed, and exit is 0 (F-01). The failing fake, a hanging one at a lowered limit, and no claude
// on the PATH each do it.
func TestReach_PluginInstallFails(t *testing.T) {
	cases := []struct {
		name     string
		spec     testutil.FakeSpec
		noClaude bool
		hang     bool
		want     string
		// not is text the output must not hold.
		not string
	}{
		{"the install exits non-zero", testutil.FakeSpec{Exit: 1, Stderr: "error: no network\n"}, false, false, "failed: error: no network", ""},
		// The reason is the first error line of stderr, not the last line (a tip, a progress frame).
		{"progress and hints after the error", testutil.FakeSpec{Exit: 1,
			Stderr: "error: marketplace not found\nWarning: slow mirror\n⠋ 45% 12 MB/s\nTip: run claude plugin --help\n"},
			false, false, "failed: error: marketplace not found", "Tip: run"},
		{"the install hangs", testutil.FakeSpec{SleepMS: 60000}, false, true, "did not finish within", ""},
		{"claude is not on the PATH", testutil.FakeSpec{}, true, false, "claude was not found on PATH", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clients := isolateReach(t, fullAnswers())
			reachGuardPlugin = installGuardPlugin
			if c.noClaude {
				t.Setenv("PATH", testutil.TempDir(t))
			} else {
				fakeClaude(t, c.spec)
			}
			if c.hang {
				prev := reachPluginTimeout
				reachPluginTimeout = 300 * time.Millisecond
				t.Cleanup(func() { reachPluginTimeout = prev })
			}
			dir := filepath.Join(clients, "acme")
			start := time.Now()
			out, err := runReach(t, dir, "--no-banner")
			if err != nil {
				t.Fatalf("a failed plugin install failed reach: %v\n%s", err, out)
			}
			if time.Since(start) > 60*time.Second {
				t.Fatalf("reach waited %v for the plugin", time.Since(start))
			}
			for _, want := range []string{"The tentaqles plugin was not installed: ", c.want, "\n  " + strings.Join(guardInstallFix, "\n  ")} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			if c.not != "" && strings.Contains(out, c.not) {
				t.Errorf("the output holds %q, which is not the reason:\n%s", c.not, out)
			}
			if !strings.Contains(out, "acme is ready.") {
				t.Errorf("reach did not finish:\n%s", out)
			}
		})
	}
}

// The plugin's install is the same whichever folder or manifest asks for it: the arguments do not
// change (S-04).
func TestReach_PluginSourceIsFixed(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	reachGuardPlugin = installGuardPlugin
	bin := fakeClaude(t, testutil.FakeSpec{})
	// Environment and folder names that a source could be read from: none of them reaches the commands.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(os.TempDir(), "elsewhere"))
	t.Setenv("TQ_GUARD_PLUGIN", "https://example.test/evil")
	if out, err := runReach(t, filepath.Join(clients, "acme2"), "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := testutil.ReadFakeLog(t, filepath.Join(bin, "claude.argv.log")); !reflect.DeepEqual(got, wantPluginCalls) {
		t.Fatalf("claude calls = %v, want %v", got, wantPluginCalls)
	}
}
