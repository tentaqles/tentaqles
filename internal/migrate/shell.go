package migrate

import (
	"fmt"

	"github.com/tentaqles/tentaqles/internal/hooks"
)

// The shell step adopts hand-installed tq hooks.
//
// Before `tq hooks install` existed, the activation block was pasted into
// shell profiles by hand. Those blocks are functional but unmanaged: tq cannot
// upgrade or remove them, and `tq hooks install` refuses to append a second
// one. Adoption replaces the hand-written block with tq's marker-delimited
// managed block while keeping, verbatim, the legacy launcher the user still
// falls back to with TQ_ENABLED=0 and the `claude` wrapper they type every
// day. Everything outside the block — BOM, CRLF, unrelated aliases — survives
// byte for byte; internal/hooks does that work and this step only decides
// which profiles to hand it, journals the write, and reports what was dropped.

func init() { register(shellStep{}) }

type shellStep struct{}

func (shellStep) Name() string { return "shell" }

// Plan inspects every shell profile tq knows about. Only profiles that are
// unmanaged *and* recognisably hand-installed are adopted; anything else is
// either left silently alone (no hook at all) or reported for the user to
// handle by hand.
func (shellStep) Plan(d Deps) (Plan, error) {
	var p Plan
	profiles := hooks.ProfilesFn()
	for _, sh := range hooks.Shells {
		st := hooks.StatusOf(sh, profiles)
		switch st.State {
		case "present (unmanaged)":
			// handled below
		case "installed":
			p.Skipped = append(p.Skipped, fmt.Sprintf("%s: already managed by tq (%s)", sh, st.Profile))
			continue
		case "corrupt":
			p.Skipped = append(p.Skipped, fmt.Sprintf(
				"%s: manual: %s has only one of tq's two markers; run tq hooks remove to repair it", sh, st.Profile))
			continue
		case "unreadable":
			p.Skipped = append(p.Skipped, fmt.Sprintf("%s: manual: cannot read %s", sh, st.Profile))
			continue
		default:
			// "missing" / "no profile": nothing tq ever touched. Reporting
			// these would bury the real findings under zsh and fish noise.
			continue
		}

		cs, err := hooks.Adopt(sh, profiles)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", sh, err)
		}
		if !cs.Found || !cs.Changed {
			// The profile calls `tq activate` but not in a shape tq can rewrite
			// safely. Rewriting it anyway would risk mangling the user's own
			// code, so say what to do instead.
			p.Skipped = append(p.Skipped, fmt.Sprintf(
				"%s: manual: run tq hooks install after removing the old lines (%s)", sh, cs.Reason))
			continue
		}
		p.Changes = append(p.Changes, Change{
			Step:   "shell",
			Kind:   "adopt-hook",
			Path:   cs.Profile,
			Detail: cs.Detail(),
		})
		// Lines the managed block does not carry over — in practice the
		// hand-rolled PATH munging. tq's own installer puts its bin directory
		// on the persistent PATH instead, but the user has to be told, because
		// on a machine where that never happened `tq` would stop resolving.
		for _, dropped := range cs.Dropped {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s: dropped from the old block: %s", sh, dropped))
		}
	}
	return p, nil
}

// Apply adopts the profiles the plan named. It re-runs hooks.Adopt rather than
// carrying a ChangeSet over from Plan: the ChangeSet holds byte offsets into
// the file as it was read, so a profile edited in between would be rewritten
// from stale offsets. Re-reading also means a profile that no longer needs
// adopting is quietly left alone instead of being clobbered.
func (shellStep) Apply(d Deps, p Plan, j *Journal) error {
	planned := map[string]bool{}
	for _, c := range p.Changes {
		if c.Step == "shell" && c.Kind == "adopt-hook" {
			planned[c.Path] = true
		}
	}
	if len(planned) == 0 {
		return nil
	}
	profiles := hooks.ProfilesFn()
	for _, sh := range hooks.Shells {
		path, ok := profiles[sh]
		if !ok || !planned[path] {
			continue
		}
		if st := hooks.StatusOf(sh, profiles); st.State != "present (unmanaged)" {
			// Already adopted, removed, or replaced since the plan was shown.
			continue
		}
		cs, err := hooks.Adopt(sh, profiles)
		if err != nil {
			return fmt.Errorf("%s: %w", sh, err)
		}
		if !cs.Found || !cs.Changed {
			continue
		}
		// Journal before writing: BackupFile fsyncs the copy, then the entry
		// naming it becomes durable, then the file changes.
		backup, err := j.BackupFile(cs.Profile)
		if err != nil {
			return fmt.Errorf("%s: %w", sh, err)
		}
		if err := j.RecordWriteFile("shell", cs.Profile, backup); err != nil {
			return fmt.Errorf("%s: %w", sh, err)
		}
		if err := cs.Apply(); err != nil {
			return fmt.Errorf("%s: adopting %s: %w", sh, cs.Profile, err)
		}
	}
	return nil
}
