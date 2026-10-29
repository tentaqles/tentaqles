package doctor

import (
	"fmt"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// PackFindings is the packs framework's section of tq doctor (spec C
// §2.13): declared tools, detection hints and each enabled pack's probes.
// internal/packs sets it from init, so doctor never imports packs (packs
// returns doctor.Finding, which would be an import cycle). Nil: no packs
// linked into this binary.
var PackFindings func(w *resolve.Workspace) []Finding

// packChecks adds the packs findings of one trusted workspace. A panic in
// a pack probe becomes one error finding instead of crashing doctor.
func packChecks(w *resolve.Workspace, add func(level, code, ws, msg, fix string)) {
	if PackFindings == nil || w == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			add("error", "packs-doctor-failed", w.Name, fmt.Sprint("the packs doctor failed: ", r), "")
		}
	}()
	for _, f := range PackFindings(w) {
		add(f.Level, f.Code, f.Workspace, f.Msg, f.Fix)
	}
}
