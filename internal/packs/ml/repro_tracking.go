package ml

// ml/run-tracking (spec C §4.3, warn only): a training run logs its params,
// metrics and code version.

import (
	"context"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

const ruleRunTracking = "run-tracking"

func runTracking(ctx context.Context, in *packs.Input) packs.Result {
	files, fs := selectFiles(ctx, in, false)
	n := 0
	for _, s := range files {
		if !s.training {
			continue
		}
		n++
		if !tracks(s.f) && !s.f.suppressed(-1, ruleRunTracking, "ml/run-tracking") {
			fs = append(fs, s.f.find(-1, packs.LevelWarn, ruleRunTracking,
				"this training run logs no params and metrics (mlflow.autolog or mlflow.log_params + log_metrics, wandb, dvclive, or a run config written next to the metrics)"))
		}
	}
	return packs.Result{Status: packs.OK, Detail: "run tracking checked in " + plural(n, "training file"), Findings: fs}
}

// tracks reports whether a file logs params and metrics through a tracker,
// or writes a run config next to its metrics.
func tracks(f *pyFile) bool {
	var params, metrics, autolog, config, metricsFile bool
	for i, l := range f.Lines {
		strs := strings.ToLower(strings.Join(l.Strings, " "))
		for _, c := range f.Calls[i] {
			r := strings.ToLower(c.Receiver)
			switch c.Name {
			case "autolog":
				autolog = true
			case "log_params", "log_param", "log_hyperparams", "log_parameters":
				params = true
			case "log_metrics", "log_metric":
				metrics = true
			case "init":
				if strings.Contains(r, "wandb") {
					if _, ok := c.kw("config"); ok {
						params = true
					}
				}
				if strings.Contains(r, "task") { // ClearML Task.init logs both
					params, metrics = true, true
				}
			case "log":
				if strings.Contains(r, "wandb") || strings.Contains(r, "run") {
					metrics = true
				}
			case "dump", "safe_dump", "to_json", "write_text", "save":
				if strings.Contains(strs, "config") || strings.Contains(strs, "params") {
					config = true
				}
				if strings.Contains(strs, "metrics") {
					metricsFile = true
				}
			}
		}
		if strings.Contains(l.Code, "wandb.config") {
			params = true
		}
	}
	return autolog || (params && metrics) || (config && metricsFile)
}
