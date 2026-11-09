package doctor

import (
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/codexconfig"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// codexGuardCheck reports a client with codex in its scope whose Codex config does not run the
// tentaqles hooks as a session needs them: the check tentaqles run makes before a codex session,
// on the same CODEX_HOME, with the same fix lines. The finding ids are codex-guard-missing,
// codex-guard-disabled, codex-guard-untrusted, and codex-guard-unreadable for a config that cannot
// be read.
func codexGuardCheck(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	if w.Manifest == nil || !slices.Contains(w.Manifest.Scope(), "codex") {
		return
	}
	g := codexconfig.CheckGuard(envplan.Desired(w)["CODEX_HOME"], w.Name)
	if g.OK {
		return
	}
	code := "codex-guard-untrusted"
	switch {
	case g.Err != nil:
		code = "codex-guard-unreadable"
	case g.Trust == codexconfig.Missing:
		code = "codex-guard-missing"
	case g.Trust == codexconfig.Disabled:
		code = "codex-guard-disabled"
	}
	add("error", code, w.Name, "no codex session starts with the tentaqles guard: "+g.Why, strings.Join(g.Fix, "\n    "))
}
