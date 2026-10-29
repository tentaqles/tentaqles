package ml

import (
	"os"
	"path/filepath"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// promoteDoctor: ml-card-missing, ml-drift-reference-missing,
// ml-promotion-command-missing (spec C §7).
func promoteDoctor(in *packs.Input) []doctor.Finding {
	st := load(in.Settings())
	var out []doctor.Finding
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(in.Root, filepath.FromSlash(p)))
		return err == nil
	}
	for _, m := range st.Models {
		if !exists(m.Card) {
			out = append(out, doctor.Finding{Level: "warn", Code: "ml-card-missing", Workspace: in.Workspace(),
				Msg: "model " + m.Name + " has no card at " + m.Card, Fix: "write it from the MODEL_CARD.md template (skill model-card)"})
		}
		if d := st.DriftFor(m); d.Reference != "" && !exists(d.Reference) {
			out = append(out, doctor.Finding{Level: "warn", Code: "ml-drift-reference-missing", Workspace: in.Workspace(),
				Msg: "model " + m.Name + ": drift.reference " + d.Reference + " is missing", Fix: "build the reference profile from the training snapshot (skill drift-check)"})
		}
	}
	if len(st.Promotion.Protected) > 0 && st.Promotion.Command == "" {
		out = append(out, doctor.Finding{Level: "warn", Code: "ml-promotion-command-missing", Workspace: in.Workspace(),
			Msg: "packs.ml.promotion.protected is set but promotion.command is not", Fix: "add the project's promotion command to the manifest, with {model} {version} {target}"})
	}
	return out
}
