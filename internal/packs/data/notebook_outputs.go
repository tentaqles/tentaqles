package data

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// data/notebook-outputs (spec C §3.4, §8): a committed notebook with stored
// outputs (warn), and an output whose text hits the redaction detectors
// (error: a commit is an exit). Findings name the detectors and counts,
// never the values. Fabric notebooks store no outputs in git.
const (
	ruleNotebookOutputs = "notebook-outputs"
	ruleNotebookPII     = "notebook-output-pii"
)

func notebookApplies(in *packs.Input) bool {
	return in.AnyChanged("*.ipynb") || in.AnyChanged(synapseGlobs...)
}

func runNotebookOutputs(_ context.Context, in *packs.Input) packs.Result {
	var fs findings
	n, skipped := 0, 0
	for _, p := range in.ChangedPaths() {
		isNB := ext(p) == ".ipynb"
		if !isNB && !(ext(p) == ".json" && matchAny(synapseGlobs, p)) {
			continue
		}
		b, err := in.ReadNew(p, maxPipelineFile+1)
		if err != nil || len(b) > maxPipelineFile {
			skipped++
			continue
		}
		f := scan.DetectNotebook(p, b)
		if f != scan.FormatIPynb && f != scan.FormatSynapse {
			continue
		}
		cells, err := scan.NotebookCells(p, b)
		if err != nil && len(cells) == 0 {
			fs.addFile(p, 1, packs.LevelWarn, ruleNotebookOutputs, "the notebook does not parse; its outputs were not checked")
			continue
		}
		n++
		withOut, first := 0, 0
		for _, c := range cells {
			if c.Outputs == 0 {
				continue
			}
			withOut++
			if first == 0 {
				first = c.Line
			}
			if c.OutputText == "" {
				continue
			}
			if hits := redact.CountAll(c.OutputText, []string{"all"}); len(hits) > 0 {
				fs.addFile(p, c.Line, packs.LevelError, ruleNotebookPII,
					fmt.Sprintf("the stored output of cell %d matches the redaction detectors (%s); clear the outputs before committing", c.Index+1, detectorList(hits)))
			}
		}
		if withOut > 0 {
			fs.addFile(p, first, packs.LevelWarn, ruleNotebookOutputs,
				fmt.Sprintf("%s with stored outputs; clear them before committing (jupyter nbconvert --clear-output, or nbstripout)", plural(withOut, "cell")))
		}
	}
	return result(n, "notebook", skipped, &fs)
}

func detectorList(hits map[string]int) string {
	keys := make([]string, 0, len(hits))
	for k := range hits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s x%d", k, hits[k])
	}
	return strings.Join(parts, ", ")
}
