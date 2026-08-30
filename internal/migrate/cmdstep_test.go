package migrate

import (
	"runtime"
	"strings"
	"testing"
)

// useGOOS pretends the cmd step is running on another OS.
func useGOOS(t *testing.T, goos string) {
	t.Helper()
	old := cmdGOOS
	cmdGOOS = goos
	t.Cleanup(func() { cmdGOOS = old })
}

// legacyAutoRun is the shape found on the dev machine, but recorded here as
// REG_EXPAND_SZ so the tests prove the value's type travels through the journal
// (restoring %LOCALAPPDATA%\... as REG_SZ would freeze the variable).
const legacyAutoRun = `"%LOCALAPPDATA%\tentaqles\shims\autorun.cmd"`

func TestCmdPlanNotWindows(t *testing.T) {
	useGOOS(t, "linux")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: legacyAutoRun})

	p, err := cmdStep{}.Plan(Deps{Reg: fr.run})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 0 || len(p.Warnings) != 0 {
		t.Fatalf("expected an empty plan off Windows, got %+v", p)
	}
	if len(p.Skipped) != 1 || p.Skipped[0] != "cmd: not windows" {
		t.Fatalf("Skipped = %v, want [cmd: not windows]", p.Skipped)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("the registry was queried off Windows: %v", fr.calls)
	}
}

func TestCmdPlanNoAutoRun(t *testing.T) {
	useGOOS(t, "windows")
	fr := newFakeReg()

	p, err := cmdStep{}.Plan(Deps{Reg: fr.run})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", p.Changes)
	}
	if !strings.Contains(joined(p.Skipped), "AutoRun is not set") {
		t.Fatalf("Skipped = %v", p.Skipped)
	}
}

func TestCmdPlanEmitsClearAutorun(t *testing.T) {
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_EXPAND_SZ", Data: legacyAutoRun})

	p, err := cmdStep{}.Plan(Deps{Reg: fr.run})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 1 {
		t.Fatalf("got %d changes, want 1: %+v", len(p.Changes), p.Changes)
	}
	c := p.Changes[0]
	if c.Step != "cmd" || c.Kind != "clear-autorun" {
		t.Errorf("change = %+v, want step cmd kind clear-autorun", c)
	}
	if c.Path != autoRunKey+`\`+autoRunName {
		t.Errorf("Path = %q", c.Path)
	}
	if c.Detail != legacyAutoRun {
		t.Errorf("Detail = %q, want the old value %q", c.Detail, legacyAutoRun)
	}
	if !c.Danger {
		t.Error("clearing a registry value the user set should be marked Danger")
	}
	if len(p.Warnings) != 0 {
		t.Errorf("a tq shim AutoRun should not warn: %v", p.Warnings)
	}
	// Plan must not write.
	for _, call := range fr.calls {
		if call[0] != "query" {
			t.Errorf("Plan performed a %s", call[0])
		}
	}
}

func TestCmdPlanWarnsOnForeignAutoRun(t *testing.T) {
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: `"C:\Program Files\clink\clink.bat" inject`})

	p, err := cmdStep{}.Plan(Deps{Reg: fr.run})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(p.Changes))
	}
	if !strings.Contains(joined(p.Warnings), "does not look like") {
		t.Fatalf("expected a warning about a foreign AutoRun, got %v", p.Warnings)
	}
}

func TestCmdPlanRefusesUnrecordableType(t *testing.T) {
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_MULTI_SZ", Data: `a\0b`})

	p, err := cmdStep{}.Plan(Deps{Reg: fr.run})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 0 {
		t.Fatalf("a value tq cannot restore must not be cleared: %+v", p.Changes)
	}
	if !strings.Contains(joined(p.Skipped), "REG_MULTI_SZ") {
		t.Fatalf("Skipped = %v", p.Skipped)
	}
}

func TestCmdPlanNoRegRunner(t *testing.T) {
	useGOOS(t, "windows")
	p, err := cmdStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(p.Changes) != 0 || len(p.Skipped) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	if !strings.Contains(p.Skipped[0], "no registry runner") {
		t.Fatalf("Skipped = %v", p.Skipped)
	}
}

// The whole point of the type travelling with the value: an AutoRun stored as
// REG_EXPAND_SZ must come back as REG_EXPAND_SZ, not as a frozen literal.
func TestCmdApplyAndRestoreKeepsRegType(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Journal.Restore refuses reg-set off Windows by design")
	}
	tqHome(t)
	fr := newFakeReg()
	want := RegValue{Type: "REG_EXPAND_SZ", Data: legacyAutoRun}
	fr.set(autoRunKey, autoRunName, want)
	d := Deps{Reg: fr.run}

	j, err := Open("20260830-100000")
	if err != nil {
		t.Fatal(err)
	}
	step := cmdStep{}
	p, err := step.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := step.Apply(d, p, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := fr.get(autoRunKey, autoRunName); ok {
		t.Fatal("AutoRun was not deleted")
	}
	if len(j.Entries) != 1 {
		t.Fatalf("journal = %+v", j.Entries)
	}
	e := j.Entries[0]
	if e.Step != "cmd" || e.Op != OpRegSet {
		t.Fatalf("entry = %+v", e)
	}
	if e.Args["Type"] != "REG_EXPAND_SZ" || e.Args["Old"] != legacyAutoRun || e.Args["Present"] != "true" {
		t.Fatalf("entry args = %+v", e.Args)
	}

	if _, err := j.Restore(Runner{Reg: fr.run}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, ok := fr.get(autoRunKey, autoRunName)
	if !ok {
		t.Fatal("AutoRun was not restored")
	}
	if got != want {
		t.Fatalf("restored %+v, want %+v", got, want)
	}
}

func TestCmdApplyWithNoChangesDoesNothing(t *testing.T) {
	tqHome(t)
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: legacyAutoRun})
	j, err := Open("20260830-100001")
	if err != nil {
		t.Fatal(err)
	}
	if err := (cmdStep{}).Apply(Deps{Reg: fr.run}, Plan{}, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := fr.get(autoRunKey, autoRunName); !ok {
		t.Fatal("Apply deleted a value that was not in the plan")
	}
	if len(j.Entries) != 0 {
		t.Fatalf("journal = %+v", j.Entries)
	}
}

// A value changed between plan and apply must not be deleted from the stale
// plan: the user approved a different value.
func TestCmdApplyRefusesStalePlan(t *testing.T) {
	tqHome(t)
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: legacyAutoRun})
	d := Deps{Reg: fr.run}
	j, err := Open("20260830-100002")
	if err != nil {
		t.Fatal(err)
	}
	step := cmdStep{}
	p, err := step.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: `"C:\clink\clink.bat" inject`})

	err = step.Apply(d, p, j)
	if err == nil {
		t.Fatal("expected Apply to refuse a stale plan")
	}
	if !strings.Contains(err.Error(), "changed since the plan") {
		t.Fatalf("error = %v", err)
	}
	if v, _ := fr.get(autoRunKey, autoRunName); v.Data != `"C:\clink\clink.bat" inject` {
		t.Fatalf("the new value was touched: %+v", v)
	}
	if len(j.Entries) != 0 {
		t.Fatalf("journal = %+v", j.Entries)
	}

	// --force accepts what is on the machine now, journalling that value.
	d.Force = true
	if err := step.Apply(d, p, j); err != nil {
		t.Fatalf("Apply --force: %v", err)
	}
	if _, ok := fr.get(autoRunKey, autoRunName); ok {
		t.Fatal("AutoRun was not deleted under --force")
	}
	if j.Entries[0].Args["Old"] != `"C:\clink\clink.bat" inject` {
		t.Fatalf("--force journalled the stale value: %+v", j.Entries[0].Args)
	}
}

// A value already gone by apply time is the state we wanted; do not fail.
func TestCmdApplyToleratesAlreadyCleared(t *testing.T) {
	tqHome(t)
	useGOOS(t, "windows")
	fr := newFakeReg()
	fr.set(autoRunKey, autoRunName, RegValue{Type: "REG_SZ", Data: legacyAutoRun})
	d := Deps{Reg: fr.run}
	j, err := Open("20260830-100003")
	if err != nil {
		t.Fatal(err)
	}
	step := cmdStep{}
	p, err := step.Plan(d)
	if err != nil {
		t.Fatal(err)
	}
	delete(fr.vals, autoRunKey+`\`+autoRunName)
	if err := step.Apply(d, p, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(j.Entries) != 0 {
		t.Fatalf("journalled a mutation that was not needed: %+v", j.Entries)
	}
}

func TestCmdStepIsRegistered(t *testing.T) {
	steps, err := Steps([]string{"cmd"})
	if err != nil {
		t.Fatalf("Steps: %v", err)
	}
	if len(steps) != 1 || steps[0].Name() != "cmd" {
		t.Fatalf("Steps([cmd]) = %+v", steps)
	}
}
