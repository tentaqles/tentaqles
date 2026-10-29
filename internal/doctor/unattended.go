package doctor

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/guardcfg"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// unattendedChecks warns about a guard.unattended block that does nothing, names
// unknown rules, or widens what an agent can do with a cloud identity.
func unattendedChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	if w == nil || w.Manifest == nil || !w.Manifest.Guard.Unattended.Configured() {
		return
	}
	effective := trust.IsBypassAllowed(w.Hash)
	if !effective {
		add("warn", "unattended-not-effective", w.Name, "guard.unattended is configured but bypass is not trusted: every rule still applies", "tq allow --bypass "+w.Name)
	}
	g, _ := guardcfg.Load()
	if bad := guardcfg.UnknownKeepAsking(w.Manifest, g); len(bad) > 0 {
		add("warn", "unattended-unknown-rule", w.Name, "guard.unattended.keep_asking names unknown rules: "+strings.Join(bad, ", "), "fix the ids in .tentaqles.yaml (see tq guard list)")
	}
	if effective && w.Manifest.HasCloudIdentity() {
		add("warn", "unattended-cloud", w.Name, "unattended + cloud identity: Claude can run cloud CLIs without asking in this workspace", "")
	}
}
