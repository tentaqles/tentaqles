package doctor

import (
	"runtime"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// The protected manifest location (ADR-0002): the manifest lives under
// $TQ_HOME/identities/<ws>/manifest.yaml and the identity root keeps a
// name-only marker. Both findings here are warnings: an identity whose
// manifest still sits in its root keeps working exactly as before until the
// human runs the migration.

func init() {
	Register(Check{Name: "protected", Global: protectedACL})
}

// protectGOOS is runtime.GOOS behind a seam so the Windows-only ACL check is
// testable everywhere.
var protectGOOS = runtime.GOOS

// manifestLayout reports a manifest that is not (only) in the protected
// location: manifest-in-root for the old layout, manifest-root-stale for a
// full manifest left in the root next to the protected copy tq reads.
func manifestLayout(w *resolve.Workspace, add AddFunc) {
	switch {
	case w.StaleRoot != "":
		add("warn", "manifest-root-stale", w.Name,
			w.StaleRoot+" is an old copy of the manifest left in the identity root; tq reads "+w.ManifestPath+" and nothing reads the root copy",
			"tq migrate manifests --apply")
	case !w.Protected:
		add("warn", "manifest-in-root", w.Name,
			"the manifest still lives in the identity root ("+w.ManifestPath+"), where a session can edit it; the protected location is "+paths.ManifestPath(w.Name),
			"tq migrate manifests --apply")
	}
}

// protectedACL reports, on Windows, whether $TQ_HOME was restricted to the
// current user (internal/protect records the one icacls run).
func protectedACL(_ Deps, add AddFunc) {
	if protectGOOS != "windows" {
		return
	}
	home := paths.Home()
	rec, err := protect.ReadRecord()
	switch {
	case err != nil:
		add("warn", "protected-acl", "", "cannot read the ACL record for "+home+": "+err.Error(),
			"delete "+paths.ACLRecord()+", then tq migrate manifests --apply records a new icacls run; or run: "+protect.Command(home, ""))
	case rec == nil:
		add("warn", "protected-acl", "", home+" keeps its inherited permissions: tq has not restricted it to your user yet",
			"tq migrate manifests --apply (runs icacls once), or run: "+protect.Command(home, ""))
	case !rec.OK:
		add("warn", "protected-acl", "", "restricting "+home+" to your user failed: "+rec.Detail,
			"run: "+protect.Command(home, "")+", then delete "+paths.ACLRecord()+" so the next tq add records a new attempt")
	default:
		add("ok", "protected-acl", "", home+" is restricted to your user (icacls, "+rec.At.Format("2006-01-02")+")", "")
	}
}
