package migrate

import (
	"fmt"
	"runtime"
	"strings"
)

// The cmd step clears the legacy cmd.exe AutoRun hook.
//
// The pre-tq setup made cmd.exe source a shim script on every launch by
// pointing HKCU\Software\Microsoft\Command Processor\AutoRun at it. tq
// replaces that mechanism with per-tool shims installed by `tq shims install`,
// so the migration's job here is just to take the AutoRun value out of the way
// — journalling it first, with its registry type, so `tq uninstall --restore`
// can put it back exactly as it was. The shims directory itself is left alone:
// deleting it is a separate, user-driven decision.

const (
	autoRunKey  = `HKCU\Software\Microsoft\Command Processor`
	autoRunName = "AutoRun"
)

// cmdGOOS is runtime.GOOS behind a seam so the not-windows path is testable
// from a Windows box (and vice versa).
var cmdGOOS = runtime.GOOS

func init() { register(cmdStep{}) }

type cmdStep struct{}

func (cmdStep) Name() string { return "cmd" }

func autoRunPath() string { return autoRunKey + `\` + autoRunName }

// Plan reads the AutoRun value through Deps.Reg. It never shells out directly,
// so tests run against a fake hive rather than the developer's registry.
func (cmdStep) Plan(d Deps) (Plan, error) {
	if cmdGOOS != "windows" {
		return Plan{Skipped: []string{"cmd: not windows"}}, nil
	}
	if d.Reg == nil {
		return Plan{Skipped: []string{"cmd: no registry runner configured"}}, nil
	}
	cur, present, err := RegQuery(Runner{Reg: d.Reg}, autoRunKey, autoRunName)
	if err != nil {
		return Plan{}, fmt.Errorf("reading %s: %w", autoRunPath(), err)
	}
	if !present {
		return Plan{Skipped: []string{fmt.Sprintf("cmd: %s is not set", autoRunPath())}}, nil
	}
	if !SupportedRegType(cur.Type) {
		// Without a faithful record of the old value there is no way back, and
		// an irreversible change is not one tq makes on the user's behalf.
		return Plan{Skipped: []string{fmt.Sprintf(
			"cmd: manual: %s is a %s value, which tq cannot record and restore byte-for-byte; clear it yourself if you want it gone",
			autoRunPath(), cur.Type)}}, nil
	}

	p := Plan{Changes: []Change{{
		Step:   "cmd",
		Kind:   "clear-autorun",
		Path:   autoRunPath(),
		Detail: cur.Data,
		// Every cmd.exe the user opens runs this today. Removing it is
		// journalled and reversible, but they should still read it first.
		Danger: true,
	}}}
	if !looksLikeLegacyShim(cur.Data) {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"cmd: %s does not look like a tq or legacy-identity shim (%s); clearing it also disables whatever else it starts, clink for instance",
			autoRunPath(), cur.Data))
	}
	return p, nil
}

// looksLikeLegacyShim reports whether an AutoRun value is one the tentaqles
// setup installed, as opposed to something else the user relies on.
func looksLikeLegacyShim(data string) bool {
	l := strings.ToLower(data)
	for _, marker := range []string{"tentaqles", "cli-identities", `\tq.exe`, `\tq `} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// Apply journals the current AutoRun value — its type included — and then
// deletes it.
func (cmdStep) Apply(d Deps, p Plan, j *Journal) error {
	var planned *Change
	for i := range p.Changes {
		if p.Changes[i].Step == "cmd" && p.Changes[i].Kind == "clear-autorun" {
			planned = &p.Changes[i]
			break
		}
	}
	if planned == nil {
		return nil
	}
	if cmdGOOS != "windows" {
		return fmt.Errorf("refusing to touch the registry on %s", cmdGOOS)
	}
	if d.Reg == nil {
		return fmt.Errorf("no registry runner configured")
	}

	// Re-read: the plan the user approved may be minutes or days old.
	cur, present, err := RegQuery(Runner{Reg: d.Reg}, autoRunKey, autoRunName)
	if err != nil {
		return fmt.Errorf("reading %s: %w", autoRunPath(), err)
	}
	if !present {
		// Someone already cleared it. That is the state we wanted.
		return nil
	}
	if cur.Data != planned.Detail && !d.Force {
		return fmt.Errorf(
			"%s changed since the plan was made (now %q, planned %q); re-run tq migrate to see the new value, or pass --force to clear it anyway",
			autoRunPath(), cur.Data, planned.Detail)
	}
	if !SupportedRegType(cur.Type) {
		return fmt.Errorf("%s is a %s value, which tq cannot record and restore; refusing to clear it", autoRunPath(), cur.Type)
	}
	if err := j.RecordRegSet("cmd", autoRunKey, autoRunName, cur, true); err != nil {
		return err
	}
	if _, err := d.Reg("delete", autoRunKey, autoRunName, RegValue{}); err != nil {
		return fmt.Errorf("clearing %s: %w", autoRunPath(), err)
	}
	return nil
}
