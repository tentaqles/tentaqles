package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hooks"
)

// The shell step is tested against byte-exact copies of the dev machine's real
// profiles, which live in internal/hooks/testdata. Nothing here reads the
// developer's actual profiles: hooks.ProfilesFn is pointed at a temp tree for
// the duration of every test.

// shellFixture names a captured profile and where it lives under a fake home.
type shellFixture struct {
	shell   hooks.Shell
	fixture string
	rel     []string
}

var realShellFixtures = []shellFixture{
	{"powershell", "real_powershell_profile.ps1", []string{"Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"}},
	{"pwsh", "real_pwsh_profile.ps1", []string{"Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"}},
	{"bash", "real_bashrc", []string{".bashrc"}},
}

func hooksFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "hooks", "testdata", name))
	if err != nil {
		t.Fatalf("reading hooks fixture %s: %v", name, err)
	}
	return raw
}

// stageProfiles copies fixtures into home and points hooks.ProfilesFn at them
// for the rest of the test. Shells with no staged file map to a path inside
// home that does not exist, so nothing can reach the real machine.
func stageProfiles(t *testing.T, home string, fixtures []shellFixture) hooks.Profiles {
	t.Helper()
	p := hooks.Profiles{
		"bash":       filepath.Join(home, ".bashrc"),
		"zsh":        filepath.Join(home, ".zshrc"),
		"fish":       filepath.Join(home, ".config", "fish", "config.fish"),
		"pwsh":       filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"),
		"powershell": filepath.Join(home, "Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"),
	}
	for _, f := range fixtures {
		path := filepath.Join(append([]string{home}, f.rel...)...)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, hooksFixture(t, f.fixture), 0o644); err != nil {
			t.Fatal(err)
		}
		p[f.shell] = path
	}
	useProfiles(t, p)
	return p
}

// useProfiles swaps hooks.ProfilesFn for the duration of the test.
func useProfiles(t *testing.T, p hooks.Profiles) {
	t.Helper()
	old := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return p }
	t.Cleanup(func() { hooks.ProfilesFn = old })
}

func writeProfileFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func changePaths(p Plan, kind string) []string {
	var out []string
	for _, c := range p.Changes {
		if c.Kind == kind {
			out = append(out, c.Path)
		}
	}
	return out
}

func joined(ss []string) string { return strings.Join(ss, "\n") }

// ---------------------------------------------------------------------------
// plan
// ---------------------------------------------------------------------------

// The machine fact this step exists for: three profiles are adopted, not two.
func TestShellPlanAdoptsThreeRealProfiles(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, realShellFixtures)

	plan, err := shellStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) != 3 {
		t.Fatalf("got %d changes, want 3:\n%s", len(plan.Changes), joined(changePaths(plan, "adopt-hook")))
	}
	want := map[string]bool{
		p["powershell"]: false,
		p["pwsh"]:       false,
		p["bash"]:       false,
	}
	for _, c := range plan.Changes {
		if c.Step != "shell" {
			t.Errorf("change Step = %q, want shell", c.Step)
		}
		if c.Kind != "adopt-hook" {
			t.Errorf("change Kind = %q, want adopt-hook", c.Kind)
		}
		if _, ok := want[c.Path]; !ok {
			t.Errorf("unexpected change path %q", c.Path)
			continue
		}
		want[c.Path] = true
		if !strings.Contains(c.Detail, "replace hand-installed block") {
			t.Errorf("Detail for %s = %q", c.Path, c.Detail)
		}
	}
	for path, seen := range want {
		if !seen {
			t.Errorf("no adopt-hook change for %s", path)
		}
	}

	// The PowerShell profiles keep a legacy branch and a claude wrapper; the
	// bashrc has neither, so its detail is shorter.
	for _, c := range plan.Changes {
		if c.Path == p["bash"] {
			if strings.Contains(c.Detail, "legacy") {
				t.Errorf("bash detail should not mention a legacy branch: %q", c.Detail)
			}
			continue
		}
		if !strings.Contains(c.Detail, "keep legacy launcher verbatim") ||
			!strings.Contains(c.Detail, "carry over the claude wrapper") {
			t.Errorf("powershell detail = %q", c.Detail)
		}
	}

	// Plan must not write anything.
	for _, f := range realShellFixtures {
		got, err := os.ReadFile(p[f.shell])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, hooksFixture(t, f.fixture)) {
			t.Errorf("Plan modified %s", p[f.shell])
		}
	}
}

// Adoption drops the hand-rolled PATH munging from all three profiles. That is
// safe on this machine (the bin dir is on the persistent user PATH) but the
// user must still be told.
func TestShellPlanWarnsAboutDroppedPathLines(t *testing.T) {
	home := tqHome(t)
	stageProfiles(t, home, realShellFixtures)

	plan, err := shellStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	all := joined(plan.Warnings)
	for _, sh := range []string{"powershell", "pwsh", "bash"} {
		if !strings.Contains(all, sh+": dropped from the old block:") {
			t.Errorf("no dropped-line warning for %s; warnings:\n%s", sh, all)
		}
	}
	if !strings.Contains(all, "$tqBin") {
		t.Errorf("expected the PowerShell PATH munging to be named; warnings:\n%s", all)
	}
	if !strings.Contains(all, `case ":$PATH:"`) {
		t.Errorf("expected the bash PATH munging to be named; warnings:\n%s", all)
	}
}

func TestShellPlanSkipsUnrecognisedProfile(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, nil)
	// Unmanaged (it calls tq activate) but with no hand-installed block shape.
	writeProfileFile(t, p["bash"], "# my own thing\neval \"$(tq activate bash)\"\n")

	plan, err := shellStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", plan.Changes)
	}
	got := joined(plan.Skipped)
	if !strings.Contains(got, "bash: manual: run tq hooks install after removing the old lines") {
		t.Errorf("skip line = %q", got)
	}
	if !strings.Contains(got, hooks.ReasonUnrecognised) {
		t.Errorf("skip line should carry the reason, got %q", got)
	}
}

func TestShellPlanIgnoresProfilesWithNoHook(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, nil)
	writeProfileFile(t, p["bash"], "export PS1='$ '\n")

	plan, err := shellStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) != 0 || len(plan.Warnings) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("expected a silent empty plan, got %+v", plan)
	}
}

func TestShellPlanReportsAlreadyManaged(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, nil)
	writeProfileFile(t, p["bash"], hooks.Block("bash"))

	plan, err := shellStep{}.Plan(Deps{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("expected no changes, got %+v", plan.Changes)
	}
	if !strings.Contains(joined(plan.Skipped), "bash: already managed by tq") {
		t.Errorf("skipped = %v", plan.Skipped)
	}
}

// ---------------------------------------------------------------------------
// apply
// ---------------------------------------------------------------------------

func TestShellApplyMakesStatusInstalledAndRestoreIsByteExact(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, realShellFixtures)

	j, err := Open("20260830-000000")
	if err != nil {
		t.Fatal(err)
	}
	step := shellStep{}
	plan, err := step.Plan(Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := step.Apply(Deps{}, plan, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, f := range realShellFixtures {
		if st := hooks.StatusOf(f.shell, p); st.State != "installed" {
			t.Errorf("%s: StatusOf = %q, want installed", f.shell, st.State)
		}
	}

	psRaw, err := os.ReadFile(p["powershell"])
	if err != nil {
		t.Fatal(err)
	}
	ps := string(psRaw)
	for _, want := range []string{
		hooks.LegacyStartMarker,
		hooks.LegacyEndMarker,
		"function Get-ClientContext",
		hooks.CarryComment,
		"--dangerously-skip-permissions",
	} {
		if !strings.Contains(ps, want) {
			t.Errorf("adopted powershell profile is missing %q", want)
		}
	}
	if !bytes.HasPrefix(psRaw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("adopted powershell profile lost its UTF-8 BOM")
	}
	if !strings.Contains(ps, "\r\n") {
		t.Error("adopted powershell profile lost its CRLF endings")
	}

	// Every mutation was journalled as a write-file before it happened.
	var writes int
	for _, e := range j.Entries {
		if e.Step == "shell" && e.Op == OpWriteFile {
			writes++
			if e.Args["Backup"] == "" || e.Args["SHA256"] == "" {
				t.Errorf("entry %d has no verified backup: %+v", e.Seq, e.Args)
			}
		}
	}
	if writes != 3 {
		t.Fatalf("journalled %d write-file entries, want 3", writes)
	}

	if _, err := j.Restore(Runner{}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, f := range realShellFixtures {
		got, err := os.ReadFile(p[f.shell])
		if err != nil {
			t.Fatal(err)
		}
		if want := hooksFixture(t, f.fixture); !bytes.Equal(got, want) {
			t.Errorf("%s: restore is not byte-exact (%d bytes, want %d)", f.shell, len(got), len(want))
		}
	}
}

// A profile edited between plan and apply must not be rewritten from the stale
// plan: Apply re-reads and re-derives the change.
func TestShellApplyRereadsProfileAfterPlan(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, realShellFixtures)

	j, err := Open("20260830-000001")
	if err != nil {
		t.Fatal(err)
	}
	step := shellStep{}
	plan, err := step.Plan(Deps{})
	if err != nil {
		t.Fatal(err)
	}

	// The user installs the managed block by hand before applying.
	managed := "# mine\n" + hooks.Block("bash")
	writeProfileFile(t, p["bash"], managed)

	if err := step.Apply(Deps{}, plan, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(p["bash"])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != managed {
		t.Errorf("bashrc was rewritten from the stale plan:\n%s", got)
	}
	for _, e := range j.Entries {
		if e.Args["Path"] == p["bash"] {
			t.Errorf("journalled a write for a profile that no longer needed one: %+v", e)
		}
	}
	// The other two are still adopted.
	for _, sh := range []hooks.Shell{"powershell", "pwsh"} {
		if st := hooks.StatusOf(sh, p); st.State != "installed" {
			t.Errorf("%s: StatusOf = %q, want installed", sh, st.State)
		}
	}
}

// A ChangeSet's Start/End are byte offsets into the content it was built from.
// Carrying one across the plan/apply boundary would splice the managed block at
// the wrong place once the file has shifted; Apply must re-derive it.
func TestShellApplyUsesTheProfileAsItIsAtApplyTime(t *testing.T) {
	home := tqHome(t)
	p := stageProfiles(t, home, realShellFixtures)

	j, err := Open("20260830-000003")
	if err != nil {
		t.Fatal(err)
	}
	step := shellStep{}
	plan, err := step.Plan(Deps{})
	if err != nil {
		t.Fatal(err)
	}

	// Prepend a line, shifting every offset in the file the plan looked at.
	const prefix = "# added after the plan was made\r\n"
	orig, err := os.ReadFile(p["powershell"])
	if err != nil {
		t.Fatal(err)
	}
	shifted := append([]byte{0xEF, 0xBB, 0xBF}, append([]byte(prefix), bytes.TrimPrefix(orig, []byte{0xEF, 0xBB, 0xBF})...)...)
	if err := os.WriteFile(p["powershell"], shifted, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := step.Apply(Deps{}, plan, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := os.ReadFile(p["powershell"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, append([]byte{0xEF, 0xBB, 0xBF}, prefix...)) {
		t.Errorf("the line added after the plan was lost or moved; head =\n%q", head(got, 120))
	}
	if st := hooks.StatusOf("powershell", p); st.State != "installed" {
		t.Errorf("StatusOf = %q, want installed", st.State)
	}
	if s := string(got); !strings.Contains(s, hooks.LegacyStartMarker) || strings.Contains(s, hooks.LegacyHeaderPrefix) {
		t.Error("the hand-installed block was not cleanly replaced")
	}
}

func head(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func TestShellApplyWithNoChangesDoesNothing(t *testing.T) {
	home := tqHome(t)
	stageProfiles(t, home, nil)
	j, err := Open("20260830-000002")
	if err != nil {
		t.Fatal(err)
	}
	if err := (shellStep{}).Apply(Deps{}, Plan{}, j); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(j.Entries) != 0 {
		t.Fatalf("journal is not empty: %+v", j.Entries)
	}
}

func TestShellStepIsRegistered(t *testing.T) {
	steps, err := Steps([]string{"shell"})
	if err != nil {
		t.Fatalf("Steps: %v", err)
	}
	if len(steps) != 1 || steps[0].Name() != "shell" {
		t.Fatalf("Steps([shell]) = %+v", steps)
	}
}
