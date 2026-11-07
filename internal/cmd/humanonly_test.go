package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/bundle"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// humanOnlyCommands are the invocations PRD v2 §8.1 makes human-only: each
// refuses at the top of its RunE, before it resolves anything, so an empty
// isolated home is fixture enough.
var humanOnlyCommands = [][]string{
	{"reach", "C:/work/acme"},
	{"allow", "acme"},
	{"deny", "acme"},
	{"hooks", "install", "bash"},
	{"hooks", "remove", "bash"},
	{"settings", "render", "acme"},
	{"bundle", "sync", "acme"},
	{"bundle", "capture", "acme"},
	{"migrate", "--apply"},
	{"uninstall", "--restore", "latest"},
	{"uninstall", "--restore", "latest", "--yes"},
	{"secret", "set", "acme", "jira"},
	{"secret", "rm", "acme", "jira"},
	{"providers", "add", "widget", "--name", "Widget", "--category", "other"},
	{"login", "acme", "gh"},
	{"settings", "anchors", "add", "foo-term"},
	{"settings", "anchors", "remove", "foo-term"},
	{"migrate", "manifests", "--apply"},
}

// readDenials parses the denial log.
func readDenials(t *testing.T) []audit.Denial {
	t.Helper()
	b, err := os.ReadFile(audit.DenialsPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Denial
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var d audit.Denial
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		out = append(out, d)
	}
	return out
}

// filesUnder lists every file under dir, relative to it.
func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// treeDigest hashes every path, link target and file body under dirs (a
// missing dir hashes as nothing), so a test can prove a tree did not change.
func treeDigest(t *testing.T, dirs ...string) string {
	t.Helper()
	h := sha256.New()
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			fmt.Fprintf(h, "%s|%s|", filepath.ToSlash(rel), d.Type())
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				target, err := os.Readlink(p)
				fmt.Fprintf(h, "-> %s %v", target, err)
			case d.Type().IsRegular():
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				h.Write(b)
			}
			fmt.Fprintln(h)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Every human-only command refuses under a harness marker, before any write:
// the only file the isolated home gains is the denial log, with one
// human-only line naming the command and the detector.
func TestHumanOnlyCommandsRefuseWithMarkerAndLog(t *testing.T) {
	isolateHome(t)
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return hooks.Profiles{} }
	t.Cleanup(func() { hooks.ProfilesFn = prev })
	calls := fakeExec(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	for i, args := range humanOnlyCommands {
		out, err := runTq(t, args...)
		if !errors.Is(err, errHumanOnly) {
			t.Fatalf("tq %s: want errHumanOnly, got %v\n%s", strings.Join(args, " "), err, out)
		}
		if !strings.Contains(err.Error(), "env-marker: CLAUDE_CODE_ENTRYPOINT is set") || !(strings.Contains(err.Error(), "tq "+args[0]) || strings.Contains(err.Error(), "tentaqles "+args[0])) {
			t.Fatalf("tq %s: the error names the command and the detector: %v", strings.Join(args, " "), err)
		}
		if got := filesUnder(t, paths.Home()); len(got) != 1 || got[0] != "denials.jsonl" {
			t.Fatalf("tq %s wrote under the home: %v", strings.Join(args, " "), got)
		}
		ds := readDenials(t)
		if len(ds) != i+1 {
			t.Fatalf("tq %s: want %d denials, got %d", strings.Join(args, " "), i+1, len(ds))
		}
		d := ds[i]
		if d.Category != "self-protection" || d.Layer != audit.LayerHumanOnly || !(strings.HasPrefix(d.Command, "tq "+args[0]) || strings.HasPrefix(d.Command, "tentaqles "+args[0])) || d.Reason != "env-marker: CLAUDE_CODE_ENTRYPOINT is set" || d.PID != os.Getpid() || d.Cwd == "" {
			t.Fatalf("tq %s: denial %+v", strings.Join(args, " "), d)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused run --off launched %+v", *calls)
	}
	b, _ := os.ReadFile(audit.DenialsPath())
	if strings.Contains(string(b), "cli\"") {
		t.Fatal("the denial log must not carry the marker's value")
	}
}

// The TTY and process-tree detectors refuse a command the same way: a
// redirected stdout, a harness running as node, and an orphaned chain.
func TestHumanOnlyCommandsRefuseWithoutTTYOrUnderAHarnessProcess(t *testing.T) {
	isolateHome(t)
	prevTTY, prevParent := humanonly.IsTerminal, humanonly.ParentOf
	t.Cleanup(func() { humanonly.IsTerminal, humanonly.ParentOf = prevTTY, prevParent })

	humanonly.IsTerminal = func(f *os.File) bool { return f != os.Stdout }
	if _, err := runTq(t, "allow", "acme"); !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "tty: stdout is not a terminal") {
		t.Fatalf("a redirected stdout: %v", err)
	}
	humanonly.IsTerminal = func(*os.File) bool { return true }

	self := os.Getpid()
	humanonly.ParentOf = func(pid int) (humanonly.Parent, bool) {
		switch pid {
		case self:
			return humanonly.Parent{PID: 1000, Name: "bash.exe", Cmdline: "bash", CmdlineKnown: true}, true
		case 1000:
			return humanonly.Parent{PID: 2000, Name: "node.exe", Cmdline: `"C:\Program Files\nodejs\node.exe" C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`, CmdlineKnown: true}, true
		case 2000:
			return humanonly.Parent{PID: 3000, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}, true
		}
		return humanonly.Parent{}, false
	}
	if _, err := runTq(t, "deny", "acme"); !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "process-tree: node.exe (pid 2000)") {
		t.Fatalf("claude running as node in the tree: %v", err)
	}
	humanonly.ParentOf = func(pid int) (humanonly.Parent, bool) {
		if pid == self {
			return humanonly.Parent{PID: 1000, Name: "bash", Cmdline: "bash", CmdlineKnown: true}, true
		}
		return humanonly.Parent{}, false // bash's parent, a harness, is gone
	}
	if _, err := runTq(t, "hooks", "install", "bash"); !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "cannot establish a human ancestry") {
		t.Fatalf("an orphaned chain: %v", err)
	}
	ds := readDenials(t)
	if len(ds) != 3 || ds[0].Reason != "tty: stdout is not a terminal" || ds[1].Reason != "process-tree: node.exe (pid 2000) is in the process tree" || !strings.HasPrefix(ds[2].Reason, "process-tree: cannot establish a human ancestry") {
		t.Fatalf("denials %+v", ds)
	}
}

// The free commands never refuse under a marker: they read, they never write
// under the home. Every command the ADR lists as free is here.
func TestFreeCommandsRunUnderAMarker(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return hooks.Profiles{} }
	t.Cleanup(func() { hooks.ProfilesFn = prev })
	fakeExec(t)
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("REGION=us\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	free := [][]string{
		{"version"},
		{"env"},
		{"activate", "bash"},
		{"list"},
		{"doctor"},
		{"hooks", "status"},
		{"settings", "--help"},
		{"settings", "anchors"},
		{"bundle", "catalog"},
		{"bundle", "diff", "acme"},
		{"providers", "list"},
		{"providers", "show", "gh"},
		{"providers", "check", "gh"},
		{"secret", "status"},
		{"secrets", "audit"},
		{"dotenv", "run", "--file", envFile, "--", "go", "version"},
		// Dry runs are reads (ADR-0005).
		{"migrate", "--steps", "manifests"},
		{"settings", "render", "--all", "--dry-run"},
	}
	for _, args := range free {
		if _, err := runTq(t, args...); errors.Is(err, errHumanOnly) {
			t.Fatalf("tq %s must run from a session: %v", strings.Join(args, " "), err)
		}
	}
	// tq run <ws> is free from the workspace's own folder; from anywhere else an
	// agent is refused (TestRunOtherClient_RefusedForAgents).
	chdir(t, root)
	if out, err := runTq(t, "run", "acme", "--", "claude", "-p", "hi"); err != nil {
		t.Fatalf("tq run acme from its own folder: %v %s", err, out)
	}
	payload := hookPayloadFor(t, root, "ls")
	for _, args := range [][]string{{"claude-hook", "session-start"}, {"claude-hook", "pre-tool-use"}} {
		if _, _, err := runTQIn(t, strings.NewReader(payload), args...); errors.Is(err, errHumanOnly) {
			t.Fatalf("tq %s must run from a session: %v", strings.Join(args, " "), err)
		}
	}
	if ds := readDenials(t); len(ds) != 0 {
		t.Fatalf("a free command logged a denial: %+v", ds)
	}
}

// declinedCase is one human-only write with its fixture: the invocation, the
// trees that must not change, and a check of the seams it would reach.
type declinedCase struct {
	name string
	// setup builds the fixture and returns the arguments, the directories to
	// hash, and an optional check that runs after the declined command.
	setup func(t *testing.T) (args []string, dirs []string, after func(t *testing.T))
}

// Every human-only write aborts on a declined confirmation with nothing
// changed under the isolated home or the workspace: the trees hash the same
// before and after, and no launcher ran.
func TestDeclinedConfirmationWritesNothing(t *testing.T) {
	fakeProfiles := func(t *testing.T, dir string, install bool) {
		t.Helper()
		prev := hooks.ProfilesFn
		profiles := hooks.Profiles{"bash": filepath.Join(dir, "bashrc")}
		hooks.ProfilesFn = func() hooks.Profiles { return profiles }
		t.Cleanup(func() { hooks.ProfilesFn = prev })
		if install {
			if _, err := hooks.Install("bash", profiles); err != nil {
				t.Fatal(err)
			}
		}
	}
	noLaunch := func(calls *[]launched) func(t *testing.T) {
		return func(t *testing.T) {
			if len(*calls) != 0 {
				t.Fatalf("a declined command launched %+v", *calls)
			}
		}
	}
	cases := []declinedCase{
		{"allow", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			root := mkLaunchWS(t, "acme", "")
			if err := trust.Deny("acme", manifestHash(t, root)); err != nil {
				t.Fatal(err)
			}
			return []string{"allow", "acme"}, []string{paths.Home(), filepath.Dir(root)}, func(t *testing.T) {
				if trust.IsTrusted("acme", manifestHash(t, root)) {
					t.Fatal("a declined allow trusted the workspace")
				}
			}
		}},
		{"deny", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			root := mkLaunchWS(t, "acme", "")
			return []string{"deny", "acme"}, []string{paths.Home(), filepath.Dir(root)}, func(t *testing.T) {
				if !trust.IsTrusted("acme", manifestHash(t, root)) {
					t.Fatal("a declined deny removed the trust")
				}
			}
		}},
		{"hooks install", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			dir := t.TempDir()
			fakeProfiles(t, dir, false)
			return []string{"hooks", "install", "bash"}, []string{paths.Home(), dir}, nil
		}},
		{"hooks remove", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			dir := t.TempDir()
			fakeProfiles(t, dir, true)
			return []string{"hooks", "remove", "bash"}, []string{paths.Home(), dir}, nil
		}},
		{"settings render", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			root := mkLaunchWS(t, "acme", "")
			return []string{"settings", "render", "acme"}, []string{paths.Home(), filepath.Dir(root)}, nil
		}},
		{"bundle sync", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			ws := mkTrustedWithBundle(t, &manifest.Bundle{MCP: []string{"github"}})
			cat := &bundle.Catalog{Marketplaces: map[string]bundle.Marketplace{}, Skills: map[string]bundle.Skill{}, MCP: map[string]bundle.MCPServer{"github": {"command": "gh-mcp"}}}
			if err := cat.Save(); err != nil {
				t.Fatal(err)
			}
			return []string{"bundle", "sync", "acme"}, []string{paths.Home(), filepath.Dir(ws.Root)}, nil
		}},
		{"bundle capture --write-catalog", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			dir := t.TempDir()
			return []string{"bundle", "capture", "--dir", dir, "--write-catalog"}, []string{paths.Home(), dir}, nil
		}},
		{"migrate --apply", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			e := newMigEnv(t)
			e.addWorkspace("alpha", "Alpha Dev", "dev@alpha.test", "claude")
			e.linkIdentity("alpha", "claude", filepath.Join(e.home, ".claude-alpha"))
			return []string{"migrate", "--apply", "--steps", "identity"}, []string{e.home, e.tqHome}, nil
		}},
		{"uninstall --yes", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			e, _ := setupApplied(t)
			return []string{"uninstall", "--restore", migTestTS, "--yes"}, []string{e.home, e.tqHome}, nil
		}},
		{"providers add", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			return []string{"providers", "add", "widget", "--name", "Widget", "--category", "other"}, []string{paths.Home()}, func(t *testing.T) {
				if _, err := os.Stat(filepath.Join(providers.UserDir(), "widget.yaml")); !os.IsNotExist(err) {
					t.Fatal("a declined providers add wrote the file")
				}
			}
		}},
		{"login", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			root := mkLaunchWS(t, "acme", "")
			calls := fakeExec(t)
			return []string{"login", "acme", "gh"}, []string{paths.Home(), filepath.Dir(root)}, noLaunch(calls)
		}},
		{"settings anchors add", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			return []string{"settings", "anchors", "add", "foo-term"}, []string{paths.Home()}, nil
		}},
		{"settings anchors remove", func(t *testing.T) ([]string, []string, func(*testing.T)) {
			isolateHome(t)
			if _, _, err := humanonly.AddUserAnchor("foo-term"); err != nil {
				t.Fatal(err)
			}
			return []string{"settings", "anchors", "remove", "foo-term"}, []string{paths.Home()}, func(t *testing.T) {
				if got, _ := humanonly.LoadUserAnchors(); strings.Join(got, ",") != "foo-term" {
					t.Fatalf("a declined remove changed anchors.yaml: %v", got)
				}
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, dirs, after := c.setup(t)
			prevConfirm := humanonly.Confirm
			asked := 0
			humanonly.Confirm = func(string) (bool, error) { asked++; return false, nil }
			t.Cleanup(func() { humanonly.Confirm = prevConfirm })
			before := treeDigest(t, dirs...)
			out, err := runTq(t, args...)
			if !errors.Is(err, humanonly.ErrAborted) || errors.Is(err, errHumanOnly) {
				t.Fatalf("tq %s: want ErrAborted, got %v\n%s", strings.Join(args, " "), err, out)
			}
			if asked != 1 {
				t.Fatalf("tq %s: asked %d times", strings.Join(args, " "), asked)
			}
			named := false
			for i := 1; i <= len(args); i++ {
				if strings.Contains(out, "tq "+strings.Join(args[:i], " ")+": about to") {
					named = true
				}
			}
			if !named {
				t.Fatalf("tq %s: the line names what is written:\n%s", strings.Join(args, " "), out)
			}
			if after := treeDigest(t, dirs...); after != before {
				t.Fatalf("tq %s changed a tree it must not touch: %v", strings.Join(args, " "), dirs)
			}
			if after != nil {
				after(t)
			}
		})
	}
}

// Dry runs and listings ask nothing: only a write is confirmed.
func TestDryRunsAskNoConfirmation(t *testing.T) {
	isolateHome(t)
	prevConfirm := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { t.Fatal("a dry run asked for a confirmation"); return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prevConfirm })
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return hooks.Profiles{} }
	t.Cleanup(func() { hooks.ProfilesFn = prev })
	for _, args := range [][]string{
		{"settings", "render", "--all", "--dry-run"},
	} {
		if _, err := runTq(t, args...); errors.Is(err, humanonly.ErrAborted) {
			t.Fatalf("tq %s: %v", strings.Join(args, " "), err)
		}
	}
}

// tq doctor names the detectors this OS has.
func TestDoctorReportsHumanOnlyDetectors(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })
	out, err := runTq(t, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"human-only detectors:", "tty (", "env-marker (", "process-tree ("} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor misses %q:\n%s", want, out)
		}
	}
}
