package data

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"gopkg.in/yaml.v3"
)

// Workstream D3 (spec C §3.4, §3.5): pipeline lints, Fabric and Synapse
// artifacts, notebook outputs, dagster validate, the `pipeline` triage flag
// with the pipeline-reviewer, and the tq/pipeline-run guard.

const pipelineQuestion = "Does this change alter a data pipeline, dbt model or scheduled job in a way that could duplicate, drop or reorder rows on a rerun or backfill?"

func init() {
	packs.RegisterPack(packs.Pack{
		Name:       "data",
		ConfigKeys: map[string]func(yaml.Node) error{"pipelines": validatePipelines},
		Merge: map[string]packs.MergeRule{
			"pipelines.paths": packs.Union(),
			// A repo must not declare its own masking functions: that would
			// silence data/pipeline-pii-output.
			"pipelines.masking": {},
		},
		Flags: []packs.Flag{{
			Name:      "pipeline",
			Reviewers: []string{"pipeline-reviewer"},
			Raise:     raisePipeline,
			Question:  pipelineQuestion,
		}},
		Hints: []packs.Hint{
			{Glob: "*.py", Contains: `(?m)^\s*from\s+airflow\b`, Why: "Airflow DAGs"},
			{Glob: "*.py", Contains: `(?m)^\s*(?:import|from)\s+dagster\b`, Why: "Dagster code"},
			{Glob: "*.py", Contains: `(?m)^\s*from\s+prefect\b`, Why: "Prefect flows"},
			{Glob: ".platform", Contains: `"type"\s*:\s*"(?:Notebook|DataPipeline|Lakehouse|Warehouse)"`, Why: "Microsoft Fabric items (git integration)"},
			{Glob: "**/notebook/*.json", Contains: `"nbformat"`, Why: "Azure Synapse notebooks (git integration)"},
			{Glob: "**/pipeline/*.json", Contains: `"activities"`, Why: "Azure Synapse or Data Factory pipelines (git integration)"},
		},
	})

	builtin := func(id string, applies func(*packs.Input) bool, run func(context.Context, *packs.Input) packs.Result) {
		packs.Register(packs.Check{ID: id, Pack: "data", Kind: packs.Builtin, Applies: applies, Run: run})
	}
	builtin("data/airflow-dag", pipelineApplies, runAirflowDAG)
	builtin("data/pipeline-backfill", pipelineApplies, runPipelineBackfill)
	builtin("data/pipeline-idempotency", pipelineApplies, runPipelineIdempotency)
	builtin("data/pipeline-pii-output", pipelineApplies, runPipelinePII)
	builtin("data/fabric-artifacts", fabricApplies, runFabricArtifacts)
	builtin("data/synapse-artifacts", synapseApplies, runSynapseArtifacts)
	builtin("data/notebook-outputs", notebookApplies, runNotebookOutputs)
	packs.Register(packs.Check{ID: "data/dagster-validate", Pack: "data", Kind: packs.External, Tool: "dagster", Env: dagsterEnv,
		Timeout: dagsterTimeout * time.Second, Applies: dagsterApplies, Run: runDagsterValidate})

	packs.RegisterGuard(packs.Guard{ID: "tq/pipeline-run", Pack: "data", Events: []string{"Bash", "PowerShell"}, Check: withScripts(pipelineRunGuard, pipelineToolRe, nil, "an orchestrator CLI (airflow, dagster, prefect, az ... create-run)")})
}

// raisePipeline: changed files under the dbt model paths, pipelines.paths,
// or Fabric and Synapse artifacts (paths and config only).
func raisePipeline(in *packs.Input) bool {
	globs := append(append(append([]string{}, pipelineGlobs(in)...), fabricGlobs...), synapseGlobs...)
	return in.AnyChanged(append(globs, dbtModelGlobs(in)...)...)
}

// dbtModelGlobs approximates the dbt model-paths from config: the default
// `models/` under packs.data.dbt.project (or the repo root).
func dbtModelGlobs(in *packs.Input) []string {
	if p := strings.Trim(in.Settings().String("dbt.project"), "/"); p != "" && p != "." {
		return []string{p + "/models/**"}
	}
	return []string{"models/**"}
}

var maskingNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// validatePipelines checks packs.data.pipelines: {paths: [globs], masking: [function names]}.
func validatePipelines(n yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping with paths and masking", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch k.Value {
		case "paths", "masking":
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: %s must be a list", v.Line, k.Value)
			}
			for _, it := range v.Content {
				if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
					return fmt.Errorf("line %d: %s entries must be non-empty strings", it.Line, k.Value)
				}
				if k.Value == "masking" && !maskingNameRe.MatchString(it.Value) {
					return fmt.Errorf("line %d: masking entry %q is not a function name", it.Line, it.Value)
				}
				if k.Value == "paths" && strings.Contains(it.Value, "..") {
					return fmt.Errorf("line %d: paths entry %q must stay inside the repo", it.Line, it.Value)
				}
			}
		default:
			return fmt.Errorf("line %d: unknown key %q (known: paths, masking)", k.Line, k.Value)
		}
	}
	return nil
}
