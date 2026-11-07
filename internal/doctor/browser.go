package doctor

import (
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/browser"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// browserChecks reads a client's browsers: list (ADR-0016): every named profile has a folder in
// that browser's user data folder, and the shims that send BROWSER, xdg-open and open to that
// profile are written. Both are warnings: the browser makes a profile the first time
// --profile-directory names it, and the shims come with the next render.
func browserChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	if !envplan.HasBrowsers(w) {
		return
	}
	for _, b := range w.Manifest.Browsers {
		exists, known := browser.ProfileExists(b.Name, b.Profile)
		if known && !exists {
			add("warn", "browser-profile-missing", w.Name,
				b.Name+` profile "`+b.Profile+`" has no folder in `+filepath.Clean(browser.UserDataRoot(b.Name)),
				"run `tentaqles open <url>` in the client's folder to make the browser create the profile, or correct the profile name in browsers: of the manifest")
		}
	}
	if !envplan.ShimsReady(w.Name) {
		add("warn", "browser-shims-missing", w.Name,
			"the browser shims (BROWSER, xdg-open, open) are not written for this client",
			"tq settings render "+w.Name)
	}
}
