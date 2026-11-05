package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// unlistedTerminal is a chain whose terminal tq does not know by name:
// this process <- pwsh <- Foo-Term.exe <- svchost (another session).
func unlistedTerminal(pid int) (humanonly.Parent, bool) {
	switch pid {
	case os.Getpid():
		return humanonly.Parent{PID: 90, Name: "pwsh.exe", Cmdline: "pwsh", CmdlineKnown: true}, true
	case 90:
		return humanonly.Parent{PID: 80, Name: "Foo-Term.exe", Cmdline: `"C:\Program Files\FooTerm\Foo-Term.exe"`, CmdlineKnown: true}, true
	case 80:
		return humanonly.Parent{PID: 70, Name: "svchost.exe", CmdlineKnown: true}, true
	}
	return humanonly.Parent{}, false
}

// The escape hatch: a refused unlisted terminal names itself and the add
// command; tq settings anchors add accepts it (after the confirmation), the
// list shows it, remove refuses the chain again.
func TestSettingsAnchorsEscapeHatch(t *testing.T) {
	isolateHome(t)
	prevParent := humanonly.ParentOf
	humanonly.ParentOf = unlistedTerminal
	t.Cleanup(func() { humanonly.ParentOf = prevParent })

	_, err := runTq(t, "allow", "acme")
	if !errors.Is(err, errHumanOnly) || !strings.Contains(err.Error(), "Foo-Term.exe (pid 80)") || !strings.Contains(err.Error(), "tq settings anchors add foo-term") {
		t.Fatalf("an unlisted terminal refuses with the add line: %v", err)
	}
	// The add itself is refused on the same chain: the human runs it from a
	// terminal tq knows, or adds the name there.
	if _, err := runTq(t, "settings", "anchors", "add", "foo-term"); !errors.Is(err, errHumanOnly) {
		t.Fatalf("anchors add under the unlisted terminal: %v", err)
	}
	humanonly.ParentOf = terminalChain
	out, err := runTq(t, "settings", "anchors", "add", "Foo-Term.exe")
	if err != nil || !strings.Contains(out, "Added foo-term to "+humanonly.UserAnchorsPath()) {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.HasPrefix(humanonly.UserAnchorsPath(), paths.Home()) {
		t.Fatalf("anchors.yaml lives in the protected area: %s", humanonly.UserAnchorsPath())
	}
	out, err = runTq(t, "settings", "anchors")
	if err != nil || !strings.Contains(out, "yours ("+humanonly.UserAnchorsPath()+"): foo-term") || !strings.Contains(out, "built-in: ") || !strings.Contains(out, "windowsterminal") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	humanonly.ParentOf = unlistedTerminal
	if _, err := runTq(t, "list"); errors.Is(err, errHumanOnly) {
		t.Fatalf("list is free: %v", err)
	}
	if _, err := runTq(t, "deny", "acme"); errors.Is(err, errHumanOnly) {
		t.Fatalf("the added terminal passes the check: %v", err)
	}
	humanonly.ParentOf = terminalChain
	if out, err := runTq(t, "settings", "anchors", "remove", "foo-term"); err != nil || !strings.Contains(out, "Removed foo-term") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if _, err := runTq(t, "settings", "anchors", "remove", "foo-term"); err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("a second remove is an error: %v", err)
	}
	humanonly.ParentOf = unlistedTerminal
	if _, err := runTq(t, "deny", "acme"); !errors.Is(err, errHumanOnly) {
		t.Fatalf("a removed terminal refuses again: %v", err)
	}
}

// Shells, runtimes and harnesses are never accepted as anchors, and a
// declined confirmation writes nothing.
func TestSettingsAnchorsAddRefusesShellsAndDeclines(t *testing.T) {
	isolateHome(t)
	for _, bad := range []string{"bash", "node", "claude", "pwsh.exe", `C:\foo\term.exe`} {
		if _, err := runTq(t, "settings", "anchors", "add", bad); err == nil || errors.Is(err, errHumanOnly) {
			t.Fatalf("add %q: %v", bad, err)
		}
	}
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	if _, err := runTq(t, "settings", "anchors", "add", "foo-term"); !errors.Is(err, humanonly.ErrAborted) {
		t.Fatalf("declined add: %v", err)
	}
	if _, err := os.Stat(humanonly.UserAnchorsPath()); !os.IsNotExist(err) {
		t.Fatal("a declined add wrote anchors.yaml")
	}
}
