package cmd

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// selfTq makes a fake tq binary the running executable, found on PATH as
// a bare `tq` when onPath is true.
func selfTq(t *testing.T, onPath bool) string {
	t.Helper()
	dir := t.TempDir()
	self := filepath.Join(dir, "tq.exe")
	os.WriteFile(self, []byte("x"), 0o755)
	prevSelf, prevLook := tqSelfPath, tqLookPath
	tqSelfPath = func() string { return self }
	tqLookPath = func(string) (string, error) {
		if !onPath {
			return filepath.Join(t.TempDir(), "tq.exe"), nil // another tq
		}
		return self, nil
	}
	t.Cleanup(func() { tqSelfPath, tqLookPath = prevSelf, prevLook })
	return self
}

// Final review, security M1: the hook-time state-dir check skipped any
// command whose first word was named tq. Only the real tq (the running
// binary, or a bare tq resolving to it) is exempt, only for subcommands
// that never run a child or write a path argument, and a child after --
// is checked as its own command.
func TestStateDirTqExemption(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "custom-tq-home")
	t.Setenv("TQ_HOME", custom)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	trust := filepath.ToSlash(filepath.Join(custom, "trust", "x"))
	plan := filepath.ToSlash(filepath.Join(custom, "packs", "acme", "cloud", "plans", "p1"))
	self := filepath.ToSlash(selfTq(t, true))
	denied := func(cmd string) bool {
		d := stateDirDecision(policy.ToolCall{Tool: "Bash", Command: cmd}, home)
		for _, r := range d.Matched {
			if r.ID == "tq/hub-store-write" {
				return true
			}
		}
		return false
	}
	for _, cmd := range []string{
		"tq dotenv run -- rm " + trust,
		"tq dotenv run --file .env -- rm -rf " + filepath.ToSlash(filepath.Join(custom, "build-gate")),
		"tq run acme -- rm " + trust,
		"tq run acme rm " + trust,
		"tq redact run -- cp x " + trust,
		"tq redact --file " + filepath.ToSlash(filepath.Join(custom, "audit.jsonl")),
		"tq pack cloud plan --env dev -- cp x " + trust,
		"./tq pack cloud show " + plan,
		"/other/bin/tq pack cloud show " + plan,
		"tq.exe migrate " + trust,
		"tq --workspace acme pack cloud show " + plan,
		// r2-m1: tq pack cloud plan runs its plan command without -- too
		"tq pack cloud plan --env dev cdk synth -o " + filepath.ToSlash(filepath.Join(custom, "trust", "out")),
		"tq pack cloud plan --env=dev --json cp x " + trust,
		// a glob class reads as the real path once brackets are dropped, but
		// the shell runs another program (<dir>/0/tq.exe for [001])
		path.Dir(path.Dir(self)) + "/[" + path.Base(path.Dir(self)) + "]/tq.exe pack cloud show " + plan,
		path.Dir(self) + "/tq*.exe pack cloud show " + plan,
		// a sub-command inside a safe tq call runs first
		"tq hub list `rm " + trust + "`",
		"tq hub list (Remove-Item " + trust + ")",
		"tq hub list @(Remove-Item " + trust + ")",
		`tq hub add "x $(rm ` + trust + `)"`,
		"tq hub add \"x `rm " + trust + "`\"",
		// Accepted false positives (review F4 reverted): no quote reading, so
		// a parenthesis or backtick anywhere refuses the exemption.
		`tq hub add "check ` + filepath.ToSlash(filepath.Join(custom, "hub")) + ` lock (prod)"`,
		`tq hub add 'check ` + filepath.ToSlash(filepath.Join(custom, "hub")) + ` lock (prod)'`,
		// review F10: bash \' is no quote; PowerShell's curly quotes are quotes
		`tq hub add \' ` + "`icacls " + trust + " /grant x:F`" + ` \'`,
		"tq hub add ‘say \"’ (icacls " + trust + " /grant x:F) ‘\"’",
	} {
		if !denied(cmd) {
			t.Errorf("%q: not denied", cmd)
		}
	}
	for _, cmd := range []string{
		"tq pack cloud show " + plan,
		self + " pack cloud show " + plan,
		"tq hub list --workspace acme",
		"tq pack cloud plan --env dev -- terraform plan",
	} {
		if denied(cmd) {
			t.Errorf("%q: denied", cmd)
		}
	}
	// a bare tq that resolves to another binary is just a program
	selfTq(t, false)
	if !denied("tq pack cloud show " + plan) {
		t.Error("a bare tq resolving elsewhere was exempt")
	}
	tqLookPath = func(string) (string, error) { return "", errors.New("not found") }
	if !denied("tq pack cloud show " + plan) {
		t.Error("a bare tq not on PATH was exempt")
	}
}

// Review F5: a bare tq is looked up as written; tq.exe and case folding
// count only on Windows.
func TestIsSelfTqCaseByOS(t *testing.T) {
	selfTq(t, true)
	prev := hostOS
	t.Cleanup(func() { hostOS = prev })
	hostOS = "linux"
	for _, w := range []string{"TQ", "Tq", "tq.exe"} {
		if isSelfTq(w) {
			t.Errorf("linux: %q is self", w)
		}
	}
	if !isSelfTq("tq") {
		t.Error("linux: tq is not self")
	}
	hostOS = "darwin"
	if isSelfTq("TQ") || samePath("/no/such/tq", "/no/such/TQ") {
		t.Error("darwin: case folded")
	}
	hostOS = "windows"
	for _, w := range []string{"tq", "TQ", "tq.exe", "TQ.EXE"} {
		if !isSelfTq(w) {
			t.Errorf("windows: %q is not self", w)
		}
	}
}
