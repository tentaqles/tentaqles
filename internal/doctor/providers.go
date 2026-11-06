package doctor

import (
	"errors"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// providerFileFindings lists every user provider file tq did not load (ADR-0007 rounds 3 and
// 4): an override of a built-in id is a warning (the built-in lists stay in force), a new id
// an error. The message names the workspaces whose scope names the id (each is refused every
// call), or none, and says the fix once, in the Fix column.
func providerFileFindings(add func(level, code, ws, msg, fix string), all []resolve.Workspace) {
	_, errs := providers.LoadAll()
	for _, e := range errs {
		var fe *providers.FileError
		if !errors.As(e, &fe) {
			add("error", "provider-file-invalid", "", e.Error(), "fix the files under ~/.tentaqles/providers, then run tq providers list")
			continue
		}
		msg := fe.Detail()
		level := "error"
		if fe.Builtin {
			level = "warn"
			msg += " (tq uses the built-in " + fe.ID + " lists instead)"
		} else if names := workspacesNaming(all, fe.ID); len(names) > 0 {
			msg += " (refused every call in " + strings.Join(names, ", ") + ", whose scope names " + fe.ID + ")"
		}
		add(level, "provider-file-invalid", "", msg, fe.Fix())
	}
}

// workspacesNaming lists the workspaces whose scope, identities or git provider name id.
func workspacesNaming(all []resolve.Workspace, id string) []string {
	var out []string
	for _, w := range all {
		m := w.Manifest
		if m == nil {
			continue
		}
		named := strings.EqualFold(strings.TrimSpace(m.Git.Provider), id)
		for _, s := range append(m.Scope(), m.ExtraScope...) {
			named = named || s == id
		}
		if _, ok := m.Identities[id]; ok {
			named = true
		}
		if named {
			out = append(out, w.Name)
		}
	}
	sort.Strings(out)
	return out
}
