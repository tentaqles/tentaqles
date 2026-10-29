package data

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// data/dagster-validate (spec C §3.4): external `dagster definitions
// validate` (timeout 120 s). It loads the code locations from workspace.yaml
// or pyproject.toml [tool.dagster] in the repo root and needs no network.
const dagsterTimeout = 120 // seconds

var (
	dagsterImportRe  = regexp.MustCompile(`(?m)^[ \t]*(?:from|import)[ \t]+dagster\b`)
	dagsterConfigs   = []string{"workspace.yaml", "dagster.yaml", "dg.toml", "pyproject.toml"}
	dagsterPyproject = regexp.MustCompile(`(?m)^\[tool\.(?:dagster|dg)\b`)
)

func dagsterApplies(in *packs.Input) bool {
	return in.AnyChanged("*.py") || in.AnyChanged(dagsterConfigs...)
}

// dagsterChanged reports whether the diff touches Dagster code or config.
func dagsterChanged(in *packs.Input) bool {
	for _, p := range in.ChangedPaths() {
		base := path.Base(p)
		switch {
		case base == "workspace.yaml" || base == "dagster.yaml" || base == "dg.toml":
			return true
		case base == "pyproject.toml":
			if b, err := in.ReadNew(p, 1<<20); err == nil && dagsterPyproject.Match(b) {
				return true
			}
		case ext(p) == ".py":
			if b, err := in.ReadNew(p, maxPipelineFile); err == nil && dagsterImportRe.Match(b) {
				return true
			}
		}
	}
	return false
}

func runDagsterValidate(ctx context.Context, in *packs.Input) packs.Result {
	if !dagsterChanged(in) {
		return packs.Result{Status: packs.Skip, Detail: "no Dagster code or config changed"}
	}
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "data", Tool: "dagster", Args: []string{"definitions", "validate"}, Dir: in.Root})
	tool := &packs.ToolRun{Name: "dagster", Version: out.Version, Path: out.Path, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
	switch {
	case err != nil:
		return packs.Result{Status: packs.Fail, Detail: "dagster did not run: " + firstLine(err.Error()), Tool: tool}
	case out.TimedOut:
		return packs.Result{Status: packs.Fail, Detail: "timed out", Tool: tool}
	case out.ExitCode != 0:
		return packs.Result{Status: packs.Fail, Detail: "dagster definitions validate failed:\n" + out.Tail(20, nil), Tool: tool}
	}
	return packs.Result{Status: packs.OK, Detail: "dagster definitions validate passed", Tool: tool}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
