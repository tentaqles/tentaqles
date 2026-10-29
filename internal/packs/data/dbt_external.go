package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// Tool timeouts of the external dbt gates (spec C §3.3); packs.data.timeouts
// overrides them, capped at 1800 s by the runner.
const (
	dbtParseTimeout = 180 * time.Second
	dbtBuildTimeout = 1200 * time.Second
	sqlfluffTimeout = 120 * time.Second
	// dbtMinVersion is the floor when dbt is declared: data_tests: and unit
	// tests arrived in dbt 1.8.
	dbtMinVersion = "1.8.0"
)

func toolResult(out toolrun.Output) *packs.ToolRun {
	return &packs.ToolRun{Name: "", Version: out.Version, Path: out.Path, ExitCode: out.ExitCode,
		DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
}

func absDir(root, rel string) string {
	if rel == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// scratchArgs points dbt's artifacts and logs at the run's scratch dir, so
// the DoD's stable_tree check never sees target/ or logs/ change.
func scratchArgs(scratch string, i int) []string {
	d := filepath.Join(scratch, fmt.Sprintf("dbt-%d", i))
	return []string{"--target-path", filepath.Join(d, "target"), "--log-path", filepath.Join(d, "logs")}
}

func runToolOnce(ctx context.Context, in *packs.Input, tool string, args []string, dir string, timeout time.Duration) (toolrun.Output, *packs.Result) {
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "data", Tool: tool, Args: args, Dir: dir, Timeout: timeout})
	if err != nil {
		return out, &packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("%s did not run: %v", tool, err)}
	}
	if out.TimedOut {
		tr := toolResult(out)
		tr.Name = tool
		return out, &packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("%s %s timed out", tool, args[0]), Tool: tr}
	}
	return out, nil
}

// ---- data/dbt-parse

func runDBTParse(ctx context.Context, in *packs.Input) packs.Result {
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	if len(r.projects) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no dbt project among the changed files"}
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "cannot create the scratch dir: " + err.Error()}
	}
	defer cleanup()
	var last toolrun.Output
	for i, p := range r.projects {
		args := append([]string{"parse"}, scratchArgs(scratch, i)...)
		out, fail := runToolOnce(ctx, in, "dbt", args, absDir(in.Root, p.Dir), dbtParseTimeout)
		if fail != nil {
			return *fail
		}
		last = out
		if out.ExitCode != 0 {
			tr := toolResult(out)
			tr.Name = "dbt"
			return packs.Result{Status: packs.Fail, Tool: tr,
				Detail: fmt.Sprintf("dbt parse failed in %s (exit %d):\n%s", orRoot(p.Dir), out.ExitCode, out.Tail(20, nil))}
		}
	}
	tr := toolResult(last)
	tr.Name = "dbt"
	return packs.Result{Status: packs.OK, Detail: plural(len(r.projects), "dbt project") + " parsed", Tool: tr}
}

func orRoot(d string) string {
	if d == "" {
		return "the repo root"
	}
	return d
}

// ---- data/dbt-build-modified

func buildApplies(in *packs.Input) bool {
	return strings.EqualFold(in.Settings().String("dbt.build"), "dev") && anyDBTFile(in)
}

func runDBTBuildModified(ctx context.Context, in *packs.Input) packs.Result {
	cfg := dbtSettings(in.Settings())
	if cfg.Build != "dev" {
		return packs.Result{Status: packs.Skip, Detail: "dbt.build is off"}
	}
	if cfg.isProdTarget(cfg.DevTarget) {
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("refusing to build: dbt.dev_target %q is in dbt.prod_targets", cfg.DevTarget)}
	}
	if !targetRe.MatchString(cfg.DevTarget) {
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("dbt.dev_target %q is not a target name", cfg.DevTarget)}
	}
	r, err := newDBTRun(in)
	if err != nil {
		return failResult(err)
	}
	switch len(r.projects) {
	case 0:
		return packs.Result{Status: packs.Skip, Detail: "no dbt project among the changed files"}
	case 1:
	default:
		return packs.Result{Status: packs.Fail, Detail: "several dbt projects changed: set packs.data.dbt.project to the one dbt.state belongs to"}
	}
	if cfg.State == "" || strings.Contains(cfg.State, "..") {
		return packs.Result{Status: packs.Fail, Detail: "export the production manifest to dbt.state (packs.data.dbt.state is not set)"}
	}
	state := absDir(in.Root, cfg.State)
	if fi, err := os.Stat(filepath.Join(state, "manifest.json")); err != nil || !fi.Mode().IsRegular() {
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("export the production manifest to dbt.state (%s/manifest.json is missing)", cfg.State)}
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "cannot create the scratch dir: " + err.Error()}
	}
	defer cleanup()
	p := r.projects[0]
	args := append([]string{"build", "--select", "state:modified+", "--defer", "--state", state, "--target", cfg.DevTarget}, scratchArgs(scratch, 0)...)
	out, fail := runToolOnce(ctx, in, "dbt", args, absDir(in.Root, p.Dir), dbtBuildTimeout)
	if fail != nil {
		return *fail
	}
	tr := toolResult(out)
	tr.Name = "dbt"
	if out.ExitCode != 0 {
		return packs.Result{Status: packs.Fail, Tool: tr,
			Detail: fmt.Sprintf("dbt build of state:modified+ against %s failed (exit %d):\n%s", cfg.DevTarget, out.ExitCode, out.Tail(20, nil))}
	}
	return packs.Result{Status: packs.OK, Tool: tr, Detail: "state:modified+ built against " + cfg.DevTarget}
}
