package doctor

import (
	"os"
	"path/filepath"
	"strings"
)

// unusedStateEntries are the entries under $TQ_HOME that tq wrote before
// 0.9.0 (the hub, the packs, the guard modes, the Jev cache and the build
// gate) and no longer reads. tq never deletes them; the user does.
var unusedStateEntries = []string{"hub", "packs", "guard", "guard.yaml", "decide", "build-gate"}

// manifestDroppedBlock warns when a loaded manifest still carries blocks tq
// 0.9.0 stopped reading (manifest.DroppedBlocks, recorded in Manifest.Ignored).
// The manifest loads regardless; removing a block changes its hash, so the
// fix ends with tq allow.
func manifestDroppedBlock(ws string, ignored []string, add func(level, code, ws, msg, fix string)) {
	if len(ignored) == 0 {
		return
	}
	add("warn", "manifest-dropped-block", ws,
		"manifest still carries blocks tq no longer reads (since 0.9.0): "+strings.Join(ignored, ", "),
		"remove them from the manifest, then tq allow "+ws)
}

// stateDirUnused warns once when the state folder still holds entries of the
// features removed in 0.9.0. It only lists what exists; nothing is deleted.
func stateDirUnused(home string, add func(level, code, ws, msg, fix string)) {
	var found []string
	for _, name := range unusedStateEntries {
		if _, err := os.Lstat(filepath.Join(home, name)); err == nil {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return
	}
	add("warn", "state-dir-unused", "",
		"unused since 0.9.0 under "+home+": "+strings.Join(found, ", ")+" (tq never deletes them)",
		"delete them yourself when you no longer want them")
}
