package cmd

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/audit"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// humanOnlyCommands are the invocations PRD v2 §8.1 makes human-only: each
// refuses at the top of its RunE, before it resolves anything, so an empty
// isolated home is fixture enough.
var humanOnlyCommands = [][]string{
	{"init", "C:/work"},
	{"add", "acme", "--base", "C:/work", "--git-email", "a@acme.test"},
	{"allow", "acme"},
	{"deny", "acme"},
	{"hooks", "install", "bash"},
	{"hooks", "remove", "bash"},
	{"settings", "render", "acme"},
	{"settings", "render", "--all", "--dry-run"},
	{"bundle", "sync", "acme"},
	{"bundle", "capture", "acme"},
	{"migrate"},
	{"migrate", "--apply"},
	{"uninstall", "--restore", "latest"},
	{"uninstall", "--restore", "latest", "--yes"},
	{"setup", "--from", "plan.yaml"},
	{"setup", "--from", "plan.yaml", "--yes"},
	{"setup", "--from", "plan.yaml", "--dry-run"},
	{"setup"},
	{"onboard", "acme"},
	{"onboard", "--from", "answers.yaml"},
	{"secret", "set", "acme", "jira"},
	{"secret", "rm", "acme", "jira"},
	{"providers", "add", "widget", "--name", "Widget", "--category", "other"},
	{"login", "acme", "gh"},
	{"run", "--off", "--", "claude"},
	{"worktrees", "prune", "--yes"},
}

// freeCommands run anywhere, a session included.
var freeCommands = [][]string{
	{"version"},
	{"list"},
	{"doctor"},
	{"hooks", "status"},
	{"settings", "--help"},
	{"bundle", "catalog"},
	{"providers", "list"},
	{"secret", "status"},
	{"worktrees", "list"},
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
		if !strings.Contains(err.Error(), "env-marker: CLAUDE_CODE_ENTRYPOINT is set") || !strings.Contains(err.Error(), "tq "+args[0]) {
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
		if d.Kind != "human-only" || !strings.HasPrefix(d.Command, "tq "+args[0]) || d.Reason != "env-marker: CLAUDE_CODE_ENTRYPOINT is set" || d.PID != os.Getpid() || d.Cwd == "" {
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

// The TTY and process-tree detectors refuse a command the same way.
func TestHumanOnlyCommandsRefuseWithoutTTYOrUnderAHarnessProcess(t *testing.T) {
	isolateHome(t)
	prevTTY, prevParent := humanonly.IsTerminal, humanonly.ParentOf
	t.Cleanup(func() { humanonly.IsTerminal, humanonly.ParentOf = prevTTY, prevParent })

	humanonly.IsTerminal = func(f *os.File) bool { return f != os.Stdout }
	if _, err := runTq(t, "allow", "acme"); !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "tty: stdout is not a terminal") {
		t.Fatalf("a redirected stdout: %v", err)
	}
	humanonly.IsTerminal = prevTTY
	humanonly.IsTerminal = func(*os.File) bool { return true }

	self := os.Getpid()
	humanonly.ParentOf = func(pid int) (int, string, string, bool) {
		switch pid {
		case self:
			return 1000, "bash.exe", "", true
		case 1000:
			return 2000, "node.exe", `"C:\Program Files\nodejs\node.exe" C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`, true
		case 2000:
			return 3000, "WindowsTerminal.exe", "", true
		}
		return 0, "", "", false
	}
	if _, err := runTq(t, "deny", "acme"); !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "process-tree: node.exe (pid 2000)") {
		t.Fatalf("claude running as node in the tree: %v", err)
	}
	ds := readDenials(t)
	if len(ds) != 2 || ds[0].Reason != "tty: stdout is not a terminal" || ds[1].Reason != "process-tree: node.exe (pid 2000) is in the process tree" {
		t.Fatalf("denials %+v", ds)
	}
}

// The free commands never refuse under a marker: they read, they never write
// under the home.
func TestFreeCommandsRunUnderAMarker(t *testing.T) {
	isolateHome(t)
	prevExit := exitFunc
	exitFunc = func(int) {}
	t.Cleanup(func() { exitFunc = prevExit })
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return hooks.Profiles{} }
	t.Cleanup(func() { hooks.ProfilesFn = prev })
	t.Setenv("CLAUDECODE", "1")
	for _, args := range freeCommands {
		if _, err := runTq(t, args...); errors.Is(err, errHumanOnly) {
			t.Fatalf("tq %s must run from a session: %v", strings.Join(args, " "), err)
		}
	}
	if ds := readDenials(t); len(ds) != 0 {
		t.Fatalf("a free command logged a denial: %+v", ds)
	}
}

// The confirmation is the last step before the write: with no at the prompt
// tq add writes nothing and exits 1; with yes it proceeds.
func TestAddConfirmsBeforeWriting(t *testing.T) {
	root := mkLaunchWS(t, "acme", "")
	base := filepath.Dir(root)
	prevRunGit := addRunGit
	addRunGit = func(...string) (string, error) { return "", nil }
	t.Cleanup(func() { addRunGit = prevRunGit })
	prevConfirm := humanonly.Confirm
	t.Cleanup(func() { humanonly.Confirm = prevConfirm })

	var prompt string
	humanonly.Confirm = func(p string) (bool, error) { prompt = p; return false, nil }
	before := filesUnder(t, paths.Home())
	out, err := runTq(t, "add", "beta", "--base", base, "--git-email", "b@beta.test", "--identities", "gh")
	if !errors.Is(err, humanonly.ErrAborted) || errors.Is(err, errHumanOnly) {
		t.Fatalf("a declined confirmation aborts: %v\n%s", err, out)
	}
	if prompt != "Type yes to continue: " {
		t.Fatalf("prompt %q", prompt)
	}
	want := "tq add: about to create " + filepath.Join(base, "beta")
	if !strings.Contains(out, want) {
		t.Fatalf("the line names what is written: want %q in\n%s", want, out)
	}
	if _, err := os.Stat(filepath.Join(base, "beta")); !os.IsNotExist(err) {
		t.Fatal("a declined tq add scaffolded the folder")
	}
	if after := filesUnder(t, paths.Home()); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("a declined tq add wrote under the home: before %v after %v", before, after)
	}

	humanonly.Confirm = func(string) (bool, error) { return true, nil }
	out, err = runTq(t, "add", "beta", "--base", base, "--git-email", "b@beta.test", "--identities", "gh")
	if err != nil || !strings.Contains(out, "(trusted)") {
		t.Fatalf("a confirmed tq add: %v\n%s", err, out)
	}
	if !trust.IsTrusted("beta", manifestHash(t, filepath.Join(base, "beta"))) {
		t.Fatal("a confirmed tq add trusts the folder")
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
		{"worktrees", "prune"},
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
