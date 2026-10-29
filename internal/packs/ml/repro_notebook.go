package ml

// ml/notebook-outputs (spec C §4.3, as data/notebook-outputs): a committed
// notebook keeps outputs (warn); an output that hits the redaction
// detectors is an error, because the commit is an exit. Messages carry
// cell numbers and detector names, never output text.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/redact"
)

const (
	ruleNotebookOutputs = "notebook-outputs"
	ruleNotebookPII     = "notebook-output-pii"
)

func runNotebookOutputs(ctx context.Context, in *packs.Input) packs.Result {
	var fs []packs.Finding
	n := 0
	for _, p := range in.ChangedPaths() {
		if ctx.Err() != nil || n >= maxFiles {
			break
		}
		if !strings.HasSuffix(strings.ToLower(p), ".ipynb") {
			continue
		}
		n++
		b, err := in.ReadNew(p, maxFileBytes)
		if err != nil {
			fs = append(fs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "cannot read: " + shortErr(err)})
			continue
		}
		cells, err := scan.NotebookCells(p, b)
		if err != nil {
			fs = append(fs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "not a readable notebook: " + shortErr(err)})
			continue
		}
		fs = append(fs, notebookOutputs(p, cells)...)
	}
	return packs.Result{Status: packs.OK, Detail: "outputs checked in " + lintPlural(n, "notebook"), Findings: fs}
}

func notebookOutputs(p string, cells []scan.Cell) []packs.Finding {
	var out []packs.Finding
	with, first := 0, 0
	for _, c := range cells {
		if c.Outputs == 0 {
			continue
		}
		if with == 0 {
			first = c.Line
		}
		with++
		if c.OutputText == "" {
			continue
		}
		hits := redact.CountAll(c.OutputText, []string{"all"})
		if len(hits) == 0 {
			continue
		}
		names := make([]string, 0, len(hits))
		for k := range hits {
			names = append(names, k)
		}
		sort.Strings(names)
		out = append(out, packs.Finding{File: p, Line: c.Line, Level: packs.LevelError, Rule: ruleNotebookPII,
			Message: fmt.Sprintf("cell %d: a stored output matches the %s detector; clear the output before committing", c.Index+1, strings.Join(names, ", "))})
	}
	if with > 0 {
		out = append([]packs.Finding{{File: p, Line: first, Level: packs.LevelWarn, Rule: ruleNotebookOutputs,
			Message: "stored outputs in " + lintPlural(with, "cell") + "; clear them (nbstripout or jupyter nbconvert --clear-output)"}}, out...)
	}
	return out
}
